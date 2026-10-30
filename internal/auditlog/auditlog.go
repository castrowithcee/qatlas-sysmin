package auditlog

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/filelock"
)

const (
	// DefaultDir is where the service keeps its log files.
	DefaultDir = "/var/lib/sysmin/logs"
	// DefaultRetentionDays is how long a day file is kept unless the caller chooses otherwise.
	DefaultRetentionDays = 90

	dirMode      = 0o700
	fileMode     = 0o600
	lockFileName = ".lock"
)

// dayFilePattern matches a day file's name and captures its date.
var dayFilePattern = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})\.jsonl$`)

// genesisHash is the prev_hash of the very first entry this installation ever logs: the SHA-256 of zero
// bytes, the canonical hash of "nothing came before this".
var genesisHash = hashHex(nil)

// Logger appends entries to the day files of one directory. The zero value is not usable; use New.
type Logger struct {
	dir           string
	retentionDays int
	// key signs every entry written; nil leaves them unsigned.
	key *Key
	// now is the logger's clock; nil means time.Now. A test overrides it to move the clock across days
	// without waiting.
	now func() time.Time
}

// New returns a logger writing under dir, keeping day files for retentionDays days. It touches no file: the
// directory and its files are created lazily, on the first Append.
func New(dir string, retentionDays int) *Logger {
	return &Logger{dir: dir, retentionDays: retentionDays}
}

// WithKey returns a logger like l that signs every entry it writes with key; see Key. A nil key signs
// nothing, like l itself. The key stays the caller's, who clears it once no logger uses it any more.
func (l *Logger) WithKey(key *Key) *Logger {
	out := *l
	out.key = key
	return &out
}

// Signs reports whether l signs the entries it writes.
func (l *Logger) Signs() bool { return l.key.usable() }

func (l *Logger) clock() time.Time {
	if l.now != nil {
		return l.now().UTC()
	}
	return time.Now().UTC()
}

// Append writes one entry for a completed action, chained to the last entry this installation ever wrote,
// across day files and across processes. Retention runs first, under the same lock: a day file older than
// the logger's retention window is removed, and if any file was actually removed, a KindCut marker
// documenting the cut is written immediately before the real entry, in the same file.
//
// When the last line of the newest day file does not parse as an entry, such as a line torn by a crash
// while it was written, Append does not fail on it: it ends that line with a newline if it lacks one and
// it is in today's file, and writes a KindDamage marker naming the line's hash before anything else. The
// marker chains from the last line before the damaged one that does parse, as the entry would have.
//
// A returned error means the entry might not be durable. The caller reports it; it must never change the
// result of the action it was called for.
func (l *Logger) Append(f Fields) error {
	if l.dir == "" {
		return errors.New("no log directory")
	}
	// A window below one day would remove today's own file, so it is refused rather than clamped.
	if l.retentionDays < 1 {
		return errors.New("retention must be at least one day")
	}
	if err := f.Validate(); err != nil {
		return fmt.Errorf("invalid log entry: %w", err)
	}
	dir := l.dir
	if err := os.MkdirAll(dir, dirMode); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	unlock, err := lockDir(dir)
	if err != nil {
		return fmt.Errorf("lock %s: %w", dir, err)
	}
	defer unlock()

	now := l.clock()
	todayPath := filepath.Join(dir, now.Format("2006-01-02")+".jsonl")

	removed, removedRaw, err := applyRetention(dir, now, l.retentionDays)
	if err != nil {
		return err
	}
	files, err := dayFiles(dir)
	if err != nil {
		return err
	}
	end, err := lastLine(dir, files)
	if err != nil {
		return err
	}

	// A surviving file is always more recent than anything retention just removed, so it wins when both
	// exist; removedRaw is the fallback for the case retention just deleted every existing file, so the
	// chain still continues from what was removed instead of silently resetting to the genesis hash.
	prevHash, seq := genesisHash, uint64(1)
	switch {
	case end.raw != nil:
		prevHash, seq = hashHex(end.raw), end.entry.Seq+1
	case removedRaw != nil:
		prevHash, seq = hashHex(removedRaw), removed.Seq+1
	}

	var lines [][]byte
	if end.damaged != nil {
		// The marker comes first, so it is the line right after the damaged one, which is where Verify
		// looks for it.
		if end.unterminated && end.damagedPath == todayPath {
			// appendLines writes an empty line as a lone newline, which ends the damaged line.
			lines = append(lines, nil)
		}
		marker := Entry{Seq: seq, Time: now, Kind: KindDamage, Damage: &Damage{LineHash: hashHex(end.damaged)}, PrevHash: prevHash}
		raw, err := l.encode(marker)
		if err != nil {
			return fmt.Errorf("encode damaged-line entry: %w", err)
		}
		lines = append(lines, raw)
		prevHash, seq = hashHex(raw), seq+1
	}
	if removedRaw != nil {
		cut := Entry{Seq: seq, Time: now, Kind: KindCut, Cut: &Cut{ThroughSeq: removed.Seq}, PrevHash: prevHash}
		raw, err := l.encode(cut)
		if err != nil {
			return fmt.Errorf("encode retention cut entry: %w", err)
		}
		lines = append(lines, raw)
		prevHash, seq = hashHex(raw), seq+1
	}

	uid := f.ClientUID
	entry := Entry{
		Seq: seq, Time: now, ClientUID: &uid, Action: f.Action, Project: f.Project, Result: f.Result,
		ErrorCode: f.ErrorCode, DurationMS: f.Duration.Milliseconds(), PrevHash: prevHash,
	}
	raw, err := l.encode(entry)
	if err != nil {
		return fmt.Errorf("encode log entry: %w", err)
	}
	lines = append(lines, raw)

	return appendLines(todayPath, lines)
}

// encode is the line entry is stored as: its JSON encoding with an empty MAC, signed when l has a key.
func (l *Logger) encode(entry Entry) ([]byte, error) {
	entry.MAC = ""
	raw, err := json.Marshal(entry)
	if err != nil || !l.key.usable() {
		return raw, err
	}
	return l.key.sign(raw)
}

// applyRetention removes every day file older than retentionDays, relative to now, and returns the last
// entry among the files it removed and its exact raw line, or a nil line when nothing was old enough to
// remove or no removed line parses. Retention only ever removes a contiguous run of the oldest days, so the
// boundary it leaves behind is always the last line of exactly the files it just deleted that parses,
// found the same way lastLine finds it; a damaged line among them goes with its file.
func applyRetention(dir string, now time.Time, retentionDays int) (Entry, []byte, error) {
	files, err := dayFiles(dir)
	if err != nil {
		return Entry{}, nil, err
	}
	cutoff := now.AddDate(0, 0, -retentionDays).Format("2006-01-02")
	var expired []string
	for _, name := range files {
		if dayFilePattern.FindStringSubmatch(name)[1] < cutoff {
			expired = append(expired, name)
		}
	}
	if len(expired) == 0 {
		return Entry{}, nil, nil
	}
	// files is already sorted chronologically, and expired keeps that order.
	end, err := lastLine(dir, expired)
	if err != nil {
		return Entry{}, nil, err
	}
	for _, name := range expired {
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return Entry{}, nil, fmt.Errorf("remove expired log %s: %w", name, err)
		}
	}
	if end.raw == nil {
		// The expired files held no line that parses, so they left nothing to document a cut through.
		return Entry{}, nil, nil
	}
	return end.entry, end.raw, nil
}

// dayFiles returns the day file names of dir, sorted chronologically, which is also lexicographic for a
// YYYY-MM-DD name. A directory that does not exist yet holds none.
func dayFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && dayFilePattern.MatchString(entry.Name()) {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// chainEnd is where the chain of a set of day files continues.
type chainEnd struct {
	// entry and raw are the last line that parses as an entry, raw nil when there is none.
	entry Entry
	raw   []byte
	// damaged is the last line of the newest file holding one when that line does not parse, nil
	// otherwise; damagedPath is its file, and unterminated is true when no newline follows it.
	damaged      []byte
	damagedPath  string
	unterminated bool
}

// lastLine returns where the chain of the day files files of dir continues: their last line that parses as
// an entry, most recent file first, which is the line Verify compares the next entry with. When the very
// last line does not parse, it is returned as damaged, and the search goes on before it. Only that case
// reads a whole file; otherwise lastLine reads only the tail of the file it stops at.
func lastLine(dir string, files []string) (chainEnd, error) {
	var end chainEnd
	for i := len(files) - 1; i >= 0; i-- {
		path := filepath.Join(dir, files[i])
		raw, err := lastLineOf(path)
		if err != nil {
			return chainEnd{}, err
		}
		if raw == nil {
			continue
		}
		if entry, ok := parseLine(raw); ok {
			end.entry, end.raw = entry, raw
			return end, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return chainEnd{}, fmt.Errorf("read %s: %w", path, err)
		}
		lines := splitLines(data)
		if len(lines) == 0 {
			continue
		}
		if end.damaged == nil {
			end.damaged, end.damagedPath = lines[len(lines)-1], path
			end.unterminated = !bytes.HasSuffix(data, []byte("\n"))
		}
		for j := len(lines) - 2; j >= 0; j-- {
			if entry, ok := parseLine(lines[j]); ok {
				end.entry, end.raw = entry, lines[j]
				return end, nil
			}
		}
	}
	return end, nil
}

// parseLine decodes one line, and reports whether it parses as an entry at all.
func parseLine(raw []byte) (Entry, bool) {
	var entry Entry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return Entry{}, false
	}
	return entry, true
}

// lastLineBlockSize is the size of the blocks lastLineOf reads backward from the end of a day file.
const lastLineBlockSize = 4096

// lastLineOf returns the last line of path, whether it parses or not, or nil when the file does not exist
// or holds no line. It reads only the tail of the file, so its cost does not grow with the file.
func lastLineOf(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	raw, err := lastLineFrom(f, info.Size(), lastLineBlockSize)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return raw, nil
}

// lastLineFrom returns the last line of the size bytes in r, exactly as the last element of splitLines
// would, or nil when there is none. It reads blocks of blockSize backward from the end and accumulates
// them until the line start is found, so a line longer than a block is still returned whole.
func lastLineFrom(r io.ReaderAt, size int64, blockSize int64) ([]byte, error) {
	var tail []byte
	for pos := size; pos > 0; {
		start := pos - blockSize
		if start < 0 {
			start = 0
		}
		block := make([]byte, pos-start)
		if _, err := r.ReadAt(block, start); err != nil {
			return nil, err
		}
		tail = append(block, tail...)
		pos = start
		trimmed := bytes.TrimRight(tail, "\n")
		if len(trimmed) == 0 {
			continue
		}
		if i := bytes.LastIndexByte(trimmed, '\n'); i >= 0 {
			return trimmed[i+1:], nil
		}
	}
	trimmed := bytes.TrimRight(tail, "\n")
	if len(trimmed) == 0 {
		return nil, nil
	}
	return trimmed, nil
}

// splitLines returns the non-empty lines of data, without their trailing newline.
func splitLines(data []byte) [][]byte {
	data = bytes.TrimRight(data, "\n")
	if len(data) == 0 {
		return nil
	}
	return bytes.Split(data, []byte("\n"))
}

// appendLines appends every line of lines, each followed by a newline, to path, creating it at fileMode if
// it does not exist yet and forcing that mode either way. It syncs before returning, so the lines are
// durable once it returns. Each line is a write of its own, so a crash while it writes can still leave the
// last line torn; the next Append documents such a line with a KindDamage marker instead of failing on it.
func appendLines(path string, lines [][]byte) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, fileMode)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if err := f.Chmod(fileMode); err != nil {
		return fmt.Errorf("set the permissions of %s: %w", path, err)
	}
	for _, line := range lines {
		if _, err := f.Write(append(append([]byte(nil), line...), '\n')); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// hashHex is the canonical hash of one line: the hex-encoded SHA-256 of its exact bytes, without a
// trailing newline. It is applied only to a line exactly as Append wrote it or Verify read it, never to a
// re-encoding, so a byte that changes anywhere in the line changes the hash.
func hashHex(line []byte) string {
	sum := sha256.Sum256(line)
	return hex.EncodeToString(sum[:])
}

// lockDir opens (creating if needed) and exclusively locks dir's lock file, and returns a function that
// unlocks and closes it. dir must already exist.
func lockDir(dir string) (func(), error) {
	f, err := os.OpenFile(filepath.Join(dir, lockFileName), os.O_CREATE|os.O_RDWR, fileMode)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(f); err != nil {
		_ = f.Close()
		return nil, err
	}
	return func() {
		_ = filelock.Unlock(f)
		_ = f.Close()
	}, nil
}
