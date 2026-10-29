package execute

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

// newEnvFileMode is the mode of an env file SetEnv has to create.
const newEnvFileMode fs.FileMode = 0o640

// writeTemp writes the new content to the temporary file; tests replace it to inject a write failure.
var writeTemp = func(f *os.File, data []byte) error {
	_, err := f.Write(data)
	return err
}

// FormatEnvValue renders v for the right-hand side of an env file line in the format of Docker Compose.
//
// Plain values of letters, digits and "_@%+:,./-" stay unquoted. Any other value is single-quoted, which
// Compose reads literally, so spaces, "#", "$" and double quotes are safe. A value with a single quote cannot
// be single-quoted; it is double-quoted instead, and only if it holds no character that Compose treats
// specially inside double quotes ("$", "\", `"`). An empty value is written as KEY=. Line breaks and NUL
// are refused.
func FormatEnvValue(v string) (string, error) {
	if strings.ContainsAny(v, "\x00\n\r") {
		return "", errors.New("value contains a line break or control character")
	}
	if v == "" {
		return "", nil
	}
	if strings.Trim(v, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_@%+:,./-") == "" {
		return v, nil
	}
	if !strings.Contains(v, "'") {
		return "'" + v + "'", nil
	}
	if strings.ContainsAny(v, "$\\\"") {
		return "", errors.New("value with a single quote cannot also contain $, \\ or a double quote")
	}
	return `"` + v + `"`, nil
}

// envLine is one statement of an env file: a plain line, or an assignment that may span several physical
// lines when its quoted value does.
type envLine struct {
	raw string
	// The parts of an assignment; key is empty for any other line.
	prefix, key, sep string // prefix is indentation and "export "; sep is the "=" with its spaces
	value, tail, eol string // tail is what follows the value, such as an inline comment
}

func (l envLine) render(newValue string) string {
	tail := l.tail
	if strings.HasPrefix(tail, "#") && !strings.HasSuffix(l.sep, " ") {
		tail = " " + tail // keep a comment that followed an empty value apart from the new one
	}
	return l.prefix + l.key + l.sep + newValue + tail + l.eol
}

func isBlank(c byte) bool { return c == ' ' || c == '\t' }

