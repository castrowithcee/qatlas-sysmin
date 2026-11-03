package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/castrowithcee/qatlas-sysmin/internal/selfupdate"
)

// Installation layout. The release archive and the installer put the files here.
const (
	serviceName  = "qsysmin"
	unitPath     = "/etc/systemd/system/qsysmin.service"
	stateDataDir = "/var/lib/qsysmin"
)

// system holds everything update and uninstall take from the host, so that tests run without root, systemd,
// or real paths.
type system struct {
	euid func() int
	// command runs an external program and returns its failure, which carries ExitCode() for a program
	// that ended with a status.
	command func(ctx context.Context, name string, args ...string) error
	// updater builds the release client; apiURL is the --api-url value, empty for GitHub.
	updater    func(version, apiURL string) *selfupdate.Client
	binaryPath string
	unitPath   string
	// dataDirs are the directories uninstall --purge removes.
	dataDirs []string
}

func defaultSystem() system {
	return system{
		euid: os.Geteuid,
		command: func(ctx context.Context, name string, args ...string) error {
			out, err := exec.CommandContext(ctx, name, args...).CombinedOutput()
			if err != nil {
				if msg := strings.TrimSpace(string(out)); msg != "" {
					return fmt.Errorf("%w: %s", err, msg)
				}
			}
			return err
		},
		updater:    func(version, apiURL string) *selfupdate.Client { return selfupdate.New(version, apiURL) },
		binaryPath: selfupdate.DefaultTarget,
		unitPath:   unitPath,
		dataDirs:   []string{stateDataDir},
	}
}

func (o *Options) system() system {
	if o.sys != nil {
		return *o.sys
	}
	return defaultSystem()
}

func requireRoot(s system, action string) error {
	if s.euid() != 0 {
		return rootRequired(action)
	}
	return nil
}

type exitCoder interface{ ExitCode() int }

// exitedWith reports whether err is a program's exit status equal to code.
func exitedWith(err error, code int) bool {
	var ec exitCoder
	return errors.As(err, &ec) && ec.ExitCode() == code
}
