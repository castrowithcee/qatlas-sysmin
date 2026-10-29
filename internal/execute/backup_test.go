package execute

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/castrowithcee/qatlas-sysmin/internal/config"
)

var testNow = time.Date(2026, 10, 29, 10, 15, 0, 0, time.UTC)

func backupExec(t *testing.T, r Runner) (*Executor, string) {
	root := t.TempDir()
	return &Executor{Runner: r, BackupRoot: root, Now: func() time.Time { return testNow }}, root
}

func backupStep(dir string, b config.BackupParams) config.ResolvedStep {
	return config.ResolvedStep{Type: config.StepBackup, Timeout: time.Minute, Dir: dir, Backup: &b}
}

func mkdirs(t *testing.T, base string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.MkdirAll(filepath.Join(base, n), 0o700); err != nil {
			t.Fatal(err)
		}
	}
}

func listNames(t *testing.T, dir string) []string {
	t.Helper()
	es, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range es {
		out = append(out, e.Name())
	}
	return out
}

func TestBackupCommandGetsDirectoryAndRetains(t *testing.T) {
	ex, root := backupExec(t, &ExecRunner{})
	proj := filepath.Join(root, "shop")
	other := filepath.Join(root, "other")
	mkdirs(t, proj, "20260101T000000Z", "20260102T000000Z", "20260103T000000Z", "20260104T000000Z", "manual-copy", "20269999T000000Z")
	mkdirs(t, other, "20260101T000000Z", "20260102T000000Z", "20260103T000000Z", "20260104T000000Z")
	foreign := t.TempDir()
	if err := os.Symlink(foreign, filepath.Join(proj, "20260100T000000Z")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(proj, "20260101T000001Z"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	workdir := t.TempDir()
	st := backupStep(workdir, config.BackupParams{Command: `echo data > "$SYSMIN_BACKUP_DIR/dump.txt"`, Keep: 2})
	rep, err := ex.Execute(context.Background(), config.Plan{Project: "shop", Steps: []config.ResolvedStep{st}})
	if err != nil {
		t.Fatal(err)
	}
	dir := rep.Steps[0].BackupDir
	if dir != filepath.Join(proj, "20261029T101500Z") {
		t.Fatalf("backup dir %q", dir)
	}
	if b, err := os.ReadFile(filepath.Join(dir, "dump.txt")); err != nil || string(b) != "data\n" {
		t.Errorf("dump: %q %v", b, err)
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("mode %v", fi.Mode())
	}
	want := []string{"20260100T000000Z", "20260101T000001Z", "20260104T000000Z", "20261029T101500Z", "20269999T000000Z", "manual-copy"}
	// 20269999T000000Z is no valid time and stays; of the valid ones only the newest two remain.
	if got := listNames(t, proj); !reflect.DeepEqual(got, want) {
		t.Errorf("project dir: %v", got)
	}
	if got := listNames(t, other); len(got) != 4 {
		t.Errorf("other project changed: %v", got)
	}
	if got := listNames(t, foreign); len(got) != 0 {
		t.Errorf("symlink target changed: %v", got)
	}
}

func TestBackupRetentionKeepsNewest(t *testing.T) {
	ex, root := backupExec(t, &fakeRunner{})
	proj := filepath.Join(root, "shop")
	mkdirs(t, proj, "20260101T000000Z", "20260102T000000Z", "20260103T000000Z", "20260104T000000Z")
	st := backupStep(t.TempDir(), config.BackupParams{Command: "true", Keep: 3})
	if _, err := ex.Execute(context.Background(), config.Plan{Project: "shop", Steps: []config.ResolvedStep{st}}); err != nil {
		t.Fatal(err)
	}
	want := []string{"20260103T000000Z", "20260104T000000Z", "20261029T101500Z"}
	if got := listNames(t, proj); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v", got)
	}
}

func TestBackupNameCollisionMovesOn(t *testing.T) {
	ex, root := backupExec(t, &fakeRunner{})
	mkdirs(t, filepath.Join(root, "shop"), "20261029T101500Z")
	st := backupStep(t.TempDir(), config.BackupParams{Command: "true", Keep: 5})
	rep, err := ex.Execute(context.Background(), config.Plan{Project: "shop", Steps: []config.ResolvedStep{st}})
	if err != nil || filepath.Base(rep.Steps[0].BackupDir) != "20261029T101501Z" {
		t.Errorf("%v %+v", err, rep.Steps[0])
	}
}

func TestFailedBackupRemovesDirAndSkipsRetention(t *testing.T) {
	ex, root := backupExec(t, &ExecRunner{})
	proj := filepath.Join(root, "shop")
	mkdirs(t, proj, "20260101T000000Z", "20260102T000000Z")
	st := backupStep(t.TempDir(), config.BackupParams{Command: `echo part > "$SYSMIN_BACKUP_DIR/x"; exit 4`, Keep: 1})
	rep, err := ex.Execute(context.Background(), config.Plan{Project: "shop", Steps: []config.ResolvedStep{st}})
	if err == nil || rep.Steps[0].Status != StatusFailed {
		t.Fatalf("err=%v", err)
	}
	if got := listNames(t, proj); !reflect.DeepEqual(got, []string{"20260101T000000Z", "20260102T000000Z"}) {
		t.Errorf("got %v", got)
	}
}

func TestBackupVolumeArgv(t *testing.T) {
	fr := &fakeRunner{}
	ex, root := backupExec(t, fr)
	st := backupStep("", config.BackupParams{Volume: "notes-data", Keep: 2})
	rep, err := ex.Execute(context.Background(), config.Plan{Project: "notes", Steps: []config.ResolvedStep{st}})
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "notes", "20261029T101500Z")
	want := []string{"run", "--rm", "--network", "none", "-v", "notes-data:/source:ro", "-v", dir + ":/backup",
		DefaultBackupImage, "tar", "-C", "/source", "-czf", "/backup/notes-data.tar.gz", "."}
	if len(fr.calls) != 1 || fr.calls[0].Name != "docker" || !reflect.DeepEqual(fr.calls[0].Args, want) {
		t.Errorf("calls %+v", fr.calls)
	}
	if !strings.Contains(DefaultBackupImage, "@sha256:") || rep.Steps[0].BackupDir != dir {
		t.Errorf("%+v", rep.Steps[0])
	}
}

