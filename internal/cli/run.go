package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"golang.org/x/sys/unix"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/service"
)

func newRunCommand(opts *Options) *cobra.Command {
	cfg := service.Config{}
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run the sysmin service",
		Long: "run is what the service unit starts: it holds the vault, opens it with the credential the service\n" +
			"manager hands over (otherwise it stays locked until the passphrase arrives over the control\n" +
			"socket), and serves the control socket until it receives SIGTERM or SIGINT.",
		Args: noVaultArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			// Keeps other processes from reading the vault's key out of this process's memory.
			if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
				return fmt.Errorf("cannot protect the service's memory: %w", err)
			}
			ctx, stop := signal.NotifyContext(c.Context(), syscall.SIGTERM, syscall.SIGINT)
			defer stop()
			cfg.Version = version
			cfg.Resolver = opts.resolver
			return runService(ctx, cfg)
		},
	}
	cmd.Flags().StringVar(&cfg.ConfigDir, "config-dir", service.DefaultConfigDir, "configuration directory")
	cmd.Flags().StringVar(&cfg.StateDir, "state-dir", service.DefaultStateDir, "state directory holding the vault and the log")
	cmd.Flags().StringVar(&cfg.SocketPath, "socket", control.DefaultSocketPath, "control socket path")
	return cmd
}

// runService serves until ctx ends. Signals and process hardening belong to the caller.
func runService(ctx context.Context, cfg service.Config) error {
	s, err := service.Open(cfg)
	if err != nil {
		return fmt.Errorf("start the service: %w", err)
	}
	if err := s.Listen(); err != nil {
		return fmt.Errorf("start the service: %w", err)
	}
	slog.Info("sysmin service started", "socket", cfg.SocketPath, "pid", os.Getpid())
	return s.Serve(ctx)
}
