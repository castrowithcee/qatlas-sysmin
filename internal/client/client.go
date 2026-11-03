// Package client is how the command line and the terminal interface talk to the service: over its control
// socket and nowhere else.
package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/user"
	"slices"
	"strconv"
	"syscall"
	"time"

	"github.com/castrowithcee/qatlas-kit/localipc"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
)

// DialTimeout bounds connecting to the service; CallTimeout bounds one call.
const (
	DialTimeout = 5 * time.Second
	CallTimeout = 30 * time.Second
)

// ServiceUser is the system user the service runs as.
const ServiceUser = control.ServiceUser

// firstAnswerTimeout bounds the wait for the answer to the first request of a connection, so a peer that
// accepts but never answers is named in a few seconds instead of after CallTimeout. It is a variable only so
// a test can shorten it.
var firstAnswerTimeout = 5 * time.Second

// Client is one connection to the service. It is not safe for concurrent calls.
type Client struct {
	conn net.Conn
	br   *bufio.Reader
	peer peerCred
	// answered is set once the peer has sent a well-formed answer; until then the first-answer deadline
	// applies.
	answered bool
}

// Dial connects to the control socket at path; an empty path means control.DefaultSocketPath. The channel
// returns only once the socket's files and the listening process belong to the service user or the caller,
// so nothing is written to anyone else: a request may carry a secret.
func Dial(path string) (*Client, error) {
	if path == "" {
		path = control.DefaultSocketPath
	}
	ep, err := control.Endpoint(path)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the service: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), DialTimeout)
	defer cancel()
	conn, err := ep.Dial(ctx)
	if err != nil {
		if errors.Is(err, syscall.EACCES) {
			if denial := accessDenial(); denial != nil {
				err = denial
			}
		}
		return nil, fmt.Errorf("cannot reach the service: %w", err)
	}
	pid, pidErr := localipc.PeerPID(conn)
	uid, uidErr := localipc.PeerUID(conn)
	if err := errors.Join(pidErr, uidErr); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("cannot verify who answers at the socket: %w", err)
	}
	return &Client{conn: conn, br: bufio.NewReader(conn), peer: peerCred{PID: int32(pid), UID: uid}}, nil
}

// PeerError names the process at the socket and the next step. Call reports one that the channel admitted
// but that does not answer the first request in time (Silent); a process that is neither the service user
// nor the caller is refused by the channel before Dial returns, with the channel's own error. It carries
// only what the kernel reported, nothing the peer sent.
type PeerError struct {
	PID int32
	UID uint32
	// Silent says the peer has the right user but did not answer the first request in time.
	Silent bool
}

func (e *PeerError) Error() string {
	next := fmt.Sprintf("stop it with \"sudo kill %d\", then run \"sudo systemctl restart %s\"", e.PID, ServiceUser)
	if e.Silent {
		return fmt.Sprintf("timeout: the process at the socket (pid %d, uid %d) does not answer; %s", e.PID, e.UID, next)
	}
	return fmt.Sprintf("the process at the socket (pid %d, uid %d) is not the %s service; %s", e.PID, e.UID, ServiceUser, next)
}

// peerCred is the listening process as the kernel reported it once the channel admitted it.
type peerCred struct {
	PID int32
	UID uint32
}

// ReservedMessage is what every surface says to someone who may not use qsysmin. It is the only text for it.
const ReservedMessage = "qsysmin is reserved for administrators (sudo)"

// NotEnabledMessage is what an administrator hears whose account is not a member of the group qsysmin.
const NotEnabledMessage = "your account is not enabled for qsysmin; an enabled administrator enables it in qsysmin tui under Host › Users"

// ErrReserved is a caller who is no administrator, or no longer is. ErrNotEnabled is an administrator who
// is not a member of the group qsysmin in the group database.
var (
	ErrReserved   = errors.New(ReservedMessage)
	ErrNotEnabled = errors.New(NotEnabledMessage)
)

// standing reads the group database for the caller: whether the account is an administrator and whether it
// is a member of the group qsysmin. Whatever cannot be read counts as no. It is a variable so a test can
// stand in for the system.
var standing = func() (admin, enabled bool) {
	h := hostadmin.NewHost(false)
	uid := os.Geteuid()
	if ok, err := h.IsAdmin(uid); err != nil || !ok {
		return false, false
	}
	ok, err := h.IsMember(uid, control.ServiceGroup)
	return true, err == nil && ok
}

