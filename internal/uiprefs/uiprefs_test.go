package uiprefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFileGivesDefaultsWithoutWarning(t *testing.T) {
	got, warnings := Load(Path(t.TempDir()))
	if got != Default() || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v; want defaults and no warning", got, warnings)
	}
}

func TestLoadDamagedFileGivesDefaultsAndWarning(t *testing.T) {
	path := Path(t.TempDir())
	if err := os.WriteFile(path, []byte("theme: [unclosed\n\t:"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warnings := Load(path)
	if got != Default() || len(warnings) != 1 || !strings.Contains(warnings[0], "cannot parse") {
		t.Fatalf("Load() = %+v, %v; want defaults and one parse warning", got, warnings)
	}
}

func TestLoadUnreadablePathGivesDefaultsAndWarning(t *testing.T) {
	// A directory in place of the file cannot be read.
	path := Path(t.TempDir())
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	got, warnings := Load(path)
	if got != Default() || len(warnings) != 1 {
		t.Fatalf("Load() = %+v, %v; want defaults and one warning", got, warnings)
	}
}

func TestLoadInvalidValuesFallBackPerField(t *testing.T) {
	path := Path(t.TempDir())
	data := "theme: \"\"\nstart: connections\nicons: emoji\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warnings := Load(path)
	want := Default()
	want.Start = "connections"
	if got != want || len(warnings) != 2 {
		t.Fatalf("Load() = %+v, %v; want %+v and two warnings", got, warnings, want)
	}
}

func TestLoadToleratesUnknownKeys(t *testing.T) {
	path := Path(t.TempDir())
	data := "theme: dark\nmouse: true\nfont: x\n"
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warnings := Load(path)
	if got.Theme != ThemeSysminDark || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v; want theme sysmin dark and no warning", got, warnings)
	}
}

func TestSaveRoundTripLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	want := Prefs{Theme: ThemeSysminLight, Start: "access", Icons: IconsNerdFont, Language: LanguageGerman, Keys: DefaultKeys()}
	if err := Save(Path(dir), want); err != nil {
		t.Fatal(err)
	}
	got, warnings := Load(Path(dir))
	if got != want || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v; want %+v", got, warnings, want)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != FileName {
		t.Fatalf("directory holds %v; want only %s", entries, FileName)
	}
}

