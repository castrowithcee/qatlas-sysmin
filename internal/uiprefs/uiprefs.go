// Package uiprefs keeps the device-local preferences of the terminal editor in tui.yaml in the per-user
// folder ~/.qatlas/sysmin, apart from the service configuration: the file is never part of an export or
// import and needs no admin session. A missing or damaged file yields the defaults and a warning, never an
// error that stops the editor. The last writer wins.
//
// The format is a flat YAML mapping. Keys this version does not know are ignored on read, so later
// versions can add fields without breaking an older reader; a write by this version writes only the keys
// it knows.
package uiprefs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"go.yaml.in/yaml/v3"
)

// FileName is the name of the preferences file inside the configuration directory.
const FileName = "tui.yaml"

// Values of Prefs.Theme and Prefs.Icons. Whether another name is a theme is the editor's to decide; this
// package only keeps the text.
const (
	ThemeTerminal    = "terminal"
	ThemeSysminDark  = "sysmin dark"
	ThemeSysminLight = "sysmin light"

	IconsNone     = "none"
	IconsNerdFont = "nerdfont"

	// The language of the editor's own texts; system follows the language of the device.
	LanguageEnglish = "english"
	LanguageGerman  = "deutsch"
	LanguageSystem  = "system"
)

// The values a form offers for the mouse row; off is Prefs.MouseOff.
const (
	ChoiceMouseOn  = "on"
	ChoiceMouseOff = "off"
)

// DefaultStart is the area the editor opens on by default. Whether another name is an area is the editor's
// to decide; this package only keeps the text.
const DefaultStart = "overview"

// formerThemes are the values of the former colour scheme and the themes they stand for now.
var formerThemes = map[string]string{"auto": ThemeTerminal, "light": ThemeSysminLight, "dark": ThemeSysminDark}

// Prefs are the device-local preferences.
type Prefs struct {
	Theme string `yaml:"theme"`
	Start string `yaml:"start"`
	Icons string `yaml:"icons"`
	// Language is empty in a Prefs built without it; every reader takes that as LanguageEnglish.
	Language string `yaml:"language"`
	// MouseOff switches the mouse off; the zero value keeps it on, so a Prefs built without it has it on.
	MouseOff bool `yaml:"-"`
	Keys     Keys `yaml:"-"`
}

// MouseEnabled reports whether the editor takes mouse input.
func (p Prefs) MouseEnabled() bool { return !p.MouseOff }

// Default returns the preferences of a device that has none.
func Default() Prefs {
	return Prefs{Theme: ThemeTerminal, Start: DefaultStart, Icons: IconsNone, Language: LanguageEnglish, Keys: DefaultKeys()}
}

// Path returns the preferences file that belongs to the configuration directory dir.
func Path(dir string) string { return filepath.Join(dir, FileName) }

// UserDir returns the per-user sysmin folder, ~/.qatlas/sysmin.
func UserDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".qatlas", "sysmin"), nil
}

// Migrate moves the preferences file of an earlier version from oldPath to newPath. It does nothing when
// newPath exists or oldPath does not; an existing new file is never overwritten. On a failure the old file
// stays in place. The result says what happened, empty when nothing did.
func Migrate(newPath, oldPath string) string {
	if _, err := os.Lstat(newPath); err == nil {
		return ""
	}
	info, err := os.Lstat(oldPath)
	if err != nil {
		return ""
	}
	if err := os.MkdirAll(filepath.Dir(newPath), 0o700); err != nil {
		return fmt.Sprintf("cannot move %s to %s, leaving it in place: %v", oldPath, newPath, err)
	}
	// A symlinked file, say from a dotfiles repository, keeps its target: the new path links to it too.
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := filepath.EvalSymlinks(oldPath)
		if err == nil {
			err = os.Symlink(target, newPath)
		}
		if err == nil {
			err = os.Remove(oldPath)
			if err != nil {
				_ = os.Remove(newPath)
			}
		}
		if err != nil {
			return fmt.Sprintf("cannot move %s to %s, leaving it in place: %v", oldPath, newPath, err)
		}
		return fmt.Sprintf("moved %s to %s", oldPath, newPath)
	}
	if err := os.Rename(oldPath, newPath); err == nil {
		_ = os.Chmod(newPath, 0o600)
		return fmt.Sprintf("moved %s to %s", oldPath, newPath)
	}
	if err := copyNew(oldPath, newPath); err != nil {
		return fmt.Sprintf("cannot move %s to %s, leaving it in place: %v", oldPath, newPath, err)
	}
	if err := os.Remove(oldPath); err != nil {
		return fmt.Sprintf("copied %s to %s, the old file stays: %v", oldPath, newPath, err)
	}
	return fmt.Sprintf("moved %s to %s", oldPath, newPath)
}

