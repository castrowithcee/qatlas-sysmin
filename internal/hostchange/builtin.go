package hostchange

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// The key steps change a user's ~/.ssh/authorized_keys. sysmin runs them itself because no standard tool
// writes below a directory the user controls without following a link the user may have planted there: each
// step opens the home, .ssh and the file relative to one another and refuses a link at the last two levels.
const (
	// stepKeysAdd appends the authorized_keys lines on standard input to the file of the user Argv[1] and
	// creates .ssh (0700) and the file (0600), owned by the user, when they are missing.
	stepKeysAdd = "authorized-keys-add"
	// stepKeysRemove removes the lines of the keys with the fingerprints Argv[2:] from the user's file.
	stepKeysRemove = "authorized-keys-remove"

	builtinPrefix = "[builtin] "
	maxKeysFile   = 1 << 20
)

var builtinSteps = map[string]bool{stepKeysAdd: true, stepKeysRemove: true, stepACLGrant: true, stepACLRestore: true}

// accountLookup resolves the user a built-in step works for.
type accountLookup func(name string) (hostadmin.Account, error)

// systemAccount resolves a user of the local passwd file.
func systemAccount(name string) (hostadmin.Account, error) {
	return hostadmin.NewHost(false).Account(name)
}

func runBuiltin(c Command, lookup accountLookup) error {
	switch {
	case c.Argv[0] == stepACLGrant || c.Argv[0] == stepACLRestore:
		return runACLStep(c)
	case c.Argv[0] == stepKeysAdd && len(c.Argv) == 2:
		acc, err := lookup(c.Argv[1])
		if err != nil {
			return err
		}
		return addKeyLines(acc, c.Secret)
	case c.Argv[0] == stepKeysRemove && len(c.Argv) >= 3:
		acc, err := lookup(c.Argv[1])
		if err != nil {
			return err
		}
		return removeKeyLines(acc, c.Argv[2:])
	}
	return fmt.Errorf("built-in step %s has the wrong arguments", c.Argv[0])
}

// errNoSSHDir marks a home without .ssh.
var errNoSSHDir = errors.New("no .ssh directory")

// openSSHDir opens the .ssh directory of the account, without following a link, and creates it when asked.
func openSSHDir(acc hostadmin.Account, create bool) (int, error) {
	if !filepath.IsAbs(acc.Home) {
		return -1, fmt.Errorf("the home %q is not an absolute path", acc.Home)
	}
	home, err := unix.Open(filepath.Clean(acc.Home), unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return -1, fmt.Errorf("open the home %s: %w", acc.Home, err)
	}
	defer unix.Close(home)
	// A home that is not the user's own, such as a shared or a system directory set by mistake, never gets
	// a .ssh created or changed by root.
	var st unix.Stat_t
	if err := unix.Fstat(home, &st); err != nil {
		return -1, err
	}
	if int(st.Uid) != acc.UID {
		return -1, fmt.Errorf("the home %s does not belong to the user", acc.Home)
	}
	created := false
	if create {
		switch err := unix.Mkdirat(home, ".ssh", 0o700); {
		case err == nil:
			created = true
		case !errors.Is(err, unix.EEXIST):
			return -1, fmt.Errorf("create %s/.ssh: %w", acc.Home, err)
		}
	}
	dir, err := unix.Openat(home, ".ssh", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return -1, errNoSSHDir
	}
	if err != nil {
		return -1, fmt.Errorf("open %s/.ssh (a link is refused): %w", acc.Home, err)
	}
	if created {
		err = fixOwner(dir, acc, 0o700)
	} else {
		err = checkOwner(dir, acc, ".ssh", false)
	}
	if err != nil {
		unix.Close(dir)
		return -1, err
	}
	return dir, nil
}

func fixOwner(fd int, acc hostadmin.Account, mode uint32) error {
	if err := unix.Fchown(fd, acc.UID, acc.GID); err != nil {
		return fmt.Errorf("set the owner: %w", err)
	}
	if err := unix.Fchmod(fd, mode); err != nil {
		return fmt.Errorf("set the mode: %w", err)
	}
	return nil
}

// checkOwner refuses an entry that belongs to neither the user nor root; a file must also be regular and
// have no second name, so that a hard link cannot carry the change to another file.
func checkOwner(fd int, acc hostadmin.Account, what string, file bool) (err error) {
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	if int(st.Uid) != acc.UID && st.Uid != 0 {
		return fmt.Errorf("%s belongs to another user", what)
	}
	if file && (st.Mode&unix.S_IFMT != unix.S_IFREG || st.Nlink != 1) {
		return fmt.Errorf("%s is not a plain file", what)
	}
	return nil
}

// keyLine is one parseable line of an authorized_keys file.
type keyLine struct {
	raw, fingerprint string
}

func parseKeyLines(data []byte) []keyLine {
	var out []keyLine
	for _, line := range strings.Split(string(data), "\n") {
		key, _, _, rest, err := ssh.ParseAuthorizedKey([]byte(line))
		if err != nil || len(bytes.TrimSpace(rest)) > 0 {
			continue
		}
		out = append(out, keyLine{raw: strings.TrimRight(line, "\r"), fingerprint: ssh.FingerprintSHA256(key)})
	}
	return out
}