func isKeyChar(c byte) bool {
	return c == '_' || c == '.' || c == '-' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// parseEnv splits data into statements. Concatenating every raw gives data back byte for byte.
func parseEnv(data string) []envLine {
	var out []envLine
	for pos := 0; pos < len(data); {
		l, next := parseStatement(data, pos)
		out = append(out, l)
		pos = next
	}
	return out
}

func lineEnd(data string, from int) (end, next int, eol string) {
	i := strings.IndexByte(data[from:], '\n')
	if i < 0 {
		return len(data), len(data), ""
	}
	end, next = from+i, from+i+1
	if end > from && data[end-1] == '\r' {
		return end - 1, next, "\r\n"
	}
	return end, next, "\n"
}

func parseStatement(data string, pos int) (envLine, int) {
	end, next, eol := lineEnd(data, pos)
	plain := envLine{raw: data[pos:next]}
	line := data[pos:end]

	i := 0
	for i < len(line) && isBlank(line[i]) {
		i++
	}
	if i == len(line) || line[i] == '#' {
		return plain, next
	}
	if strings.HasPrefix(line[i:], "export") && i+6 < len(line) && isBlank(line[i+6]) {
		i += 6
		for i < len(line) && isBlank(line[i]) {
			i++
		}
	}
	prefix := line[:i]
	k := i
	for k < len(line) && isKeyChar(line[k]) {
		k++
	}
	if k == i {
		return plain, next
	}
	key := line[i:k]
	j := k
	for j < len(line) && isBlank(line[j]) {
		j++
	}
	if j == len(line) || line[j] != '=' {
		return plain, next
	}
	j++
	for j < len(line) && isBlank(line[j]) {
		j++
	}
	sep := line[k:j]
	valStart := pos + j

	l := envLine{prefix: prefix, key: key, sep: sep}
	switch {
	case valStart < end && (data[valStart] == '\'' || data[valStart] == '"'):
		q := data[valStart]
		closeAt := -1
		for p := valStart + 1; p < len(data); p++ {
			if q == '"' && data[p] == '\\' {
				p++
				continue
			}
			if data[p] == q {
				closeAt = p
				break
			}
		}
		if closeAt < 0 {
			return plain, next // unterminated: leave the line alone
		}
		l.value = data[valStart+1 : closeAt]
		if q == '"' {
			l.value = unescapeDouble(l.value)
		}
		tailEnd, nxt, e := lineEnd(data, closeAt)
		l.tail, l.eol = data[closeAt+1:tailEnd], e
		l.raw = data[pos:nxt]
		return l, nxt
	default:
		rest := data[valStart:end]
		cut := len(rest)
		for p := 0; p < len(rest); p++ {
			if rest[p] == '#' && (p == 0 || isBlank(rest[p-1])) {
				cut = p
				break
			}
		}
		val := strings.TrimRight(rest[:cut], " \t")
		l.value, l.tail, l.eol = val, rest[len(val):], eol
		l.raw = data[pos:next]
		return l, next
	}
}

func unescapeDouble(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '\\' || i+1 == len(s) {
			b.WriteByte(s[i])
			continue
		}
		i++
		switch s[i] {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case '"', '\\', '$':
			b.WriteByte(s[i])
		default:
			b.WriteByte('\\')
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// SetEnv sets the assignments in the env file at path and returns the values they replaced, in assignment
// order. All other bytes of the file stay as they are; only the lines of the assigned variables change, and
// a variable the file lacks is appended. The file is replaced atomically, so on any error or when ctx ends
// before the final rename the original stays unchanged and no temporary file remains.
//
// A variable that occurs more than once is replaced in every line; the previous value is the last one, which
// is the one Compose uses. Interpolation is not applied to previous values. The env file itself must not be
// a symbolic link.
func SetEnv(ctx context.Context, path string, assignments []config.Assignment) ([]EnvChange, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	data, mode, owner, err := readEnvFile(path)
	if err != nil {
		return nil, err
	}

	want := map[string]string{}
	for _, a := range assignments {
		v, err := FormatEnvValue(a.Value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", a.Key, err)
		}
		want[a.Key] = v
	}

	lines := parseEnv(data)
	prev := map[string]EnvChange{}
	var buf strings.Builder
	for _, l := range lines {
		nv, hit := want[l.key]
		if l.key == "" || !hit {
			buf.WriteString(l.raw)
			continue
		}
		prev[l.key] = EnvChange{Key: l.key, Previous: l.value, WasSet: true}
		buf.WriteString(l.render(nv))
	}
	for _, a := range assignments {
		if _, found := prev[a.Key]; found {
			continue
		}
		if buf.Len() > 0 && !strings.HasSuffix(buf.String(), "\n") {
			buf.WriteString("\n")
		}
		buf.WriteString(a.Key + "=" + want[a.Key] + "\n")
		prev[a.Key] = EnvChange{Key: a.Key}
	}

	if err := replaceFile(ctx, path, []byte(buf.String()), mode, owner); err != nil {
		return nil, err
	}
	changes := make([]EnvChange, 0, len(assignments))
	for _, a := range assignments {
		changes = append(changes, prev[a.Key])
	}
	return changes, nil
}

type fileOwner struct {
	set      bool
	uid, gid int
}

// readEnvFile returns the content, mode and owner of the env file; a missing file is empty with the default
// mode.
func readEnvFile(path string) (string, fs.FileMode, fileOwner, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return "", newEnvFileMode, fileOwner{}, nil
	}
	if errors.Is(err, syscall.ELOOP) {
		return "", 0, fileOwner{}, fmt.Errorf("env file %s is a symbolic link", path)
	}
	if err != nil {
		return "", 0, fileOwner{}, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", 0, fileOwner{}, err
	}
	if !info.Mode().IsRegular() {
		return "", 0, fileOwner{}, fmt.Errorf("env file %s is not a regular file", path)
	}
	var data bytes.Buffer
	if _, err := data.ReadFrom(f); err != nil {
		return "", 0, fileOwner{}, err
	}
	var o fileOwner
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		o = fileOwner{set: true, uid: int(st.Uid), gid: int(st.Gid)}
	}
	return data.String(), info.Mode().Perm(), o, nil
}

// replaceFile writes data to a temporary file next to path and renames it over path.
func replaceFile(ctx context.Context, path string, data []byte, mode fs.FileMode, owner fileOwner) (err error) {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	if err = tmp.Chmod(mode); err != nil {
		return err
	}
	if owner.set {
		// Best effort: only a privileged service can hand the file back to its owner.
		_ = tmp.Chown(owner.uid, owner.gid)
	}
	if err = writeTemp(tmp, data); err != nil {
		return err
	}
	if err = tmp.Sync(); err != nil {
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = os.Rename(tmp.Name(), path); err != nil {
		return err
	}
	d, derr := os.Open(dir)
	if derr != nil {
		return derr
	}
	defer d.Close()
	return d.Sync()
}

func runSetEnv(ctx context.Context, s *stepRun, st config.ResolvedStep) error {
	if err := checkSetEnv(st); err != nil {
		return err
	}
	prev, err := SetEnv(ctx, st.EnvFile, st.Assignments)
	if err != nil {
		return err
	}
	s.prev = prev
	keys := make([]string, len(st.Assignments))
	for i, a := range st.Assignments {
		keys[i] = a.Key
	}
	s.note("set %s in %s", strings.Join(keys, ", "), st.EnvFile)
	return nil
}

func checkSetEnv(st config.ResolvedStep) error {
	if st.EnvFile == "" || !filepath.IsAbs(st.EnvFile) {
		return errors.New("set-env needs an absolute env file path")
	}
	if len(st.Assignments) == 0 {
		return errors.New("set-env has no assignments")
	}
	for _, a := range st.Assignments {
		if _, err := FormatEnvValue(a.Value); err != nil {
			return fmt.Errorf("%s: %w", a.Key, err)
		}
	}
	return nil
}

func describeSetEnv(st config.ResolvedStep) ([]string, error) {
	if err := checkSetEnv(st); err != nil {
		return nil, err
	}
	out := make([]string, len(st.Assignments))
	for i, a := range st.Assignments {
		out[i] = fmt.Sprintf("set %s=%s in %s", a.Key, a.Value, st.EnvFile)
	}
	return out, nil
}