// copyNew copies src to dst with mode 0600 and fails if dst exists. A partial copy is removed.
func copyNew(src, dst string) (err error) {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.Remove(dst)
		}
	}()
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Load reads the preferences at path. It never fails: a missing file gives the defaults without a warning;
// an unreadable or malformed file, or a value that is not valid, gives the default for that part and a
// warning that names it.
func Load(path string) (Prefs, []string) {
	prefs := Default()
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return prefs, nil
		}
		return prefs, []string{fmt.Sprintf("cannot read %s, using the defaults: %v", path, err)}
	}
	var raw struct {
		Theme    *string `yaml:"theme"`
		Start    *string `yaml:"start"`
		Icons    *string `yaml:"icons"`
		Language *string `yaml:"language"`
		Mouse    *string `yaml:"mouse"`
		Keys     *struct {
			Info    *string `yaml:"info"`
			Save    *string `yaml:"save"`
			Help    *string `yaml:"help"`
			Command *string `yaml:"command"`
			Lock    *string `yaml:"lock"`
		} `yaml:"keys"`
	}
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return prefs, []string{fmt.Sprintf("cannot parse %s, using the defaults: %v", path, err)}
	}
	var warnings []string
	if raw.Theme != nil {
		theme, former := formerThemes[*raw.Theme]
		switch {
		case former:
			prefs.Theme = theme
		case *raw.Theme == "":
			warnings = append(warnings, fmt.Sprintf("%s: theme is empty, using %s", path, ThemeTerminal))
		default:
			prefs.Theme = *raw.Theme
		}
	}
	if raw.Start != nil {
		if *raw.Start == "" {
			warnings = append(warnings, fmt.Sprintf("%s: start is empty, using %s", path, DefaultStart))
		} else {
			prefs.Start = *raw.Start
		}
	}
	if raw.Icons != nil {
		switch *raw.Icons {
		case IconsNone, IconsNerdFont:
			prefs.Icons = *raw.Icons
		default:
			warnings = append(warnings, fmt.Sprintf("%s: icons %q is not none or nerdfont, using %s",
				path, *raw.Icons, IconsNone))
		}
	}
	if raw.Language != nil {
		switch *raw.Language {
		case LanguageEnglish, LanguageGerman, LanguageSystem:
			prefs.Language = *raw.Language
		default:
			warnings = append(warnings, fmt.Sprintf("%s: language %q is not english, deutsch or system, using %s",
				path, *raw.Language, LanguageEnglish))
		}
	}
	if raw.Mouse != nil {
		switch *raw.Mouse {
		case "true":
		case "false":
			prefs.MouseOff = true
		default:
			warnings = append(warnings, fmt.Sprintf("%s: mouse %q is not true or false, using true", path, *raw.Mouse))
		}
	}
	if raw.Keys != nil {
		prefs.Keys, warnings = loadKeys(path, map[string]*string{
			ActionInfo: raw.Keys.Info, ActionSave: raw.Keys.Save, ActionHelp: raw.Keys.Help,
			ActionCommand: raw.Keys.Command, ActionLock: raw.Keys.Lock,
		}, warnings)
	}
	return prefs, warnings
}

// Save writes prefs to path through a temporary file and a rename, so a reader sees the old file or the new
// one, never half of one. A value that is not valid is refused and nothing is written.
func Save(path string, prefs Prefs) error {
	if prefs.Theme == "" {
		return errors.New("theme must not be empty")
	}
	switch prefs.Icons {
	case IconsNone, IconsNerdFont:
	default:
		return fmt.Errorf("icons %q is not none or nerdfont", prefs.Icons)
	}
	switch prefs.Language {
	case "":
		prefs.Language = LanguageEnglish
	case LanguageEnglish, LanguageGerman, LanguageSystem:
	default:
		return fmt.Errorf("language %q is not english, deutsch or system", prefs.Language)
	}
	if prefs.Start == "" {
		return errors.New("start must not be empty")
	}
	keys := prefs.Keys.WithDefaults()
	if err := keys.Validate(); err != nil {
		return err
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(struct {
		Prefs `yaml:",inline"`
		Mouse bool      `yaml:"mouse"`
		Keys  *keysFile `yaml:"keys,omitempty"`
	}{prefs, prefs.MouseEnabled(), keys.file()}); err != nil {
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}

	// A file kept in a dotfiles directory may be a symlink; the real file is the target.
	target := path
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		target = resolved
	}
	dir := filepath.Dir(target)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".sysmin-tui-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write next to %s: %w", target, err)
	}
	name := tmp.Name()
	moved := false
	defer func() {
		if !moved {
			_ = os.Remove(name)
		}
	}()
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("cannot write %s: %w", name, err)
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return fmt.Errorf("cannot set the permissions of %s: %w", name, err)
	}
	if err := os.Rename(name, target); err != nil {
		return fmt.Errorf("cannot replace %s: %w", target, err)
	}
	moved = true
	return nil
}
