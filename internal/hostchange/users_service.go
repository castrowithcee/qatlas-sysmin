package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// A service account is a system account without login, for a service or container with an identity of its
// own: no password, the shell nologin, no SSH keys, and a home only when asked. It is created and undone here,
// apart from the persons of user.create.

// createServiceState is the journal state of user.create-service. The captured form has no UID yet; Seal
// fills it in once the system has assigned one.
type createServiceState struct {
	Name        string `json:"name"`
	UID         int    `json:"uid"`
	Home        string `json:"home"`
	HomeCreated bool   `json:"home_created"`
}

// serviceHome is the home path of the account: the usual one when qsysmin creates it, otherwise the path that
// names no directory.
func (u userActions) serviceHome(name string, withHome bool) string {
	if withHome {
		return u.home(name)
	}
	return noHome
}

// shell reads the login shell of a user from the passwd file.
func (u userActions) shell(name string) (string, error) {
	data, err := os.ReadFile(u.h.Accounts.PasswdPath)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if f := strings.Split(strings.TrimSpace(line), ":"); len(f) >= 7 && f[0] == name {
			return f[6], nil
		}
	}
	return "", Refusef("there is no local user %s", name)
}

func (u userActions) planCreateService(args []string, _ []byte) (Plan, error) {
	req, err := parseCreateArgs(args, true)
	if err != nil {
		return Plan{}, err
	}
	home := u.serviceHome(req.name, req.home)
	homeFlag := "--no-create-home"
	if req.home {
		homeFlag = "--create-home"
	}
	useradd := []string{"useradd", "--system", homeFlag, "--home-dir", home, "--user-group", "--shell", noLoginShell}
	if gs := req.groups(); len(gs) > 0 {
		useradd = append(useradd, "-G", strings.Join(gs, ","))
	}
	sentence := "Create the service account " + req.name + " without login"
	if req.home {
		sentence += ", with the home " + home
	} else {
		sentence += ", without a home"
	}
	if req.docker {
		sentence += ", with access to Docker"
	}
	if len(req.further) > 0 {
		sentence += ", in " + groupWords(req.further)
	}
	return Plan{
		Sentence: sentence + ".",
		Commands: []Command{{Argv: append(useradd, req.name)}},
		Capture: func(context.Context) (json.RawMessage, error) {
			if _, err := u.account(req.name); err == nil {
				return nil, Refusef("the user %s exists already", req.name)
			} else if !errors.Is(err, hostadmin.ErrNotFound) {
				return nil, err
			}
			groups, err := u.h.Accounts.Groups()
			if err != nil {
				return nil, err
			}
			if slices.ContainsFunc(groups, func(g hostadmin.Group) bool { return g.Name == req.name }) {
				return nil, Refusef("a group %s exists already; the user would not get a group of its own", req.name)
			}
			// A directory at the usual home path is refused even without a home: the check afterwards could not
			// tell it from one the creation made.
			for _, path := range []string{filepath.Join(u.h.MailDir, req.name), u.home(req.name)} {
				if _, err := os.Lstat(path); err == nil {
					return nil, Refusef("%s exists already; undoing the creation would delete it", path)
				} else if !errors.Is(err, fs.ErrNotExist) {
					return nil, err
				}
			}
			for _, g := range req.groups() {
				if err := checkGroup(groups, g, !fixedGroups[g]); err != nil {
					return nil, err
				}
			}
			return json.Marshal(createServiceState{Name: req.name, UID: anyUID, Home: home, HomeCreated: req.home})
		},
		Seal: func(context.Context) (json.RawMessage, error) {
			usr, err := u.h.Accounts.User(req.name)
			if err != nil {
				return nil, err
			}
			return json.Marshal(createServiceState{Name: req.name, UID: usr.UID, Home: home, HomeCreated: req.home})
		},
		Check: func(context.Context) error {
			usr, err := u.h.Accounts.User(req.name)
			if err != nil {
				return err
			}
			if usr.UID <= 0 || hostadmin.Regular(usr.UID) {
				return fmt.Errorf("%s got UID %d, which is not a system UID", req.name, usr.UID)
			}
			if usr.Home != home {
				return fmt.Errorf("%s got the home %s instead of %s", req.name, usr.Home, home)
			}
			if sh, err := u.shell(req.name); err != nil || sh != noLoginShell {
				return fmt.Errorf("%s does not have the shell %s", req.name, noLoginShell)
			}
			if sh, err := u.shadow(req.name); err != nil || !strings.HasPrefix(sh.Hash, "!") && !strings.HasPrefix(sh.Hash, "*") {
				return fmt.Errorf("the password of %s is not locked", req.name)
			}
			_, err = os.Lstat(u.home(req.name))
			switch {
			case req.home && err != nil:
				return fmt.Errorf("the home %s was not created", home)
			case !req.home && err == nil:
				return fmt.Errorf("%s has a home although none was asked for", req.name)
			case !req.home && !errors.Is(err, fs.ErrNotExist):
				return err
			}
			for _, g := range req.groups() {
				if !memberOf(usr, g) {
					return fmt.Errorf("%s is not in the group %s", req.name, g)
				}
			}
			return nil
		},
	}, nil
}

// valid checks a recorded state strictly: it is the only input of the inverse.
func (st createServiceState) valid(u userActions) bool {
	if checkName("user", st.Name) != nil || st.Name == "root" || st.UID <= 0 {
		return false
	}
	if !filepath.IsAbs(st.Home) || filepath.Clean(st.Home) != st.Home {
		return false
	}
	return st.Home == u.serviceHome(st.Name, st.HomeCreated)
}

func (u userActions) inverseCreateService(prev json.RawMessage) (Plan, error) {
	var st createServiceState
	if err := decodeState(prev, &st); err != nil {
		return Plan{}, err
	}
	if !st.valid(u) {
		return Plan{}, errors.New("the recorded service account cannot be undone")
	}
	userdel := []string{"userdel", st.Name}
	sentence := fmt.Sprintf("Delete the service account %s, keeping files it owns outside a home.", st.Name)
	if st.HomeCreated {
		userdel = []string{"userdel", "-r", st.Name}
		sentence = fmt.Sprintf("Delete the service account %s and its home %s, which qsysmin created; files it owns elsewhere stay.", st.Name, st.Home)
	}
	return Plan{
		Sentence: sentence,
		Commands: []Command{{Argv: userdel}},
		Capture: func(ctx context.Context) (json.RawMessage, error) {
			usr, err := u.target(st.Name, st.UID)
			if err != nil {
				return nil, err
			}
			if !validUID(usr.UID) || usr.UID == 0 || hostadmin.Regular(usr.UID) {
				return nil, Refusef("%s is not a system account (UID %d) and is not deleted here", st.Name, usr.UID)
			}
			if sh, err := u.shell(st.Name); err != nil || sh != noLoginShell {
				return nil, Refusef("%s no longer has the shell %s; the account is kept", st.Name, noLoginShell)
			}
			if usr.Home != st.Home {
				return nil, Refusef("%s now has the home %s instead of %s", st.Name, usr.Home, st.Home)
			}
			if actor := ActingUID(ctx); actor != 0 && actor == usr.UID {
				return nil, Refusef("you cannot delete your own account")
			}
			if err := u.guardRevoke(ctx, usr); err != nil {
				return nil, err
			}
			return json.Marshal(map[string]any{"name": st.Name, "uid": usr.UID})
		},
		Check: func(context.Context) error {
			if _, err := u.account(st.Name); !errors.Is(err, hostadmin.ErrNotFound) {
				return fmt.Errorf("%s still exists", st.Name)
			}
			return nil
		},
	}, nil
}
