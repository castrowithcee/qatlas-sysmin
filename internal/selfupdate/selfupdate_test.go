package selfupdate

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/release/releasetest"
	"github.com/castrowithcee/qatlas-sysmin/internal/selfupdate/selfupdatetest"
)

const oldProgram = "old program"

func installed(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sysmin")
	if err := os.WriteFile(path, []byte(oldProgram), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

const oldUnit = "[Unit]\nDescription=old\n"

func clientFor(s *selfupdatetest.Server, version, target string) *Client {
	unit := filepath.Join(filepath.Dir(target), "units", "sysmin.service")
	if _, err := os.Stat(unit); err != nil && target != "" {
		_ = os.MkdirAll(filepath.Dir(unit), 0o755)
		_ = os.WriteFile(unit, []byte(oldUnit), 0o600)
	}
	return &Client{BaseURL: s.URL, Version: version, GOARCH: "amd64", Target: target, UnitTarget: unit,
		SigningKeys: []ssh.PublicKey{s.Signer.PublicKey()}}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func assertOnlyTarget(t *testing.T, target string) {
	t.Helper()
	entries, err := os.ReadDir(filepath.Dir(target))
	if err != nil || len(entries) != 2 { // the program and the units directory
		t.Errorf("target directory = %v (%v), want only the program", entries, err)
	}
	units, err := os.ReadDir(filepath.Join(filepath.Dir(target), "units"))
	if err != nil || len(units) != 1 || units[0].Name() != "sysmin.service" {
		t.Errorf("unit directory = %v (%v), want only the unit", units, err)
	}
}

func TestInstallReplacesTarget(t *testing.T) {
	s := selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new program"})
	target := installed(t)
	c := clientFor(s, "v1.0.0", target)
	tg, err := c.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Install(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Updated || res.Previous != "v1.0.0" || res.Version != "v1.1.0" {
		t.Errorf("result = %+v", res)
	}
	if got := readFile(t, target); got != "new program" {
		t.Errorf("target = %q", got)
	}
	if info, _ := os.Stat(target); info.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v, want 0755", info.Mode().Perm())
	}
	assertOnlyTarget(t, target)
}

func TestInstallRejectsBadReleasesWithoutChange(t *testing.T) {
	foreign := releasetest.NewSigner(t)
	tests := []struct {
		name string
		rel  selfupdatetest.Release
		want string
	}{
		{"tampered checksum", selfupdatetest.Release{Tag: "v1.1.0", Program: "x", BadChecksum: true}, "checksum mismatch"},
		{"foreign namespace", selfupdatetest.Release{Tag: "v1.1.0", Program: "x", Namespace: "qatlas-release"}, "namespace"},
		{"foreign key", selfupdatetest.Release{Tag: "v1.1.0", Program: "x", Signer: foreign}, "not trusted"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := selfupdatetest.New(t, "amd64", tt.rel)
			target := installed(t)
			c := clientFor(s, "v1.0.0", target)
			tg, err := c.Resolve(context.Background(), "v1.1.0")
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.Install(context.Background(), tg)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("err = %v, want %q", err, tt.want)
			}
			if got := readFile(t, target); got != oldProgram {
				t.Errorf("target changed to %q", got)
			}
			if got := readFile(t, unitOf(c)); got != oldUnit {
				t.Errorf("unit changed to %q", got)
			}
			assertOnlyTarget(t, target)
		})
	}
}

func TestProductionKeysRejectTestSignature(t *testing.T) {
	s := selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "x"})
	target := installed(t)
	c := clientFor(s, "v1.0.0", target)
	c.SigningKeys = nil // production path: only release.TrustedKeys()
	tg, err := c.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Install(context.Background(), tg); err == nil || !strings.Contains(err.Error(), "not trusted") {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, target) != oldProgram {
		t.Error("target changed")
	}
}

func TestResolveRefusesUnsignedAndUnknown(t *testing.T) {
	s := selfupdatetest.New(t, "amd64",
		selfupdatetest.Release{Tag: "v1.0.0", Program: "x"},
		selfupdatetest.Release{Tag: "v1.1.0", Program: "x", NoSignature: true},
		selfupdatetest.Release{Tag: "v0.0.1", Program: "x"},
	)
	c := clientFor(s, "v1.0.0", installed(t))
	for _, tag := range []string{"v1.1.0", "v9.9.9", "v0.0.1", "1.0.0"} {
		var te *TargetError
		if _, err := c.Resolve(context.Background(), tag); !errors.As(err, &te) {
			t.Errorf("Resolve(%q) err = %v, want TargetError", tag, err)
		}
	}
	tg, err := c.Resolve(context.Background(), "v1.0.0")
	if err != nil || !tg.Current {
		t.Fatalf("Resolve current = %+v, %v", tg, err)
	}
	target := installed(t)
	c.Target = target
	res, err := c.Install(context.Background(), tg)
	if err != nil || res.Updated || readFile(t, target) != oldProgram {
		t.Errorf("installing the running version = %+v, %v", res, err)
	}
}

