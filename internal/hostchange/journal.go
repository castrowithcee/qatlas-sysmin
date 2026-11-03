package hostchange

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/castrowithcee/qatlas-kit/filelock"
)

// DefaultJournalDir is outside the state directory of the service, so the service cannot reach it.
const DefaultJournalDir = "/var/lib/qsysmin-host/journal"

// KeepEntries is the number of latest changes the journal keeps.
const KeepEntries = 50

// Results of a change.
const (
	ResultPending  = "pending"
	ResultApplied  = "applied"
	ResultFailed   = "failed"
	ResultRestored = "restored"
	ResultUndone   = "undone"
	// ResultRefused is reported by host apply and undo for a change a rule refused; no journal entry has it.
	ResultRefused = "refused"
)

// Outcomes of the check after a change.
const (
	CheckNone   = "none"
	CheckPassed = "passed"
	CheckFailed = "failed"
)

// Entry is one journaled change, stored as one file. Previous may hold sensitive data, which is why the
// journal is readable by root only.
type Entry struct {
	ID       string    `json:"id"`
	Time     time.Time `json:"time"`
	Action   string    `json:"action"`
	Args     []string  `json:"args"`
	Sentence string    `json:"sentence"`
	// Commands are the displayed commands, with placeholders for secrets.
	Commands []string `json:"commands"`
	Result   string   `json:"result"`
	// SudoUID is the human behind the sudo call; 0 when root was not reached through sudo.
	SudoUID       int             `json:"sudo_uid"`
	Reversible    bool            `json:"reversible"`
	NotReversible string          `json:"not_reversible,omitempty"`
	Previous      json.RawMessage `json:"previous,omitempty"`
	Check         string          `json:"check"`
	CheckError    string          `json:"check_error,omitempty"`
	Restored      bool            `json:"restored"`
	Error         string          `json:"error,omitempty"`
	// UndoOf names the entry an undo reverts; UndoneBy names the undo that reverted this entry.
	UndoOf   string `json:"undo_of,omitempty"`
	UndoneBy string `json:"undone_by,omitempty"`
}

var entryID = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}\.[0-9]{6}Z-[0-9a-f]{6}$`)

// ValidID reports whether s has the form of a journal id.
func ValidID(s string) bool { return entryID.MatchString(s) }

func newID(t time.Time) (string, error) {
	var b [3]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return t.UTC().Format("20060102T150405.000000Z") + "-" + hex.EncodeToString(b[:]), nil
}

// Journal is the root-only store of changes.
type Journal struct {
	dir   string
	owner int
}

// OpenJournal creates the directory chain with mode 0700 when it is missing and refuses a directory or an
// entry that is not owned by ownerUID or that is open to group or others. The owner is 0 in production;
// tests pass their own user.
func OpenJournal(dir string, ownerUID int) (*Journal, error) {
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("journal directory %q is not absolute", dir)
	}
	if err := ensureDir(filepath.Dir(dir), ownerUID, true); err != nil {
		return nil, err
	}
	if err := ensureDir(dir, ownerUID, false); err != nil {
		return nil, err
	}
	return &Journal{dir: dir, owner: ownerUID}, nil
}

// ensureDir creates a missing directory with 0700 and verifies it. A parent only has to be safe from
// writes by others, because the system directories above it are not ours to change.
func ensureDir(dir string, owner int, parent bool) error {
	fi, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
		fi, err = os.Lstat(dir)
	}
	if err != nil {
		return fmt.Errorf("journal directory: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("journal path %s is not a directory", dir)
	}
	if uid, ok := ownerOf(fi); !ok || uid != owner {
		return fmt.Errorf("journal path %s is not owned by the expected user", dir)
	}
	if parent {
		if fi.Mode().Perm()&0o022 != 0 {
			return fmt.Errorf("journal parent %s is writable by others (mode %04o)", dir, fi.Mode().Perm())
		}
		return nil
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("journal directory %s is open to others (mode %04o)", dir, fi.Mode().Perm())
	}
	return nil
}

func ownerOf(fi os.FileInfo) (int, bool) {
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, false
	}
	return int(st.Uid), true
}

// Lock serializes root commands on the journal; the returned function releases the lock.
func (j *Journal) Lock() (func(), error) {
	f, err := os.OpenFile(filepath.Join(j.dir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := filelock.Lock(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { _ = filelock.Unlock(f); f.Close() }, nil
}

func (j *Journal) path(id string) string { return filepath.Join(j.dir, id+".json") }

// Write stores the entry atomically with mode 0600, replacing an earlier version of the same id.
func (j *Journal) Write(e Entry) error {
	if !ValidID(e.ID) {
		return fmt.Errorf("invalid journal id %q", e.ID)
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(j.dir, "."+e.ID+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, werr := f.Write(append(data, '\n'))
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp)
		return werr
	}
	if err := os.Rename(tmp, j.path(e.ID)); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// ErrNoEntry marks an id the journal does not hold.
var ErrNoEntry = errors.New("no such journal entry")

// Get reads one entry and refuses a file that is not owned by the expected user or open to others.
func (j *Journal) Get(id string) (Entry, error) {
	if !ValidID(id) {
		return Entry{}, fmt.Errorf("invalid journal id %q", id)
	}
	path := j.path(id)
	fi, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Entry{}, ErrNoEntry
	}
	if err != nil {
		return Entry{}, err
	}
	if !fi.Mode().IsRegular() {
		return Entry{}, fmt.Errorf("journal entry %s is not a regular file", id)
	}
	if uid, ok := ownerOf(fi); !ok || uid != j.owner {
		return Entry{}, fmt.Errorf("journal entry %s is not owned by the expected user", id)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return Entry{}, fmt.Errorf("journal entry %s is open to others (mode %04o)", id, fi.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Entry{}, err
	}
	var e Entry
	if err := json.Unmarshal(data, &e); err != nil {
		return Entry{}, fmt.Errorf("journal entry %s: %w", id, err)
	}
	if e.ID != id {
		return Entry{}, fmt.Errorf("journal entry %s carries another id", id)
	}
	return e, nil
}

func (j *Journal) ids() ([]string, error) {
	files, err := os.ReadDir(j.dir)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, f := range files {
		if id, ok := strings.CutSuffix(f.Name(), ".json"); ok && ValidID(id) {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	return ids, nil
}

// List returns the entries, newest first.
func (j *Journal) List() ([]Entry, error) {
	ids, err := j.ids()
	if err != nil {
		return nil, err
	}
	entries := make([]Entry, 0, len(ids))
	for i := len(ids) - 1; i >= 0; i-- {
		e, err := j.Get(ids[i])
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// Prune removes the oldest entries beyond keep.
func (j *Journal) Prune(keep int) error {
	ids, err := j.ids()
	if err != nil {
		return err
	}
	for len(ids) > keep {
		if err := os.Remove(j.path(ids[0])); err != nil {
			return err
		}
		ids = ids[1:]
	}
	return nil
}
