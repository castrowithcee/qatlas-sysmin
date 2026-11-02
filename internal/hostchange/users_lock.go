package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Locking stops every login: the password is locked and the account ends. Locking the password alone would
// leave a login with an SSH key open.

// lockExpire is the day the account ends when locked, the day after the epoch: long past.
const lockExpire = "1"

// lockState is the journal state of user.lock and user.unlock, enough to restore the lock exactly.
type lockState struct {
	Name   string `json:"name"`
	UID    int    `json:"uid"`
	Hash   string `json:"hash"`
	Expire string `json:"expire"`
}

func hashLocked(h string) bool { return strings.HasPrefix(h, "!") || strings.HasPrefix(h, "*") }

func (u userActions) planLock(args []string, _ []byte) (Plan, error) {
	name, err := lockArgs(ActionUserLock, args)
	if err != nil {
		return Plan{}, err
	}
	return Plan{
		Sentence: "Lock " + name + ": no login is possible any more, with a password or an SSH key.",
		Commands: []Command{{Argv: []string{"usermod", "-L", "-e", lockExpire, name}}},
		Capture: func(ctx context.Context) (json.RawMessage, error) {
			usr, err := u.target(name, anyUID)
			if err != nil {
				return nil, err
			}
			if actor := ActingUID(ctx); actor != 0 && actor == usr.UID {
				return nil, Refusef("you cannot lock yourself")
			}
			st, err := u.lockSnapshot(usr.Name, usr.UID)
			if err != nil {
				return nil, err
			}
			if hashLocked(st.Hash) && usr.Expired != nil && *usr.Expired {
				return nil, Refusef("nothing to change: %s is locked already", name)
			}
			if err := u.guardRevoke(ctx, usr); err != nil {
				return nil, err
			}
			return json.Marshal(st)
		},
		Check: func(context.Context) error {
			now, err := u.shadow(name)
			if err != nil {
				return err
			}
			if !hashLocked(now.Hash) || now.Expire != lockExpire {
				return fmt.Errorf("%s is not locked", name)
			}
			return nil
		},
	}, nil
}

// releaseHash is the hash an unlock leaves: one leading "!" less, as usermod -U does where something remains,
// otherwise the hash unchanged.
func releaseHash(h string) string {
	if strings.HasPrefix(h, "!") && len(h) > 1 {
		return h[1:]
	}
	return h
}

func (u userActions) planUnlock(args []string, _ []byte) (Plan, error) {
	name, err := lockArgs(ActionUserUnlock, args)
	if err != nil {
		return Plan{}, err
	}
	var want string
	return Plan{
		Sentence: "Unlock " + name + ": logins work again as before the lock, and the account no longer ends.",
		Commands: []Command{{Argv: []string{"usermod", "-U", "-e", "-1", name}}},
		Capture: func(context.Context) (json.RawMessage, error) {
			usr, err := u.target(name, anyUID)
			if err != nil {
				return nil, err
			}
			st, err := u.lockSnapshot(usr.Name, usr.UID)
			if err != nil {
				return nil, err
			}
			want = releaseHash(st.Hash)
			if want == st.Hash && st.Expire == "" {
				return nil, Refusef("nothing to change: %s is not locked", name)
			}
			return json.Marshal(st)
		},
		Check: func(context.Context) error {
			now, err := u.shadow(name)
			if err != nil {
				return err
			}
			if now.Hash != want || now.Expire != "" {
				return fmt.Errorf("%s is still locked", name)
			}
			return nil
		},
	}, nil
}

func lockArgs(id string, args []string) (string, error) {
	if len(args) != 1 {
		return "", Argf("%s needs a user name", id)
	}
	if err := checkName("user", args[0]); err != nil {
		return "", err
	}
	return args[0], nil
}

// lockSnapshot records the shadow fields a restore needs, refusing what it could not restore exactly.
func (u userActions) lockSnapshot(name string, uid int) (lockState, error) {
	sh, err := u.shadow(name)
	if err != nil {
		return lockState{}, err
	}
	if !validHash(sh.Hash) || !validDay(sh.Expire) {
		return lockState{}, Refusef("the password or expiry entry of %s is empty or unusual and could not be restored", name)
	}
	return lockState{Name: name, UID: uid, Hash: sh.Hash, Expire: sh.Expire}, nil
}

// inverseLock puts the password lock and the expiry back as recorded. The lock of the hash is undone or redone
// by the one leading "!" the current hash differs by; any other difference means the password changed since,
// and nothing is restored. Where that locks out a user who is open now, it is guarded like a lock.
func (u userActions) inverseLock(prev json.RawMessage) (Plan, error) {
	var st lockState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if err := checkName("user", st.Name); err != nil || !validUID(st.UID) || !validHash(st.Hash) || !validDay(st.Expire) {
		return Plan{}, errors.New("the recorded lock state cannot be restored")
	}
	cur, err := u.shadow(st.Name)
	if err != nil {
		return Plan{}, err
	}
	var flag []string
	switch {
	case cur.Hash == st.Hash:
	case cur.Hash == "!"+st.Hash:
		flag = []string{"-U"}
	case st.Hash == "!"+cur.Hash && !strings.HasPrefix(cur.Hash, "!"):
		flag = []string{"-L"}
	default:
		return Plan{}, fmt.Errorf("the password of %s changed since; the lock state is not restored", st.Name)
	}
	expire := "-1"
	if st.Expire != "" {
		expire = st.Expire
	}
	argv := append(append([]string{"usermod"}, flag...), "-e", expire, st.Name)
	return Plan{
		Sentence: "Restore the previous lock state of " + st.Name + ".",
		Commands: []Command{{Argv: argv}},
		Capture: func(ctx context.Context) (json.RawMessage, error) {
			usr, err := u.target(st.Name, st.UID)
			if err != nil {
				return nil, err
			}
			if st.Expire != "" && !lockedOut(usr) {
				if actor := ActingUID(ctx); actor != 0 && actor == usr.UID {
					return nil, Refusef("you cannot lock yourself")
				}
				if err := u.guardRevoke(ctx, usr); err != nil {
					return nil, err
				}
			}
			now, err := u.lockSnapshot(st.Name, usr.UID)
			if err != nil {
				return nil, err
			}
			if now.Hash != cur.Hash {
				return nil, Refusef("the password of %s changed since; the lock state is not restored", st.Name)
			}
			return json.Marshal(now)
		},
		Check: func(context.Context) error {
			now, err := u.shadow(st.Name)
			if err != nil {
				return err
			}
			if now.Hash != st.Hash || now.Expire != st.Expire {
				return fmt.Errorf("the previous lock state of %s is not back", st.Name)
			}
			return nil
		},
	}, nil
}