func TestBackupVolumeRejectsBadInput(t *testing.T) {
	for _, vol := range []string{"-x", "a:b", "/etc", "a,b"} {
		fr := &fakeRunner{}
		ex, root := backupExec(t, fr)
		st := backupStep("", config.BackupParams{Volume: vol, Keep: 1})
		if _, err := ex.Execute(context.Background(), config.Plan{Project: "p", Steps: []config.ResolvedStep{st}}); err == nil || len(fr.calls) != 0 {
			t.Errorf("%q: err=%v calls=%d", vol, err, len(fr.calls))
		}
		if got := listNames(t, filepath.Join(root, "p")); len(got) != 0 {
			t.Errorf("%q left %v", vol, got)
		}
	}
	ex, _ := backupExec(t, &fakeRunner{})
	ex.BackupImage = "busybox:latest"
	st := backupStep("", config.BackupParams{Volume: "v", Keep: 1})
	if _, err := ex.Execute(context.Background(), config.Plan{Project: "p", Steps: []config.ResolvedStep{st}}); err == nil {
		t.Error("unpinned image accepted")
	}
}

func TestBackupRejectsBadProject(t *testing.T) {
	ex, root := backupExec(t, &fakeRunner{})
	st := backupStep(t.TempDir(), config.BackupParams{Command: "true", Keep: 1})
	if _, err := ex.Execute(context.Background(), config.Plan{Project: "../x", Steps: []config.ResolvedStep{st}}); err == nil {
		t.Error("accepted")
	}
	if got := listNames(t, root); len(got) != 0 {
		t.Errorf("created %v", got)
	}
}

func TestBackupDryRunCreatesNothing(t *testing.T) {
	root := t.TempDir()
	ex := &Executor{BackupRoot: filepath.Join(root, "b")}
	plan := config.Plan{Project: "notes", Steps: []config.ResolvedStep{backupStep("", config.BackupParams{Volume: "v1", Keep: 4})}}
	var out strings.Builder
	if err := ex.WriteDryRun(&out, plan); err != nil {
		t.Fatal(err)
	}
	for _, w := range []string{"docker run --rm --network none -v v1:/source:ro", filepath.Join(root, "b", "notes", "<timestamp>"), "keep last 4"} {
		if !strings.Contains(out.String(), w) {
			t.Errorf("lacks %q:\n%s", w, out.String())
		}
	}
	if got := listNames(t, root); len(got) != 0 {
		t.Errorf("created %v", got)
	}
}

func TestRestore(t *testing.T) {
	fr := &fakeRunner{}
	ex, root := backupExec(t, fr)
	proj := filepath.Join(root, "shop")
	good := filepath.Join(proj, "20260101T000000Z")
	mkdirs(t, proj, "20260101T000000Z")
	foreign := t.TempDir()
	link := filepath.Join(proj, "20260102T000000Z")
	if err := os.Symlink(foreign, link); err != nil {
		t.Fatal(err)
	}
	mkdirs(t, root, "other/20260101T000000Z")
	workdir := t.TempDir()
	st := backupStep(workdir, config.BackupParams{Command: "true", Keep: 1, RestoreCommand: "restore-it"})

	res, err := ex.Restore(context.Background(), "shop", st, good)
	if err != nil || res.Status != StatusOK || res.BackupDir != good {
		t.Fatalf("%v %+v", err, res)
	}
	c := fr.calls[0]
	if c.Name != shell || !reflect.DeepEqual(c.Args, []string{"-c", "restore-it"}) || c.Dir != workdir || !reflect.DeepEqual(c.Env, []string{"SYSMIN_BACKUP_DIR=" + good}) {
		t.Errorf("call %+v", c)
	}

	noRestore := backupStep(workdir, config.BackupParams{Command: "true", Keep: 1})
	for name, tc := range map[string]struct {
		st  config.ResolvedStep
		dir string
	}{
		"other project": {st, filepath.Join(root, "other", "20260101T000000Z")},
		"dotdot":        {st, proj + "/../shop/20260101T000000Z"},
		"symlink":       {st, link},
		"missing":       {st, filepath.Join(proj, "20260103T000000Z")},
		"bad name":      {st, filepath.Join(proj, "latest")},
		"project dir":   {st, proj},
		"relative":      {st, "20260101T000000Z"},
		"no restore":    {noRestore, good},
	} {
		n := len(fr.calls)
		if _, err := ex.Restore(context.Background(), "shop", tc.st, tc.dir); err == nil || len(fr.calls) != n {
			t.Errorf("%s: err=%v", name, err)
		}
	}
	if _, err := ex.Restore(context.Background(), "../shop", st, good); err == nil {
		t.Error("bad project accepted")
	}
}