func TestResolveDevBuildAndOlder(t *testing.T) {
	s := selfupdatetest.New(t, "amd64",
		selfupdatetest.Release{Tag: "v1.0.0", Program: "x"},
		selfupdatetest.Release{Tag: "v1.1.0", Program: "x"})
	if _, err := clientFor(s, "dev", installed(t)).Resolve(context.Background(), ""); !errors.Is(err, ErrDevelopmentBuild) {
		t.Errorf("dev err = %v", err)
	}
	tg, err := clientFor(s, "v1.1.0", installed(t)).Resolve(context.Background(), "v1.0.0")
	if err != nil || !tg.Older {
		t.Errorf("older = %+v, %v", tg, err)
	}
}

func TestListShowsSignedReleasesWithPrereleases(t *testing.T) {
	s := selfupdatetest.New(t, "amd64",
		selfupdatetest.Release{Tag: "v1.0.0", Program: "x"},
		selfupdatetest.Release{Tag: "v1.1.0", Program: "x"},
		selfupdatetest.Release{Tag: "v1.2.0", Program: "x", NoSignature: true},
		selfupdatetest.Release{Tag: "v1.3.0", Program: "x", Draft: true},
		selfupdatetest.Release{Tag: "v1.4.0-rc.1", Program: "x", Prerelease: true},
		selfupdatetest.Release{Tag: "v0.0.5", Program: "x"},
	)
	got, err := clientFor(s, "v1.0.0", installed(t)).List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var tags []string
	for _, e := range got {
		tags = append(tags, e.Version)
	}
	if want := "v1.4.0-rc.1 v1.1.0 v1.0.0"; strings.Join(tags, " ") != want {
		t.Errorf("tags = %v, want %s", tags, want)
	}
	if !got[0].Prerelease || !got[2].Current || got[1].Older {
		t.Errorf("entries = %+v", got)
	}
}

func TestInstallRefusesSymlinkAndMissingTargetBeforeDownload(t *testing.T) {
	s := selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "x"})
	c := clientFor(s, "v1.0.0", "")
	tg, err := c.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	_ = os.WriteFile(real, []byte(oldProgram), 0o755)
	link := filepath.Join(dir, "link")
	_ = os.Symlink(real, link)
	c.Target = link
	var ue *UnsupportedInstallationError
	if _, err := c.Install(context.Background(), tg); !errors.As(err, &ue) {
		t.Errorf("symlink err = %v", err)
	}
	c.Target = filepath.Join(dir, "missing")
	if _, err := c.Install(context.Background(), tg); err == nil {
		t.Error("missing target accepted")
	}
}

func TestUnsupportedArchitecture(t *testing.T) {
	s := selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "x"})
	c := clientFor(s, "v1.0.0", installed(t))
	tg, _ := c.Resolve(context.Background(), "")
	c.GOARCH = "riscv64"
	if _, err := c.Install(context.Background(), tg); err == nil {
		t.Error("unsupported architecture accepted")
	}
}

func TestValidateAssetURL(t *testing.T) {
	prod := &Client{}
	for url, ok := range map[string]bool{
		"https://api.github.com/repos/x/releases/assets/1": true,
		"https://objects.githubusercontent.com/a":          true,
		"http://api.github.com/a":                          false,
		"https://evil.example/a":                           false,
		"https://github.com.evil.example/a":                false,
		"/relative":                                        false,
	} {
		if got := prod.validateAssetURL(url) == nil; got != ok {
			t.Errorf("production %q = %v, want %v", url, got, ok)
		}
	}
	custom := &Client{BaseURL: "http://127.0.0.1:9000/"}
	if custom.validateAssetURL("http://127.0.0.1:9000/a") != nil {
		t.Error("same origin rejected")
	}
	for _, u := range []string{"http://127.0.0.1:9001/a", "https://127.0.0.1:9000/a", "https://api.github.com/a"} {
		if custom.validateAssetURL(u) == nil {
			t.Errorf("custom base accepted %q", u)
		}
	}
	if (&Client{BaseURL: "ftp://host"}).validateAssetURL("ftp://host/a") == nil {
		t.Error("non-http base accepted")
	}
}

