package adopt

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func skipRoot(t *testing.T) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root passes any directory and reads any file")
	}
}

func chmodUntil(t *testing.T, path string, mode, restore os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, restore) })
}

func TestCheckDir(t *testing.T) {
	skipRoot(t)
	root := t.TempDir()
	closed := filepath.Join(root, "closed")
	inner := filepath.Join(closed, "inner")
	if err := os.MkdirAll(inner, 0o700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckDir(inner); err != nil {
		t.Fatalf("open directory: %v", err)
	}
	chmodUntil(t, closed, 0o600, 0o700)

	var ae *AccessError
	for _, d := range []string{inner, filepath.Join(inner, "missing"), closed} {
		if err := CheckDir(d); !errors.As(err, &ae) || ae.Path != closed {
			t.Errorf("CheckDir(%q) = %v, want access error for %q", d, err, closed)
		}
	}
	for _, d := range []string{filepath.Join(root, "missing"), file, filepath.Join(file, "x")} {
		if err := CheckDir(d); !errors.Is(err, ErrNotDirectory) {
			t.Errorf("CheckDir(%q) = %v, want not a directory", d, err)
		}
	}
	// A plain file without the execute bit is not reported as a closed directory.
	if err := CheckDir(file); errors.As(err, &ae) {
		t.Errorf("file reported as closed directory: %v", err)
	}
	if err := CheckDir("/"); err != nil {
		t.Errorf("root: %v", err)
	}
}

// missingAccess returns the one blocking access problem of p and fails when there is not exactly one.
func missingAccess(t *testing.T, p Proposal) Problem {
	t.Helper()
	var found []Problem
	for _, pr := range p.Problems {
		if pr.Code == CodeMissingAccess {
			found = append(found, pr)
		}
	}
	if len(found) != 1 {
		t.Fatalf("want one %s problem, got %+v", CodeMissingAccess, p.Problems)
	}
	return found[0]
}

func TestInspectClosedDirectoryIsAMissingAccessProblem(t *testing.T) {
	skipRoot(t)
	f, dir := stack(t, `{"services":{}}`)
	chmodUntil(t, dir, 0o600, 0o700)
	p := inspect(t, f, dir)
	pr := missingAccess(t, p)
	if !slices.Equal(pr.Access, []AccessNeed{{Path: dir, Right: RightPass}}) || !strings.Contains(pr.Message, "below "+dir) {
		t.Errorf("problem = %+v", pr)
	}
	for _, c := range f.calls {
		if c.Args[0] == "compose" {
			t.Errorf("compose ran: %v", c.Args)
		}
	}
	// Without containers the closed directory is still the finding, since the stack cannot be told apart.
	f.ps = ""
	if pr := missingAccess(t, inspect(t, f, dir)); len(pr.Access) != 1 {
		t.Errorf("problem = %+v", pr)
	}
}

func TestAllMissingAccessAtOnce(t *testing.T) {
	skipRoot(t)
	root := t.TempDir()
	dir := filepath.Join(root, "stack")
	closed := filepath.Join(root, "closed")
	other := filepath.Join(closed, "inner", "app.env")
	for _, d := range []string{dir, filepath.Dir(other)} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	compose := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(compose, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chmodUntil(t, compose, 0o000, 0o600)
	dotEnv := writeEnv(t, dir, "DEMO_SECRET=not-a-real-secret\n")
	chmodUntil(t, dotEnv, 0o000, 0o600)
	prodEnv := filepath.Join(dir, "prod.env")
	if err := os.WriteFile(prodEnv, []byte("DEMO_SECRET=not-a-real-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chmodUntil(t, prodEnv, 0o444, 0o600)
	chmodUntil(t, closed, 0o600, 0o700)

	l := labels("demo-stack", compose, other+","+prodEnv)
	f := &fakeDocker{ps: idA + "\n", inspect: l + "\n", compose: `{"services":{}}`}
	p := inspect(t, f, dir)
	pr := missingAccess(t, p)
	want := []AccessNeed{
		{compose, RightRead}, {closed, RightPass}, {dotEnv, RightRead}, {prodEnv, RightWrite},
	}
	if !slices.Equal(pr.Access, want) {
		t.Fatalf("access = %+v, want %+v", pr.Access, want)
	}
	for _, w := range []string{"pass " + closed, "read " + compose, "read " + dotEnv, "write " + prodEnv, "below " + closed} {
		if !strings.Contains(pr.Message, w) {
			t.Errorf("message %q lacks %q", pr.Message, w)
		}
	}
	u := serviceUser()
	for _, w := range []string{"Give " + u, "setfacl -m u:" + u + ":x " + closed, ":r " + compose + " " + dotEnv, ":rw " + prodEnv} {
		if !strings.Contains(pr.Hint, w) {
			t.Errorf("hint %q lacks %q", pr.Hint, w)
		}
	}
	for _, c := range f.calls {
		if c.Args[0] == "compose" {
			t.Errorf("compose ran: %v", c.Args)
		}
	}
	if b, _ := json.Marshal(p); strings.Contains(string(b), "not-a-real-secret") {
		t.Errorf("secret in %s", b)
	}
}

func TestUnreadableGitDirectoryIsAccessProblem(t *testing.T) {
	skipRoot(t)
	f, dir := gitStack(t, "v1.2.0")
	git := filepath.Join(dir, ".git")
	chmodUntil(t, git, 0o000, 0o700)
	p := inspect(t, f, dir)
	pr := missingAccess(t, p)
	if !slices.Equal(pr.Access, []AccessNeed{{git, RightRead}}) || !strings.Contains(pr.Hint, ":rx "+git) {
		t.Fatalf("problem = %+v", pr)
	}
	if got := codes(p); !slices.Equal(got, []string{CodeMissingAccess}) {
		t.Errorf("misleading git findings: %v", got)
	}
}

func TestGitWorkingCopyWriteAccess(t *testing.T) {
	skipRoot(t)
	f, dir := gitStack(t, "v1.2.0")
	chmodUntil(t, dir, 0o500, 0o700)
	pr := missingAccess(t, inspect(t, f, dir))
	if !slices.Equal(pr.Access, []AccessNeed{{dir, RightWrite}}) || !strings.Contains(pr.Hint, ":rwx "+dir) {
		t.Fatalf("problem = %+v", pr)
	}
}

func TestHintsNameTheServiceUser(t *testing.T) {
	skipRoot(t)
	f, dir := stack(t, `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`)
	env := writeEnv(t, dir, "APP_VERSION=1.4.2\n")
	chmodUntil(t, env, 0o444, 0o600)
	pr := missingAccess(t, inspect(t, f, dir))
	if !strings.Contains(pr.Hint, serviceUser()) || !strings.HasSuffix(pr.Hint, ":rw "+env) ||
		strings.Contains(pr.Hint, "the account") || !slices.Equal(pr.Access, []AccessNeed{{env, RightWrite}}) {
		t.Errorf("problem = %+v", pr)
	}
}

func TestHintQuotesPaths(t *testing.T) {
	in := &inspector{}
	in.need("/srv/demo stack", RightPass, true)
	in.accessProblem()
	if h := in.prop.Problems[0].Hint; !strings.HasSuffix(h, " '/srv/demo stack'") {
		t.Errorf("hint = %q", h)
	}
}

func TestWriteCheckRunsWhenComposeFails(t *testing.T) {
	skipRoot(t)
	f, dir := stack(t, "")
	env := writeEnv(t, dir, "APP_VERSION=1.4.2\n")
	chmodUntil(t, env, 0o444, 0o600)
	p := inspect(t, f, dir)
	pr := missingAccess(t, p)
	if !slices.Equal(pr.Access, []AccessNeed{{env, RightWrite}}) {
		t.Errorf("problem = %+v", pr)
	}
	if !slices.Contains(codes(p), CodeComposeConfig) {
		t.Errorf("codes = %v", codes(p))
	}
}