// accessDenial names why a refused or unreachable socket is no concern of the service: ErrReserved for a
// caller who is no administrator, ErrNotEnabled for an administrator outside the group qsysmin, and
// ErrSessionGroup for an enabled administrator whose login session predates the membership. It is nil when
// none of these explains the refusal.
func accessDenial() error {
	admin, enabled := standing()
	switch {
	case !admin:
		return ErrReserved
	case !enabled:
		return ErrNotEnabled
	case sessionLacksGroup():
		return ErrSessionGroup
	}
	return nil
}

// ErrSessionGroup is a refused connection of a user who is in the group qsysmin, from a login session that
// started before the user joined it: the session does not carry the group yet.
var ErrSessionGroup = errors.New("this login session started before the user joined the group " + control.ServiceGroup)

// sessionLacksGroup reports whether the group database lists the user in ServiceGroup while this process
// does not carry the group. It is a variable so a test can stand in for the system.
var sessionLacksGroup = func() bool {
	g, err := user.LookupGroup(control.ServiceGroup)
	if err != nil {
		return false
	}
	gid, err := strconv.Atoi(g.Gid)
	if err != nil || os.Getegid() == gid {
		return false
	}
	if groups, err := os.Getgroups(); err != nil || slices.Contains(groups, gid) {
		return false
	}
	u, err := user.Current()
	if err != nil {
		return false
	}
	ids, err := u.GroupIds()
	return err == nil && slices.Contains(ids, g.Gid)
}

// Close ends the connection.
func (c *Client) Close() error { return c.conn.Close() }

// Call runs op with params (nil for none) and decodes the result into result (nil to discard it). A failure
// the service reports is a *control.Error.
func (c *Client) Call(op string, params, result any) error {
	req := control.Request{V: control.Version, Op: op}
	if params != nil {
		data, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("cannot encode the parameters: %w", err)
		}
		req.Params = data
		defer clear(data) // the parameters may carry a secret
	}
	now := time.Now()
	_ = c.conn.SetDeadline(now.Add(CallTimeout))
	first := !c.answered
	if first {
		_ = c.conn.SetReadDeadline(now.Add(min(firstAnswerTimeout, CallTimeout)))
	}
	// A service that rejects the caller answers and closes before the request arrives, so the write may fail
	// although the answer is waiting.
	writeErr := control.WriteLine(c.conn, req)
	line, err := control.ReadLine(c.br, control.MaxResponseBytes)
	if err != nil {
		var ne net.Error
		if first && errors.As(err, &ne) && ne.Timeout() && writeErr == nil {
			return &PeerError{PID: c.peer.PID, UID: c.peer.UID, Silent: true}
		}
		// A connection that breaks before its first answer is what a rejection looks like when the answer was lost.
		if !c.answered {
			if denial := accessDenial(); denial != nil {
				return denial
			}
		}
		if writeErr != nil {
			return fmt.Errorf("cannot send the request: %w", writeErr)
		}
		return fmt.Errorf("cannot read the answer: %w", err)
	}
	c.answered = true
	var resp control.Response
	if err := json.Unmarshal(line, &resp); err != nil || resp.V != control.Version {
		return errors.New("the service answered in a form this program does not understand")
	}
	if !resp.OK {
		switch resp.Error {
		case control.CodeNotAdmin:
			return ErrReserved
		case control.CodeRefused:
			if denial := accessDenial(); denial != nil {
				return denial
			}
		}
		return &control.Error{Code: resp.Error}
	}
	if result != nil && resp.Result != nil {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("cannot decode the result: %w", err)
		}
	}
	return nil
}

// Status asks for the service's version and socket.
func (c *Client) Status() (control.StatusResult, error) {
	var out control.StatusResult
	err := c.Call(control.OpStatus, nil, &out)
	return out, err
}

// RecordHostChange hands a finished host change to the service's audit log. Only root may call it; anyone
// else gets control.CodeNotRoot.
func (c *Client) RecordHostChange(p control.HostRecordParams) error {
	return c.Call(control.OpHostRecord, p, nil)
}

// LogEntries reads one page of the action log, newest first. p.After continues from the Next of the
// previous page.
func (c *Client) LogEntries(p control.LogListParams) (control.LogListResult, error) {
	var out control.LogListResult
	err := c.Call(control.OpLogList, p, &out)
	return out, err
}

// VerifyLog checks the hash chain of the action log. A broken
// chain is a result with Intact false, not a failed call.
func (c *Client) VerifyLog() (control.LogVerifyResult, error) {
	var out control.LogVerifyResult
	err := c.Call(control.OpLogVerify, nil, &out)
	return out, err
}