func TestNoAuthorizationHeaderEvenWithTokenInEnvironment(t *testing.T) {
	t.Setenv("GH_TOKEN", "example-token")
	t.Setenv("GITHUB_TOKEN", "example-token")
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Authorization")
		_, _ = w.Write([]byte("[]"))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, Version: "v1.0.0"}
	if _, err := c.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if seen != "" {
		t.Errorf("Authorization header = %q, want none", seen)
	}
}

func TestSemverOrder(t *testing.T) {
	order := []string{"v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-rc.1", "v1.0.0", "v1.0.1", "v1.10.0"}
	for i := 0; i+1 < len(order); i++ {
		a, err1 := parseSemver(order[i])
		b, err2 := parseSemver(order[i+1])
		if err1 != nil || err2 != nil || a.compare(b) >= 0 || b.compare(a) <= 0 {
			t.Errorf("%s < %s failed (%v %v)", order[i], order[i+1], err1, err2)
		}
	}
	for _, bad := range []string{"1.0.0", "v1.0", "v01.0.0", "v1.0.0-"} {
		if _, err := parseSemver(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func unitOf(c *Client) string { return c.UnitTarget }

func TestInstallReplacesDifferingUnitBeforeProgram(t *testing.T) {
	s := selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new program", Unit: "[Unit]\nDescription=new\n"})
	target := installed(t)
	c := clientFor(s, "v1.0.0", target)
	tg, err := c.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Install(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	if !res.UnitUpdated || !res.Updated {
		t.Errorf("result = %+v", res)
	}
	if got := readFile(t, unitOf(c)); got != "[Unit]\nDescription=new\n" {
		t.Errorf("unit = %q", got)
	}
	if info, _ := os.Stat(unitOf(c)); info.Mode().Perm() != 0o644 {
		t.Errorf("unit mode = %v, want 0644", info.Mode().Perm())
	}
	assertOnlyTarget(t, target)
}

func TestInstallLeavesIdenticalUnitUntouched(t *testing.T) {
	s := selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new program", Unit: oldUnit})
	target := installed(t)
	c := clientFor(s, "v1.0.0", target)
	before, err := os.Stat(unitOf(c))
	if err != nil {
		t.Fatal(err)
	}
	past := before.ModTime().Add(-time.Hour)
	if err := os.Chtimes(unitOf(c), past, past); err != nil {
		t.Fatal(err)
	}
	tg, err := c.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Install(context.Background(), tg)
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(unitOf(c))
	if err != nil {
		t.Fatal(err)
	}
	if res.UnitUpdated || !res.Updated || !os.SameFile(before, after) || !after.ModTime().Equal(past) || after.Mode().Perm() != 0o600 {
		t.Errorf("result = %+v, unit rewritten", res)
	}
	if readFile(t, target) != "new program" {
		t.Error("program not replaced")
	}
	assertOnlyTarget(t, target)
}

func TestInstallWithoutUnitInArchiveChangesNothing(t *testing.T) {
	s := selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new program", NoUnit: true})
	target := installed(t)
	c := clientFor(s, "v1.0.0", target)
	tg, err := c.Resolve(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Install(context.Background(), tg); err == nil || !strings.Contains(err.Error(), "lib/systemd/system/sysmin.service") {
		t.Fatalf("err = %v", err)
	}
	if readFile(t, target) != oldProgram || readFile(t, unitOf(c)) != oldUnit {
		t.Error("installation changed")
	}
	assertOnlyTarget(t, target)
}

func TestInstallInstallsMissingUnit(t *testing.T) {
	s := selfupdatetest.New(t, "amd64", selfupdatetest.Release{Tag: "v1.1.0", Program: "new program", Unit: "fresh"})
	target := installed(t)
	c := clientFor(s, "v1.0.0", target)
	if err := os.Remove(unitOf(c)); err != nil {
		t.Fatal(err)
	}
	tg, _ := c.Resolve(context.Background(), "")
	res, err := c.Install(context.Background(), tg)
	if err != nil || !res.UnitUpdated || readFile(t, unitOf(c)) != "fresh" {
		t.Errorf("res = %+v, err = %v", res, err)
	}
	assertOnlyTarget(t, target)
}
