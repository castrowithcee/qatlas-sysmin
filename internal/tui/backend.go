package tui

import (
	"errors"
	"sync"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// Backend is what the interface asks of the service. Every call runs as a command, off the event loop, so an
// implementation may block; it must serialize its calls, because the connection behind it is not safe for
// concurrent use. A failure the service reports is a *control.Error.
type Backend interface {
	// Status reports the state of the service.
	Status() (control.StatusResult, error)
	// LogEntries and VerifyLog only read; every call is an entry of the audit log itself, so none is made on a
	// timer.
	LogEntries(p control.LogListParams) (control.LogListResult, error)
	VerifyLog() (control.LogVerifyResult, error)
}

// NewBackend is the Backend over the control socket at path. It dials on the first call and again after the
// connection broke.
func NewBackend(socket string) *ClientBackend { return &ClientBackend{socket: socket} }

// ClientBackend is a Backend over one connection to the service.
type ClientBackend struct {
	socket string
	mu     sync.Mutex
	conn   *client.Client
}

// Close ends the connection, if there is one.
func (b *ClientBackend) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.conn == nil {
		return nil
	}
	err := b.conn.Close()
	b.conn = nil
	return err
}

// do runs fn on the connection. A failure that is no answer of the service means the connection is gone or
// not trustworthy, so it is dropped and the next call dials again; the read-only call is repeated once on a
// fresh connection.
func (b *ClientBackend) do(fn func(*client.Client) error) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for attempt := 0; ; attempt++ {
		if b.conn == nil {
			c, err := client.Dial(b.socket)
			if err != nil {
				return err
			}
			b.conn = c
		}
		err := fn(b.conn)
		var ce *control.Error
		if err == nil || errors.As(err, &ce) {
			return err
		}
		_ = b.conn.Close()
		b.conn = nil
		if attempt > 0 {
			return err
		}
	}
}

func (b *ClientBackend) Status() (st control.StatusResult, err error) {
	err = b.do(func(c *client.Client) (e error) { st, e = c.Status(); return e })
	return st, err
}

func (b *ClientBackend) LogEntries(p control.LogListParams) (res control.LogListResult, err error) {
	err = b.do(func(c *client.Client) (e error) { res, e = c.LogEntries(p); return e })
	return res, err
}

func (b *ClientBackend) VerifyLog() (res control.LogVerifyResult, err error) {
	err = b.do(func(c *client.Client) (e error) { res, e = c.VerifyLog(); return e })
	return res, err
}
