//go:build e2efakes

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"github.com/castrowithcee/qatlas-kit/selfupdate"
)

// TestReleasesFakeSatisfiesTheKitClient installs a release served by the fake through the kit's updater,
// which accepts assets only under <API>/releases/assets/<digits>.
func TestReleasesFakeSatisfiesTheKitClient(t *testing.T) {
	root := t.TempDir()
	old := releaseRoot
	releaseRoot = root
	t.Cleanup(func() { releaseRoot = old })

	signer := releasetest.NewSigner(t)
	for _, tag := range []string{"v0.7.0", "v0.8.0"} {
		name := "qsysmin_" + tag + "_linux_amd64.tar.gz"
		archive := releasetest.Archive(t, name,
			releasetest.File{Name: "./bin/qsysmin", Body: []byte("program " + tag)},
			releasetest.File{Name: "./lib/systemd/system/qsysmin.service", Body: []byte("unit " + tag)})
		assets := releasetest.Release(t, signer, "qsysmin-release", map[string][]byte{name: archive})
		if err := os.MkdirAll(filepath.Join(root, tag), 0o755); err != nil {
			t.Fatal(err)
		}
		for file, body := range assets {
			if err := os.WriteFile(filepath.Join(root, tag, file), body, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	server := httptest.NewServer(http.HandlerFunc(serveReleases))
	t.Cleanup(server.Close)

	dir := t.TempDir()
	program, unit := filepath.Join(dir, "qsysmin"), filepath.Join(dir, "qsysmin.service")
	if err := os.WriteFile(program, []byte("program v0.7.0"), 0o755); err != nil {
		t.Fatal(err)
	}
	client := &selfupdate.Client{
		RepositoryAPI:  server.URL + "/repos/" + qsysminRepo,
		CurrentVersion: "v0.7.0",
		MinimumVersion: "v0.7.0",
		ArchiveName:    func(v string) string { return "qsysmin_" + v + "_linux_amd64.tar.gz" },
		UserAgent:      "qsysmin-e2e-test",
		Namespace:      "qsysmin-release",
		TrustedKeys:    []ssh.PublicKey{signer.PublicKey()},
		ProgramName:    "qsysmin",
		Companions:     []selfupdate.Companion{{Entry: "lib/systemd/system/qsysmin.service", Path: unit}},
		Executable:     program,
	}
	ctx := context.Background()
	check, err := client.Check(ctx)
	if err != nil || !check.UpdateAvailable || check.Latest != "v0.8.0" {
		t.Fatalf("Check = %+v, %v", check, err)
	}
	target, err := client.Target(ctx, "v0.8.0")
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := client.Prepare(ctx, target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.InstallPrepared(ctx, prepared); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(program); string(got) != "program v0.8.0" {
		t.Errorf("program = %q", got)
	}
	if got, _ := os.ReadFile(unit); string(got) != "unit v0.8.0" {
		t.Errorf("unit = %q", got)
	}
}
