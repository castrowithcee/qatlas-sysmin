// Package client is how the command line and the terminal interface talk to the service: over its control
// socket and nowhere else.
package client

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// DialTimeout bounds connecting to the service; CallTimeout bounds one call.
const (
	DialTimeout = 5 * time.Second
	CallTimeout = 30 * time.Second
)

// Client is one connection to the service. It is not safe for concurrent calls.
type Client struct {
	conn net.Conn
	br   *bufio.Reader
}

// Dial connects to the control socket at path; an empty path means control.DefaultSocketPath.
func Dial(path string) (*Client, error) {
	if path == "" {
		path = control.DefaultSocketPath
	}
	conn, err := net.DialTimeout("unix", path, DialTimeout)
	if err != nil {
		return nil, fmt.Errorf("cannot reach the service: %w", err)
	}
	return &Client{conn: conn, br: bufio.NewReader(conn)}, nil
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
	}
	_ = c.conn.SetDeadline(time.Now().Add(CallTimeout))
	if err := control.WriteLine(c.conn, req); err != nil {
		return fmt.Errorf("cannot send the request: %w", err)
	}
	line, err := control.ReadLine(c.br, control.MaxResponseBytes)
	if err != nil {
		return fmt.Errorf("cannot read the answer: %w", err)
	}
	var resp control.Response
	if err := json.Unmarshal(line, &resp); err != nil || resp.V != control.Version {
		return errors.New("the service answered in a form this program does not understand")
	}
	if !resp.OK {
		return &control.Error{Code: resp.Error}
	}
	if result != nil && resp.Result != nil {
		if err := json.Unmarshal(resp.Result, result); err != nil {
			return fmt.Errorf("cannot decode the result: %w", err)
		}
	}
	return nil
}

// Status asks for the service's protocol and program version.
func (c *Client) Status() (control.StatusResult, error) {
	var out control.StatusResult
	err := c.Call(control.OpStatus, nil, &out)
	return out, err
}
