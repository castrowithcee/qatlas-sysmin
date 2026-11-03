package tui

import (
	"errors"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-kit/redact"
	kittui "github.com/castrowithcee/qatlas-kit/tui"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostadmin"
	"github.com/castrowithcee/qatlas-sysmin/internal/hostchange"
	"github.com/castrowithcee/qatlas-sysmin/internal/uiprefs"
)

// Options is what Run needs to start the editor on a terminal.
type Options struct {
	// Prefs are the device-local preferences; the zero value is read as the defaults.
	Prefs uiprefs.Prefs
	// PrefsWarnings are problems found while loading Prefs; the editor shows the first one at start.
	PrefsWarnings []string
	// Redactor holds the secrets that must never reach the screen or the clipboard; nil knows none.
	Redactor *redact.Redactor
	// Backend serves the service interface. Nil runs the bare frame without a service.
	Backend Backend
	// Host is what the Host area reads; nil reads the local account databases without privilege.
	Host HostSource
	// PrefsPath is the file Settings writes the preferences to; empty means they cannot be saved.
	PrefsPath string
	// Version is the running program. Releases looks up the releases for Settings; nil offers no lookup.
	Version  string
	Releases ReleaseCheck
	In, Out  *os.File
	// HostRun builds the sudo call that runs a host change, an undo or a root-only read; nil offers none and
	// every attempt says so. HostActions describes the actions; nil means hostchange.Default().
	HostRun     HostRunner
	HostActions *hostchange.Registry
}

// ErrRegroup is what Run returns when the person asked to restart the interface with the group qsysmin; the
// caller starts it again under sudo -g qsysmin.
var ErrRegroup = errors.New("restart the interface under sudo -g qsysmin")

// UpdateRequest is what Run returns when the person asked to install a newer release of qsysmin; the caller
// installs it and starts the interface again.
type UpdateRequest struct{ Version string }

func (r *UpdateRequest) Error() string { return "install qsysmin " + r.Version }

// Run starts the editor on the terminal streams of opts and returns when the person quits.
//
// With a Backend, Run asks the service for its status before it draws anything. A caller without access gets
// client.ErrReserved or client.ErrNotEnabled back and the terminal stays untouched. If the service takes the
// access away while the interface runs, the interface quits and Run returns the same error.
func Run(opts Options) error {
	if opts.Backend != nil {
		if _, err := opts.Backend.Status(); isDenied(err) {
			return err
		}
	}
	prefs := opts.Prefs
	if prefs == (uiprefs.Prefs{}) {
		prefs = uiprefs.Default()
	}
	setProfile(detectProfile(opts.Out))
	known, themeWarning := knownTheme(prefs.Theme)
	prefs.Theme = known
	applyTheme(prefs.Theme)
	warnings := opts.PrefsWarnings
	if themeWarning != "" {
		warnings = append(warnings, themeWarning)
	}
	model := newModel(prefs, opts.Redactor, kittui.DetectCharsetMode(os.Environ()))
	warnings = append(warnings, model.bindKeys()...)
	model.prefsPath, model.version, model.releases = opts.PrefsPath, opts.Version, opts.Releases
	model.hostSrc = opts.Host
	model.hostRun, model.hostReg = opts.HostRun, opts.HostActions
	model.setHostTerminal(opts.In, opts.Out)
	if model.hostSrc == nil {
		model.hostSrc = hostadmin.NewHost(false)
	}
	if opts.Backend != nil {
		model.enableAreas(opts.Backend)
	}
	if len(warnings) > 0 {
		model.status = "warning: " + warnings[0]
	}
	fragments := newFragmentFilter(time.Now)
	program := tea.NewProgram(model, tea.WithInput(opts.In), tea.WithOutput(newLegacyKeys(opts.Out)),
		tea.WithFilter(fragments.filter), tea.WithColorProfile(toColorprofile(currentProfile())))
	fragments.send = program.Send
	final, err := program.Run()
	if fm, ok := final.(*Model); ok && err == nil {
		switch {
		case fm.denied != nil:
			return fm.denied
		case fm.regroupAsked:
			return ErrRegroup
		case fm.updateTo != "":
			return &UpdateRequest{fm.updateTo}
		}
	}
	return err
}
