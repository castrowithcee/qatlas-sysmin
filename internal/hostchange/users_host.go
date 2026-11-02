package hostchange

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// UserHost names what the user actions read as root. Tests point it at the files of an invented host.
type UserHost struct {
	// Accounts reads passwd, group and shadow; it must be privileged.
	Accounts *hostadmin.Host
	// HomeBase is the directory new homes are created in.
	HomeBase string
	// MailDir holds the mail spools that userdel -r removes.
	MailDir string
	// Lastlog is the classic login record file; Lastlog2 is the database that replaces it on newer systems.
	Lastlog, Lastlog2 string
	// lookup replaces the passwd lookup of the owner checks in tests, whose files all belong to one user.
	lookup accountLookup
}

// DefaultUserHost is the real host.
func DefaultUserHost() UserHost {
	return UserHost{
		Accounts: hostadmin.NewHost(true), HomeBase: "/home", MailDir: "/var/mail",
		Lastlog: "/var/log/lastlog", Lastlog2: "/var/lib/lastlog/lastlog2.db",
	}
}

// anyUID asks target for no match of the UID. It is not 0, because that is the UID of root.
const anyUID = -1

// accountName is the Debian NAME_REGEX without the trailing "$" of machine accounts, at most 32 characters.
var accountName = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

var keyFingerprint = regexp.MustCompile(`^SHA256:[A-Za-z0-9+/]{43}$`)

func checkName(kind, name string) error {
	if !accountName.MatchString(name) {
		return Argf("%q is not a valid %s name (lower-case letters, digits, _ and -, at most 32)", name, kind)
	}
	return nil
}

// target reads the user an action changes and refuses anyone outside the local passwd file. A uid other than
// anyUID also refuses a different user that took over the name since the change.
func (u userActions) target(name string, uid int) (hostadmin.User, error) {
	usr, err := u.h.Accounts.User(name)
	if errors.Is(err, hostadmin.ErrNotFound) {
		return hostadmin.User{}, Refusef("there is no local user %s", name)
	}
	if err != nil {
		return hostadmin.User{}, err
	}
	if uid != anyUID && usr.UID != uid {
		return hostadmin.User{}, Refusef("%s now has UID %d instead of %d; it is not the user of the change", name, usr.UID, uid)
	}
	return usr, nil
}

// guardRevoke keeps the administrators: nobody takes administrator rights or access to sysmin from
// themselves, and someone else keeps both. Every account in sudo and in sysmin counts, whatever its UID,
// because each of them can operate sysmin.
func (u userActions) guardRevoke(ctx context.Context, usr hostadmin.User) error {
	if !usr.Admin {
		return nil
	}
	if actor := ActingUID(ctx); actor != 0 && actor == usr.UID {
		return Refusef("you cannot take administrator rights or access to sysmin from yourself")
	}
	users, err := u.h.Accounts.Users()
	if err != nil {
		return err
	}
	for _, o := range users {
		if o.Name != usr.Name && o.UID != usr.UID && o.Admin && o.Enabled && !lockedOut(o) {
			return nil
		}
	}
	return Refusef("%s is the last administrator with access to sysmin and stays one", usr.Name)
}

// lockedOut reports an account that cannot log in at all: one that has ended, which is what a lock adds. A
// locked password alone does not count, because a key login still works and many accounts have no password.
func lockedOut(usr hostadmin.User) bool { return usr.Expired != nil && *usr.Expired }

func (u userActions) account(name string) (hostadmin.Account, error) {
	if u.h.lookup != nil {
		return u.h.lookup(name)
	}
	return u.h.Accounts.Account(name)
}

func memberOf(usr hostadmin.User, group string) bool {
	return usr.PrimaryGroup == group || slices.Contains(usr.Groups, group)
}

// shadowFields are the fields of a shadow entry that chpasswd and chage change.
type shadowFields struct {
	Hash, Lastchg string
	// Expire is the day an account ends; empty means never.
	Expire string
}

func (u userActions) shadow(name string) (shadowFields, error) {
	data, err := os.ReadFile(u.h.Accounts.ShadowPath)
	if err != nil {
		return shadowFields{}, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Split(strings.TrimSpace(line), ":")
		if len(f) >= 3 && f[0] == name {
			sf := shadowFields{Hash: f[1], Lastchg: f[2]}
			if len(f) > 7 {
				sf.Expire = f[7]
			}
			return sf, nil
		}
	}
	return shadowFields{}, Refusef("%s has no entry in the shadow file", name)
}

// lastlogRecord is the size of one record of /var/log/lastlog: a 32-bit time, the line and the host.
const lastlogRecord = 4 + 32 + 256

// loginRecorded reads the classic lastlog record of the UID. Where lastlog2 keeps the records, or the
// classic file is missing, whether the user logged in cannot be determined and the answer is an error.
func (u userActions) loginRecorded(uid int) (bool, error) {
	if u.h.Lastlog2 != "" {
		if _, err := os.Lstat(u.h.Lastlog2); err == nil {
			return false, fmt.Errorf("logins are recorded in %s, which sysmin does not read", u.h.Lastlog2)
		} else if !errors.Is(err, fs.ErrNotExist) {
			return false, err
		}
	}
	f, err := os.OpenFile(u.h.Lastlog, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return false, fmt.Errorf("there are no login records: %w", err)
	}
	defer f.Close()
	if info, err := f.Stat(); err != nil || !info.Mode().IsRegular() {
		return false, fmt.Errorf("%s is not a plain file", u.h.Lastlog)
	}
	rec := make([]byte, lastlogRecord)
	n, err := f.ReadAt(rec, int64(uid)*lastlogRecord)
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	for _, b := range rec[:n] {
		if b != 0 {
			return true, nil
		}
	}
	return false, nil
}

// keyLines lists the keys of the user's authorized_keys, read without following a link.
func (u userActions) keyLines(name string) ([]keyLine, error) {
	acc, err := u.account(name)
	if err != nil {
		return nil, err
	}
	lines, err := readKeyLines(acc)
	if err != nil {
		return nil, Refusef("the SSH keys of %s cannot be read safely: %v", name, err)
	}
	return lines, nil
}

func hasFingerprint(lines []keyLine, fp string) bool {
	return slices.ContainsFunc(lines, func(k keyLine) bool { return k.fingerprint == fp })
}

// listWords joins names for a sentence: "a", "a and b", "a, b and c".
func listWords(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

func groupWords(names []string) string {
	if len(names) == 1 {
		return "the group " + names[0]
	}
	return "the groups " + listWords(names)
}
