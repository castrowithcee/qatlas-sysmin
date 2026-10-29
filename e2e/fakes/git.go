//go:build e2efakes

package main

import (
	"net/http/cgi"
	"os"
	"os/exec"
	"path/filepath"
)

const gitRoot = "/srv/git"

// runGit serves the bare repositories under /srv/git over smart HTTP behind basic auth and seeds a small
// repository named stacks.git on first start.
func runGit() error {
	if err := seedGit(); err != nil {
		return err
	}
	backend := &cgi.Handler{
		Path: "/usr/lib/git-core/git-http-backend",
		Env:  []string{"GIT_PROJECT_ROOT=" + gitRoot, "GIT_HTTP_EXPORT_ALL=1"},
	}
	return serveTLS(":443", basicAuth(os.Getenv("GIT_USER"), os.Getenv("GIT_PASSWORD"), backend))
}

func seedGit() error {
	bare := filepath.Join(gitRoot, "stacks.git")
	if _, err := os.Stat(bare); err == nil {
		return nil
	}
	work, err := os.MkdirTemp("", "seed")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)
	files := map[string]string{
		"README.md":        "# Example stacks\n\nInvented content for end-to-end checks.\n",
		"web/compose.yaml": "services:\n  web:\n    image: registry.sysmine2e.test/demo/web:1.0.0\n    restart: unless-stopped\n",
	}
	for name, content := range files {
		path := filepath.Join(work, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	steps := [][]string{
		{"init", "--bare", "--initial-branch=main", bare},
		{"-C", bare, "config", "http.receivepack", "true"},
		{"-C", work, "init", "--initial-branch=main"},
		{"-C", work, "add", "."},
		{"-C", work, "-c", "user.name=E2E Seed", "-c", "user.email=seed" + "@" + "sysmine2e.test", "commit", "-m", "Initial stacks"},
		{"-C", work, "push", bare, "main"},
	}
	for _, args := range steps {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			return &execError{args: args, out: string(out), err: err}
		}
	}
	return nil
}

type execError struct {
	args []string
	out  string
	err  error
}

func (e *execError) Error() string { return "git " + e.args[0] + ": " + e.err.Error() + ": " + e.out }
