package tui

import (
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
	In, Out  *os.File
}

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
	if len(warnings) > 0 {
		model.status = "warning: " + warnings[0]
	}
	fragments := newFragmentFilter(time.Now)
	program := tea.NewProgram(model, tea.WithInput(opts.In), tea.WithOutput(legacyKeys{opts.Out}),
		tea.WithFilter(fragments.filter), tea.WithColorProfile(toColorprofile(currentProfile())))
	fragments.send = program.Send
	_, err := program.Run()
	return err
}
