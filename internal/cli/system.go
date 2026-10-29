package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/selfupdate"
)

// Installation layout. The release archive and the installer put the files here.
const (
	serviceName   = "sysmin"
	unitPath      = "/etc/systemd/system/sysmin.service"
	configDataDir = "/etc/sysmin"
	stateDataDir  = "/var/lib/sysmin"
)

// system holds everything update and uninstall take from the host, so that tests run without root, systemd,
// or real paths.
type system struct {
	euid func() int
	// interactive reports whether in is a terminal a person can answer on.
	interactive func(in io.Reader) bool
	// command runs an external program and returns its failure, which carries ExitCode() for a program
	// that ended with a status.
	command func(ctx context.Context, name string, args ...string) error
	// updater builds the release client; releaseURL is the --release-url value, empty for GitHub.
	updater    func(version, releaseURL string) *selfupdate.Client
	binaryPath string
	unitPath   string
	// dataDirs are the directories uninstall --purge removes.
	dataDirs []string
}

func defaultSystem() system {
	return system{
		euid:        os.Geteuid,
		interactive: readerIsTerminal,
		command: func(ctx context.Context, name string, args ...string) error {
			out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
			if err != nil {
				if msg := strings.TrimSpace(string(out)); msg != "" {
					return fmt.Errorf("%w: %s", err, msg)
				}
			}
			return err
		},
		updater:    func(version, releaseURL string) *selfupdate.Client { return selfupdate.New(version, releaseURL) },
		binaryPath: selfupdate.DefaultTarget,
		unitPath:   unitPath,
		dataDirs:   []string{configDataDir, stateDataDir},
	}
}

func (o *Options) system() system {
	if o.sys != nil {
		return *o.sys
	}
	return defaultSystem()
}

func readerIsTerminal(in io.Reader) bool {
	f, ok := in.(*os.File)
	return ok && isTerminal(f)
}

func requireRoot(s system, action string) error {
	if s.euid() != 0 {
		return fmt.Errorf("%s must run as root; use sudo", action)
	}
	return nil
}

// requireConfirmation fails before anything is done when there is neither --yes nor a terminal to ask on.
func requireConfirmation(s system, in io.Reader, yes bool, action string) error {
	if !yes && !s.interactive(in) {
		return &UsageError{fmt.Errorf("%s needs confirmation: run it on a terminal or pass --yes", action)}
	}
	return nil
}

// confirm asks on a terminal; with --yes it only prints the question's subject.
func confirm(c interface {
	InOrStdin() io.Reader
	ErrOrStderr() io.Writer
}, yes bool, question string) (bool, error) {
	w := c.ErrOrStderr()
	if yes {
		fmt.Fprintln(w, question)
		return true, nil
	}
	fmt.Fprintf(w, "%s [y/N] ", question)
	line, err := bufio.NewReader(c.InOrStdin()).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, err
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	return answer == "y" || answer == "yes", nil
}

type exitCoder interface{ ExitCode() int }

// exitedWith reports whether err is a program's exit status equal to code.
func exitedWith(err error, code int) bool {
	var ec exitCoder
	return errors.As(err, &ec) && ec.ExitCode() == code
}
