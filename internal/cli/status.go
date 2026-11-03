package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/castrowithcee/qatlas-kit/output"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// withService runs fn on a connection to the service at socket. Every failure is a diagnostic with a code and
// the step to take.
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

// accessError turns a caller without access into its diagnostic.
func accessError(err error) error {
	switch {
	case errors.Is(err, client.ErrReserved):
		return client.ErrReserved
	case errors.Is(err, client.ErrNotEnabled):
		return client.ErrNotEnabled
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
		return failure(codePeerMismatch, err, "Stop the process as the message says, then repeat the request.",
			"Tell the user which process answers at the socket; do not stop it yourself.")
	}
	if errors.Is(err, client.ErrSessionGroup) {
		return failure(codeSessionGroup, err,
			fmt.Sprintf("Log in again, or put \"sudo -g %s\" in front of the command.", control.ServiceGroup), askUserReport)
	}
	return failure(output.CodeUnreachable, err,
		"Check that the service is running and that you are root or in the group qsysmin.",
		"Tell the user the service is unreachable; do not start or restart it.")
}

func newStatusCommand(opts *Options) *cobra.Command {
	var socket string
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the state of the running service",
		RunE: func(c *cobra.Command, _ []string) error {
			var st control.StatusResult
			err := withService(socket, func(cl *client.Client) (err error) {
				st, err = cl.Status()
				return err
			})
			if err != nil {
				return err
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
