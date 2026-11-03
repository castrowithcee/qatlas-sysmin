package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/output"
)

// withService runs fn on a connection to the service at socket. Every failure is a runtime error with a
// message that says what to check.
func withService(socket string, fn func(*client.Client) error) error {
	c, err := client.Dial(socket)
	if err != nil {
		return dialError(err)
	}
	defer c.Close()
	err = fn(c)
	if errors.Is(err, client.ErrSessionGroup) {
		return dialError(err)
	}
	return accessError(err)
}

// accessError turns a caller without access into the one line that says so, printed without a prefix.
func accessError(err error) error {
	switch {
	case errors.Is(err, client.ErrReserved):
		return &plainError{client.ReservedMessage}
	case errors.Is(err, client.ErrNotEnabled):
		return &plainError{client.NotEnabledMessage}
	}
	return err
}

// dialError says what to check when the service cannot be reached.
func dialError(err error) error {
	if denied := accessError(err); denied != err {
		return denied
	}
	var pe *client.PeerError
	if errors.As(err, &pe) {
		return err // it names the process and the next step itself
	}
	if errors.Is(err, client.ErrSessionGroup) {
		return fmt.Errorf("%w; log in again, or put \"sudo -g %s\" in front of the command", err, control.ServiceGroup)
	}
	return fmt.Errorf("%w (is the service running, and are you root or in the group qsysmin?)", err)
}

func newStatusCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the state of the running service",
		Args:  noArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			var st control.StatusResult
			err := withService(socket, func(cl *client.Client) (err error) {
				st, err = cl.Status()
				return err
			})
			if err != nil {
				return fmt.Errorf("status: %w", err)
			}
			return emit(c, opts, output.Object{Fields: []output.Field{
				{Name: "version", Value: st.Version},
				{Name: "socket", Value: st.Socket},
			}})
		},
	}
	cmd.Flags().StringVar(&socket, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}