func TestSaveReplacesWholeFileAndLastWriterWins(t *testing.T) {
	path := Path(t.TempDir())
	if err := os.WriteFile(path, []byte("theme: dark\nstart: library\nicons: nerdfont\nextra: x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Prefs{Theme: ThemeSysminLight, Start: "overview", Icons: IconsNone}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "theme: sysmin light\nstart: overview\nicons: none\nlanguage: english\nmouse: true\n" {
		t.Fatalf("file = %q", got)
	}
}

func TestSaveInvalidWritesNothing(t *testing.T) {
	dir := t.TempDir()
	for _, bad := range []Prefs{
		{Theme: "", Start: "overview", Icons: IconsNone},
		{Theme: ThemeTerminal, Start: "", Icons: IconsNone},
		{Theme: ThemeTerminal, Start: "overview", Icons: "x"},
		{Theme: ThemeTerminal, Start: "overview", Icons: IconsNone, Language: "fr"},
	} {
		if err := Save(Path(dir), bad); err == nil {
			t.Errorf("Save(%+v) = nil; want an error", bad)
		}
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("directory holds %v; want it empty", entries)
	}
}

func TestSaveFailedRenameLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	// A non-empty directory in place of the file makes the rename fail.
	target := Path(dir)
	if err := os.MkdirAll(filepath.Join(target, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Save(target, Default()); err == nil {
		t.Fatal("Save() = nil; want an error")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 || entries[0].Name() != FileName {
		t.Fatalf("directory holds %v; want no temporary file", entries)
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	path := Path(filepath.Join(t.TempDir(), "new", "dir"))
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}

func TestLanguageLoadsAndFallsBack(t *testing.T) {
	path := Path(t.TempDir())
	if err := os.WriteFile(path, []byte("language: system\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, warnings := Load(path); got.Language != LanguageSystem || len(warnings) != 0 {
		t.Fatalf("Load() = %+v, %v; want language system", got, warnings)
	}
	if err := os.WriteFile(path, []byte("language: de\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warnings := Load(path)
	if got.Language != LanguageEnglish || len(warnings) != 1 {
		t.Fatalf("Load() = %+v, %v; want english and one warning", got, warnings)
	}
	if Default().Language != LanguageEnglish {
		t.Fatalf("default language = %q, want english", Default().Language)
	}
}

func TestMouseIsOnByDefaultAndSurvivesARoundTrip(t *testing.T) {
	if !Default().MouseEnabled() || !(Prefs{}).MouseEnabled() {
		t.Fatal("the mouse is not on in the defaults")
	}
	path := Path(t.TempDir())
	off := Default()
	off.MouseOff = true
	if err := Save(path, off); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); !strings.Contains(string(data), "mouse: false\n") {
		t.Fatalf("file = %q, want mouse: false", data)
	}
	got, warnings := Load(path)
	if len(warnings) != 0 || got.MouseEnabled() {
		t.Fatalf("Load() = %+v, %v; want the mouse off and no warning", got, warnings)
	}
	if err := Save(path, Default()); err != nil {
		t.Fatal(err)
	}
	if got, warnings := Load(path); len(warnings) != 0 || !got.MouseEnabled() {
		t.Fatalf("Load() = %+v, %v; want the mouse on and no warning", got, warnings)
	}
}

func TestLoadWarnsForAMouseValueThatIsNoBoolean(t *testing.T) {
	path := Path(t.TempDir())
	if err := os.WriteFile(path, []byte("mouse: sometimes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, warnings := Load(path)
	if len(warnings) != 1 || !got.MouseEnabled() {
		t.Fatalf("Load() = %+v, %v; want the mouse on and one warning", got, warnings)
	}
}

// A file of an earlier version names a colour scheme and maybe a background: the scheme becomes its theme, and
// the background is dropped without a word and not written again.
func TestFormerColourSchemeBecomesTheme(t *testing.T) {
	for _, tc := range []struct {
		file, want string
	}{
		{"theme: auto\n", ThemeTerminal},
		{"theme: light\n", ThemeSysminLight},
		{"theme: dark\n", ThemeSysminDark},
		{"start: overview\n", ThemeTerminal},
		{"theme: auto\nbackground: sysmin\n", ThemeTerminal},
		{"theme: light\nbackground: terminal\n", ThemeSysminLight},
		{"theme: dark\nbackground: sysmin\n", ThemeSysminDark},
		{"background: neon\n", ThemeTerminal},
		{"theme: sysmin light\n", ThemeSysminLight},
		{"theme: some later theme\n", "some later theme"},
	} {
		path := Path(t.TempDir())
		if err := os.WriteFile(path, []byte(tc.file), 0o600); err != nil {
			t.Fatal(err)
		}
		got, warnings := Load(path)
		if got.Theme != tc.want || len(warnings) != 0 {
			t.Errorf("Load(%q) = theme %q, %v; want %q and no warning", tc.file, got.Theme, warnings, tc.want)
			continue
		}
		if err := Save(path, got); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), "background") {
			t.Errorf("Save after Load(%q) still writes the background:\n%s", tc.file, data)
		}
	}
	if got, _ := Load(Path(t.TempDir())); got.Theme != ThemeTerminal {
		t.Errorf("without a file the theme is %q, want %s", got.Theme, ThemeTerminal)
	}
}

func TestMigrateMovesOldFile(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old", FileName)
	newPath := filepath.Join(root, "home", ".qatlas", "sysmin", FileName)
	if err := os.MkdirAll(filepath.Dir(oldPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(oldPath, []byte("theme: terminal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if msg := Migrate(newPath, oldPath); msg == "" {
		t.Error("want a message for the move")
	}
	if got, err := os.ReadFile(newPath); err != nil || string(got) != "theme: terminal\n" {
		t.Errorf("new file = %q, %v", got, err)
	}
	if _, err := os.Lstat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old file still there: %v", err)
	}
	if info, _ := os.Stat(filepath.Dir(newPath)); info.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %v", info.Mode().Perm())
	}
}

func TestMigrateKeepsExistingNewFile(t *testing.T) {
	root := t.TempDir()
	oldPath := filepath.Join(root, "old.yaml")
	newPath := filepath.Join(root, "new.yaml")
	for p, c := range map[string]string{oldPath: "old\n", newPath: "new\n"} {
		if err := os.WriteFile(p, []byte(c), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if msg := Migrate(newPath, oldPath); msg != "" {
		t.Errorf("message = %q, want none", msg)
	}
	if got, _ := os.ReadFile(oldPath); string(got) != "old\n" {
		t.Errorf("old file = %q", got)
	}
	if got, _ := os.ReadFile(newPath); string(got) != "new\n" {
		t.Errorf("new file = %q", got)
	}
}

func TestMigrateSymlinkKeepsTarget(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "dotfiles.yaml")
	oldPath := filepath.Join(root, "old.yaml")
	newPath := filepath.Join(root, "new", FileName)
	if err := os.WriteFile(target, []byte("theme: terminal\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("dotfiles.yaml", oldPath); err != nil {
		t.Fatal(err)
	}
	Migrate(newPath, oldPath)
	if got, err := os.Readlink(newPath); err != nil || got != target {
		t.Errorf("new link = %q, %v, want %q", got, err, target)
	}
	if _, err := os.Lstat(oldPath); !os.IsNotExist(err) {
		t.Errorf("old link still there: %v", err)
	}
	if prefs, w := Load(newPath); len(w) != 0 || prefs.Theme != ThemeTerminal {
		t.Errorf("prefs = %+v, warnings = %q", prefs, w)
	}
	if _, err := os.Stat(target); err != nil {
		t.Errorf("link target lost: %v", err)
	}
}
