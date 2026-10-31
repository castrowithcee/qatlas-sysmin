package hostadmin

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"

	"golang.org/x/crypto/ssh"
)

const maxAuthorizedKeysBytes = 1 << 20

// readAuthorizedKeys lists the keys of a home directory by type, comment and fingerprint. A missing file
// is an empty list; a file that cannot be read safely is unknown (nil). Only a regular file is read, never
// through a link, so a link cannot point the privileged read at another file.
func readAuthorizedKeys(home string) []SSHKey {
	if !filepath.IsAbs(home) {
		return nil
	}
	path := filepath.Join(home, ".ssh", "authorized_keys")
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return []SSHKey{}
	}
	if err != nil {
		return nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil
	}
	keys := []SSHKey{}
	sc := bufio.NewScanner(io.LimitReader(f, maxAuthorizedKeysBytes))
	sc.Buffer(make([]byte, 0, 64<<10), maxAuthorizedKeysBytes)
	for sc.Scan() {
		key, comment, _, _, err := ssh.ParseAuthorizedKey(sc.Bytes())
		if err != nil {
			continue
		}
		keys = append(keys, SSHKey{Type: key.Type(), Comment: comment, Fingerprint: ssh.FingerprintSHA256(key)})
	}
	if sc.Err() != nil {
		return nil
	}
	return keys
}
