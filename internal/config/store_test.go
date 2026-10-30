package config

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

var exampleNames = []string{"image-project", "stack-override", "website-git"}

// copyTree copies the regular files below src into dst.
func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dst, rel), data, 0o640)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func exampleCopy(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	copyTree(t, filepath.Join("..", "..", "examples", name), dir)
	return dir
}

func readAll(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && d.Name() != lockFile {
			data, _ := os.ReadFile(p)
			rel, _ := filepath.Rel(root, p)
			out[rel] = string(data)
		}
		return nil
	})
	return out
}

func saveAll(t *testing.T, st *Store, m *Model, revs Revisions) {
	t.Helper()
	if _, err := st.SaveSettings(m.Settings, revs[SettingsPath()]); err != nil {
		t.Fatal(err)
	}
	for _, p := range m.Projects {
		if _, err := st.SaveProject(p, revs[ProjectPath(p.Name)]); err != nil {
			t.Fatal(err)
		}
	}
	for _, tpl := range m.Templates {
		if _, err := st.SaveTemplate(tpl, revs[TemplatePath(tpl.Name)]); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoreRoundTripIsStable(t *testing.T) {
	for _, name := range exampleNames {
		t.Run(name, func(t *testing.T) {
			dir := exampleCopy(t, name)
			st := NewStore(dir)
			m1, revs, err := st.Load()
			if err != nil {
				t.Fatal(err)
			}
			saveAll(t, st, m1, revs)
			first := readAll(t, dir)

			m2, revs, err := st.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(m1, m2) {
				t.Fatal("load, save, load changed the model")
			}
			saveAll(t, st, m2, revs)
			if !reflect.DeepEqual(first, readAll(t, dir)) {
				t.Fatal("saving again changed the bytes")
			}
			// Saving unchanged content keeps the revision the load reported.
			_, revs3, _ := st.Load()
			if !reflect.DeepEqual(revs, revs3) {
				t.Fatal("revisions differ")
			}
		})
	}
}

func TestStoreAtomicWriteLeavesNoTempFiles(t *testing.T) {
	dir := exampleCopy(t, "website-git")
	st := NewStore(dir)
	m, revs, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	saveAll(t, st, m, revs)
	p := m.Projects[0]
	p.Name = "second-site"
	if _, err := st.SaveProject(p, RevisionAbsent); err != nil {
		t.Fatal(err)
	}
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if strings.Contains(d.Name(), "tmp") {
			t.Errorf("leftover temporary file %s", path)
		}
		if !d.IsDir() {
			if info, _ := d.Info(); info.Mode().Perm() != 0o640 {
				t.Errorf("%s has mode %v, want 0640", path, info.Mode().Perm())
			}
		}
		return nil
	})
	info, err := os.Stat(filepath.Join(dir, ProjectsDir))
	if err != nil || info.Mode().Perm() != 0o750 {
		t.Errorf("projects dir: %v %v", info, err)
	}
	if _, _, err := st.Load(); err != nil {
		t.Fatal(err)
	}
}

func TestStoreDetectsStaleRevision(t *testing.T) {
	dir := exampleCopy(t, "website-git")
	a, b := NewStore(dir), NewStore(dir)
	ma, ra, err := a.Load()
	if err != nil {
		t.Fatal(err)
	}
	mb, rb, err := b.Load()
	if err != nil {
		t.Fatal(err)
	}
	pa, pb := ma.Projects[0], mb.Projects[0]
	path := ProjectPath(pa.Name)
	pa.Values = map[string]string{"first": "yes"}
	pb.Values = map[string]string{"second": "yes"}

	if _, err := a.SaveProject(pa, ra[path]); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(filepath.Join(dir, path))
	_, err = b.SaveProject(pb, rb[path])
	var ce *ConflictError
	if !errors.Is(err, ErrConflict) || !errors.As(err, &ce) {
		t.Fatalf("want conflict, got %v", err)
	}
	after, _ := os.ReadFile(filepath.Join(dir, path))
	if !bytes.Equal(before, after) || !strings.Contains(string(after), "first") {
		t.Fatal("the conflicting write changed the file")
	}
	if err := b.DeleteProject(pa.Name, rb[path]); !errors.Is(err, ErrConflict) {
		t.Fatalf("delete with a stale revision: %v", err)
	}
	// Creating over an existing file is a conflict too; the settings revision is independent of projects.
	if _, err := b.SaveProject(pb, RevisionAbsent); !errors.Is(err, ErrConflict) {
		t.Fatalf("create over existing: %v", err)
	}
	if _, err := b.SaveSettings(mb.Settings, rb[SettingsPath()]); err != nil {
		t.Fatalf("unrelated file conflicted: %v", err)
	}
	// Reload and delete.
	_, rb, _ = b.Load()
	if err := b.DeleteProject(pa.Name, rb[path]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, path)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("project file still exists")
	}
	if err := b.DeleteProject(pa.Name, RevisionAbsent); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("delete of absent file: %v", err)
	}
}

func TestStoreRejectsBadNames(t *testing.T) {
	st := NewStore(t.TempDir())
	if _, err := st.SaveProject(Project{Name: "../evil"}, RevisionAbsent); err == nil {
		t.Fatal("accepted a path as project name")
	}
	if err := st.DeleteTemplate("a/b", RevisionAbsent); err == nil {
		t.Fatal("accepted a path as template name")
	}
}

// withSchema2 simulates a later schema whose first step renames env_file from its old key.
func withSchema2(t *testing.T) {
	t.Helper()
	oldVersion, oldSteps := schemaVersion, migrations
	schemaVersion = 2
	migrations = map[int]migration{1: func(doc *yaml.Node) error {
		for i := 0; i+1 < len(doc.Content); i += 2 {
			if doc.Content[i].Value == "legacy_env_file" {
				doc.Content[i].Value = "env_file"
			}
		}
		return nil
	}}
	t.Cleanup(func() { schemaVersion, migrations = oldVersion, oldSteps })
}

