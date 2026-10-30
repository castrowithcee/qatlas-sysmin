package adopt

import (
	"context"
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

func TestInspectClosedDirectoryIsAnAccessError(t *testing.T) {
	skipRoot(t)
	f, dir := stack(t, `{"services":{}}`)
	chmodUntil(t, dir, 0o600, 0o700)
	_, err := Inspect(context.Background(), Options{Runner: f}, dir, "")
	var ae *AccessError
	if !errors.As(err, &ae) || ae.Path != dir {
		t.Fatalf("err = %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("commands ran: %v", f.calls)
	}
	_, err = Inspect(context.Background(), Options{Runner: f}, filepath.Join(dir, "missing"), "")
	if !errors.Is(err, ErrNotDirectory) && !errors.As(err, &ae) {
		t.Errorf("err = %v", err)
	}
}

func TestUnreadableComposeFile(t *testing.T) {
	skipRoot(t)
	f, dir := stack(t, "")
	file := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(file, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chmodUntil(t, file, 0o000, 0o600)
	p := inspect(t, f, dir)
	if !slices.Equal(codes(p), []string{CodeNotReadable}) {
		t.Fatalf("problems = %+v", p.Problems)
	}
	pr := p.Problems[0]
	if !strings.Contains(pr.Message, file) || !strings.Contains(pr.Hint, "setfacl -m u:") || !strings.HasSuffix(pr.Hint, ":r "+file) {
		t.Errorf("problem = %+v", pr)
	}
	for _, c := range f.calls {
		if c.Args[0] == "compose" {
			t.Errorf("compose ran with an unreadable file: %v", c.Args)
		}
	}
}

func TestUnreadableEnvFileIsNamed(t *testing.T) {
	skipRoot(t)
	f, dir := stack(t, `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`)
	env := writeEnv(t, dir, "APP_VERSION=1.4.2\n")
	chmodUntil(t, env, 0o000, 0o600)
	p := inspect(t, f, dir)
	var found *Problem
	for i := range p.Problems {
		if p.Problems[i].Code == CodeEnvFileUnreadable {
			found = &p.Problems[i]
		}
	}
	if found == nil || !strings.Contains(found.Message, env) || !strings.HasSuffix(found.Hint, ":r "+env) {
		t.Fatalf("problems = %+v", p.Problems)
	}
}

func TestUnreadableGitDirectoryIsAProblem(t *testing.T) {
	skipRoot(t)
	f, dir := gitStack(t, "v1.2.0")
	git := filepath.Join(dir, ".git")
	chmodUntil(t, git, 0o000, 0o700)
	p := inspect(t, f, dir)
	var found *Problem
	for i := range p.Problems {
		if p.Problems[i].Code == CodeNotReadable {
			found = &p.Problems[i]
		}
	}
	if found == nil || !strings.Contains(found.Message, git) || !strings.Contains(found.Hint, "setfacl") {
		t.Fatalf("problems = %+v", p.Problems)
	}
}

func TestHintsNameTheServiceUser(t *testing.T) {
	skipRoot(t)
	f, dir := stack(t, `{"services":{"app":{"image":"example.invalid/shop/app:${APP_VERSION}"}}}`)
	env := writeEnv(t, dir, "APP_VERSION=1.4.2\n")
	chmodUntil(t, env, 0o444, 0o600)
	p := inspect(t, f, dir)
	if !slices.Equal(codes(p), []string{CodeNotWritable}) || !strings.Contains(p.Problems[0].Hint, serviceUser()) ||
		strings.Contains(p.Problems[0].Hint, "the account") {
		t.Errorf("problems = %+v", p.Problems)
	}
	if h := grantHint("/srv/demo stack", "x"); !strings.HasSuffix(h, " '/srv/demo stack'") {
		t.Errorf("hint = %q", h)
	}
}