func readAll(f *os.File) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(f, maxKeysFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxKeysFile {
		return nil, errors.New("authorized_keys is too large")
	}
	return data, nil
}

// readKeyLines lists the keys of the account's authorized_keys; a missing .ssh or file is an empty list.
func readKeyLines(acc hostadmin.Account) ([]keyLine, error) {
	dir, err := openSSHDir(acc, false)
	if errors.Is(err, errNoSSHDir) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, "authorized_keys", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if errors.Is(err, unix.ENOENT) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open authorized_keys (a link is refused): %w", err)
	}
	f := os.NewFile(uintptr(fd), "authorized_keys")
	defer f.Close()
	if err := checkOwner(fd, acc, "authorized_keys", true); err != nil {
		return nil, err
	}
	data, err := readAll(f)
	if err != nil {
		return nil, err
	}
	return parseKeyLines(data), nil
}

// inputKeys parses the standard input of stepKeysAdd: every line that is not blank must be one key.
func inputKeys(input []byte) ([]keyLine, error) {
	var out []keyLine
	for _, line := range strings.Split(string(input), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		ks := parseKeyLines([]byte(line))
		if len(ks) != 1 {
			return nil, errors.New("the input holds a line that is not an SSH key")
		}
		out = append(out, ks[0])
	}
	if len(out) == 0 {
		return nil, errors.New("the input holds no SSH key")
	}
	return out, nil
}

func addKeyLines(acc hostadmin.Account, input []byte) error {
	adds, err := inputKeys(input)
	if err != nil {
		return err
	}
	dir, err := openSSHDir(acc, true)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	created := true
	fd, err := unix.Openat(dir, "authorized_keys", unix.O_RDWR|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if errors.Is(err, unix.EEXIST) {
		created = false
		fd, err = unix.Openat(dir, "authorized_keys", unix.O_RDWR|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	}
	if err != nil {
		return fmt.Errorf("open authorized_keys (a link is refused): %w", err)
	}
	f := os.NewFile(uintptr(fd), "authorized_keys")
	defer f.Close()
	if created {
		err = fixOwner(fd, acc, 0o600)
	} else {
		err = checkOwner(fd, acc, "authorized_keys", true)
	}
	if err != nil {
		return err
	}
	data, err := readAll(f)
	if err != nil {
		return err
	}
	have := map[string]bool{}
	for _, k := range parseKeyLines(data) {
		have[k.fingerprint] = true
	}
	var buf bytes.Buffer
	if len(data) > 0 && data[len(data)-1] != '\n' {
		buf.WriteByte('\n')
	}
	for _, k := range adds {
		if have[k.fingerprint] {
			return fmt.Errorf("the key %s is there already", k.fingerprint)
		}
	}
	for _, k := range adds {
		buf.WriteString(k.raw + "\n")
	}
	if _, err := f.WriteAt(buf.Bytes(), int64(len(data))); err != nil {
		return err
	}
	return f.Sync()
}

func removeKeyLines(acc hostadmin.Account, fingerprints []string) error {
	drop := map[string]bool{}
	for _, fp := range fingerprints {
		drop[fp] = true
	}
	dir, err := openSSHDir(acc, false)
	if err != nil {
		return err
	}
	defer unix.Close(dir)
	fd, err := unix.Openat(dir, "authorized_keys", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("open authorized_keys (a link is refused): %w", err)
	}
	f := os.NewFile(uintptr(fd), "authorized_keys")
	defer f.Close()
	if err := checkOwner(fd, acc, "authorized_keys", true); err != nil {
		return err
	}
	var st unix.Stat_t
	if err := unix.Fstat(fd, &st); err != nil {
		return err
	}
	data, err := readAll(f)
	if err != nil {
		return err
	}
	var kept strings.Builder
	removed := 0
	for _, line := range strings.SplitAfter(string(data), "\n") {
		if ks := parseKeyLines([]byte(line)); len(ks) == 1 && drop[ks[0].fingerprint] {
			removed++
			continue
		}
		kept.WriteString(line)
	}
	if removed == 0 {
		return errors.New("authorized_keys holds none of the keys to remove")
	}
	return replaceAt(dir, "authorized_keys", []byte(kept.String()), int(st.Uid), int(st.Gid), st.Mode&0o777)
}

// replaceAt writes a new file next to name and renames it over name, with the owner and mode of the old one.
func replaceAt(dir int, name string, data []byte, uid, gid int, mode uint32) error {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return err
	}
	tmp := "." + name + ".sysmin-" + hex.EncodeToString(b[:])
	fd, err := unix.Openat(dir, tmp, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if err != nil {
		return err
	}
	f := os.NewFile(uintptr(fd), tmp)
	_, err = f.Write(data)
	if err == nil {
		err = unix.Fchown(fd, uid, gid)
	}
	if err == nil {
		err = unix.Fchmod(fd, mode)
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = unix.Renameat(dir, tmp, dir, name)
	}
	if err != nil {
		_ = unix.Unlinkat(dir, tmp, 0)
		return err
	}
	return unix.Fsync(dir)
}