func planOf(t *testing.T, m *Model) string {
	t.Helper()
	plan, err := m.Resolve("example-site", []Target{{Version: "v1.3.0", PreviousVersion: "v1.2.0"}})
	if err != nil {
		t.Fatal(err)
	}
	return render(plan)
}

func TestStoreMigratesOlderSchema(t *testing.T) {
	ref, err := LoadPath(filepath.Join("..", "..", "examples", "website-git"))
	if err != nil {
		t.Fatal(err)
	}
	want := planOf(t, ref)

	dir := exampleCopy(t, "website-git")
	projFile := filepath.Join(dir, ProjectPath("example-site"))
	data, _ := os.ReadFile(projFile)
	legacy := []byte(strings.Replace(string(data), "env_file:", "legacy_env_file:", 1))
	if err := os.WriteFile(projFile, legacy, 0o640); err != nil {
		t.Fatal(err)
	}
	settingsOld, _ := os.ReadFile(filepath.Join(dir, SettingsFile))

	withSchema2(t)
	m, revs, err := NewStore(dir).Load()
	if err != nil {
		t.Fatal(err)
	}
	if got := planOf(t, m); got != want {
		t.Errorf("migration changed the resolved plan:\n%s\nwant:\n%s", got, want)
	}
	if m.Projects[0].Schema != 2 || m.Settings.Schema != 2 {
		t.Errorf("schemas not upgraded: %d %d", m.Projects[0].Schema, m.Settings.Schema)
	}
	onDisk, _ := os.ReadFile(projFile)
	if revs[ProjectPath("example-site")] != revisionOf(onDisk) || !strings.Contains(string(onDisk), "schema: 2") {
		t.Error("revision or content of the migrated file is wrong")
	}

	entries, err := os.ReadDir(filepath.Join(dir, backupDir))
	if err != nil || len(entries) != 2 {
		t.Fatalf("backups: %v %v", entries, err)
	}
	found := 0
	for _, e := range entries {
		got, _ := os.ReadFile(filepath.Join(dir, backupDir, e.Name()))
		switch {
		case strings.HasPrefix(e.Name(), "projects_example-site.yaml.v1."):
			found++
			if !bytes.Equal(got, legacy) {
				t.Error("project backup differs from the old bytes")
			}
		case strings.HasPrefix(e.Name(), "sysmin.yaml.v1."):
			found++
			if !bytes.Equal(got, settingsOld) {
				t.Error("settings backup differs from the old bytes")
			}
		}
	}
	if found != 2 {
		t.Errorf("backup names unexpected: %v", entries)
	}

	// A second load finds nothing to migrate.
	if _, _, err := NewStore(dir).Load(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(filepath.Join(dir, backupDir)); len(entries) != 2 {
		t.Error("second load created more backups")
	}
}

func TestStoreMissingMigrationStepChangesNothing(t *testing.T) {
	dir := exampleCopy(t, "website-git")
	before := readAll(t, dir)
	withSchema2(t)
	delete(migrations, 1)
	if _, _, err := NewStore(dir).Load(); err == nil {
		t.Fatal("load succeeded without a migration step")
	}
	if !reflect.DeepEqual(before, readAll(t, dir)) {
		t.Fatal("a failed migration changed the directory")
	}
}

func TestStoreLeavesNewerSchemaUntouched(t *testing.T) {
	dir := exampleCopy(t, "website-git")
	projFile := filepath.Join(dir, ProjectPath("example-site"))
	data, _ := os.ReadFile(projFile)
	newer := []byte(strings.Replace(string(data), "schema: 1", "schema: 7", 1))
	if err := os.WriteFile(projFile, newer, 0o640); err != nil {
		t.Fatal(err)
	}
	before := readAll(t, dir)
	_, _, err := NewStore(dir).Load()
	var se *SchemaError
	if !errors.As(err, &se) || se.Got != 7 || se.Supported != CurrentSchema {
		t.Fatalf("want SchemaError for version 7, got %v", err)
	}
	if !reflect.DeepEqual(before, readAll(t, dir)) {
		t.Fatal("a too new file was touched")
	}
}

func TestStoreLoadWithoutSettingsFile(t *testing.T) {
	dir := t.TempDir()
	st := NewStore(dir)
	m, revs, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if m.Settings.Schema != CurrentSchema || len(m.Projects) != 0 || len(m.Templates) != 0 {
		t.Fatalf("model = %+v", m)
	}
	if _, ok := revs[SettingsPath()]; ok {
		t.Fatalf("a missing file has a revision: %v", revs)
	}
	rev, err := st.SaveSettings(m.Settings, revs[SettingsPath()])
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, SettingsFile))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %v", info.Mode().Perm())
	}
	_, revs, err = st.Load()
	if err != nil || revs[SettingsPath()] != rev {
		t.Fatalf("second load: revs=%v rev=%q err=%v", revs, rev, err)
	}
}

func TestReadSettings(t *testing.T) {
	dir := t.TempDir()
	got, err := ReadSettings(dir)
	if err != nil || got.Schema != CurrentSchema {
		t.Fatalf("missing file: %+v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte("schema: 1\npoll_interval: 10m\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if got, err = ReadSettings(dir); err != nil || got.EffectivePollInterval().Minutes() != 10 {
		t.Fatalf("valid file: %+v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, SettingsFile), []byte("schema: [\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err = ReadSettings(dir); err == nil {
		t.Fatal("an invalid file gave no error")
	}
}
