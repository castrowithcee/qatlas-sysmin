package tui

import (
	"errors"
	"os"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/redact"
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
	// PrefsPath is the file Settings writes the preferences to; empty means they cannot be saved.
	PrefsPath string
	// Version is the running program. Releases looks up the releases for Settings; nil offers no lookup.
	Version  string
	Releases ReleaseCheck
	In, Out  *os.File
}

// ErrRegroup is what Run returns when the person asked to restart the interface with the group sysmin; the
// caller starts it again under sudo -g sysmin.
var ErrRegroup = errors.New("restart the interface under sudo -g sysmin")

// UpdateRequest is what Run returns when the person asked to install a newer release of sysmin; the caller
// installs it and starts the interface again.
type UpdateRequest struct{ Version string }

func (r *UpdateRequest) Error() string { return "install sysmin " + r.Version }

// Run starts the editor on the terminal streams of opts and returns when the person quits.
func Run(opts Options) error {
	prefs := opts.Prefs
	if prefs == (uiprefs.Prefs{}) {
		prefs = uiprefs.Default()
	}
	setProfile(detectProfile(opts.Out))
	known, themeWarning := knownTheme(prefs.Theme)
	prefs.Theme = known
	applyTheme(prefs.Theme)
	setLanguage(prefs.Language)
	warnings := opts.PrefsWarnings
	if themeWarning != "" {
		warnings = append(warnings, themeWarning)
	}
	if err := prefs.Keys.WithDefaults().Validate(); err != nil {
		warnings = append(warnings, "keys: "+err.Error()+", using the standard keys")
		prefs.Keys = uiprefs.DefaultKeys()
	}
	applyKeys(prefs.Keys)
	model := New(prefs, opts.Redactor)
	model.prefsPath, model.version, model.releases = opts.PrefsPath, opts.Version, opts.Releases
	if opts.Backend != nil {
		model.enableAreas(opts.Backend)
	}
	if len(warnings) > 0 {
		model.status = "warning: " + warnings[0]
	}
	fragments := newFragmentFilter(time.Now)
	program := tea.NewProgram(model, tea.WithInput(opts.In), tea.WithOutput(legacyKeys{opts.Out}),
		tea.WithFilter(fragments.filter), tea.WithColorProfile(toColorprofile(currentProfile())))
	fragments.send = program.Send
	final, err := program.Run()
	if fm, ok := final.(*Model); ok && err == nil {
		switch {
		case fm.regroupAsked:
			return ErrRegroup
		case fm.updateTo != "":
			return &UpdateRequest{fm.updateTo}
		}
	}
	return err
}
