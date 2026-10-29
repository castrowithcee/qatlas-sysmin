package scripts

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/release"
	"github.com/castrowithcee/qatlas-sysmin/internal/release/releasetest"
)

var signerLine = regexp.MustCompile(`(?m)^signer_line='([^']*)'$`)

// stubs are the system commands the installer calls. They record their calls in $STUB_LOG and keep the
// state of the fake user database in $STUB_STATE, so the tests never touch the host.
var stubs = map[string]string{
	"id": `if [ "$#" = 1 ] && [ "$1" = -u ]; then echo "$STUB_UID"; exit 0; fi
case "$*" in
"-nG sysmin") if [ -f "$STUB_STATE/in-docker" ]; then echo "sysmin docker"; else echo sysmin; fi; exit 0 ;;
"sysmin") [ -f "$STUB_STATE/user" ]; exit $? ;;
esac
exit 1`,
	"getent": `[ "$1" = group ] || exit 2
case "$2" in
docker) [ -z "$STUB_NO_DOCKER" ]; exit $? ;;
sysmin) [ -f "$STUB_STATE/group" ]; exit $? ;;
esac
exit 2`,
	"groupadd":        `echo "groupadd $*" >>"$STUB_LOG"; touch "$STUB_STATE/group"`,
	"useradd":         `echo "useradd $*" >>"$STUB_LOG"; touch "$STUB_STATE/user" "$STUB_STATE/in-docker"`,
	"usermod":         `echo "usermod $*" >>"$STUB_LOG"; touch "$STUB_STATE/in-docker"`,
	"chown":           `echo "chown $*" >>"$STUB_LOG"`,
	"systemctl":       `echo "systemctl $*" >>"$STUB_LOG"`,
	"systemd-creds":   `exit 0`,
	"systemd-analyze": `echo "$STUB_TPM2"`,
}

type env struct {
	t       *testing.T
	server  *releasetest.Server
	signer  ssh.Signer
	script  string // installer copy that trusts the test key
	root    string
	stubDir string
	state   string
	log     string
	uid     string
	noDock  bool
	tpm2    string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("install.sh supports Linux only")
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("no release archive for " + runtime.GOARCH)
	}
	for _, tool := range []string{"sh", "curl", "tar", "ssh-keygen", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	dir := t.TempDir()
	e := &env{t: t, server: releasetest.NewServer(t), signer: releasetest.NewSigner(t), uid: "0", tpm2: "no",
		root: filepath.Join(dir, "root"), stubDir: filepath.Join(dir, "stubs"), state: filepath.Join(dir, "state"),
		log: filepath.Join(dir, "calls.log")}
	for _, d := range []string{e.root, e.stubDir, e.state} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(e.stubDir, name), []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(e.log, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	line := "sysmin-release " + strings.TrimSpace(string(ssh.MarshalAuthorizedKey(e.signer.PublicKey())))
	e.script = filepath.Join(dir, "install.sh")
	original := readFile(t, "install.sh")
	if !signerLine.MatchString(original) {
		t.Fatal("install.sh has no signer_line")
	}
	patched := signerLine.ReplaceAllLiteralString(original, "signer_line='"+line+"'")
	if err := os.WriteFile(e.script, []byte(patched), 0o755); err != nil {
		t.Fatal(err)
	}
	return e
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func archiveName(tag string) string {
	return fmt.Sprintf("sysmin_%s_linux_%s.tar.gz", tag, runtime.GOARCH)
}

func tarball(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		mode := int64(0o644)
		if strings.HasPrefix(name, "bin/") {
			mode = 0o755
		}
		if err := tw.WriteHeader(&tar.Header{Name: "./" + name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

type releaseOptions struct {
	wrongChecksum bool
	signer        ssh.Signer // nil: the environment's test key
}

// publish adds a release whose binary is the text program and makes it the latest.
func (e *env) publish(tag, program string, opts releaseOptions) {
	e.t.Helper()
	archive := tarball(e.t, map[string]string{
		"bin/sysmin":                        program,
		"lib/systemd/system/sysmin.service": "[Unit]\nDescription=" + tag + "\n",
		"share/doc/sysmin/LICENSE":          "license " + tag + "\n",
	})
	sum := sha256.Sum256(archive)
	hash := hex.EncodeToString(sum[:])
	if opts.wrongChecksum {
		hash = strings.Repeat("0", 64)
	}
	checksums := []byte(hash + "  " + archiveName(tag) + "\n" + strings.Repeat("1", 64) + "  install.sh\n")
	signer := opts.signer
	if signer == nil {
		signer = e.signer
	}
	e.server.Publish(tag, map[string][]byte{
		archiveName(tag):    archive,
		"checksums.txt":     checksums,
		"checksums.txt.sig": releasetest.Sign(e.t, signer, release.Namespace, "sha512", checksums),
	})
}

// run executes the installer with a clean environment; script "" means the test copy.
func (e *env) run(script string, stdin bool, args ...string) (string, error) {
	e.t.Helper()
	if script == "" {
		script = e.script
	}
	args = append([]string{"--release-url", e.server.URL}, args...)
	var cmd *exec.Cmd
	if stdin {
		f, err := os.Open(script)
		if err != nil {
			e.t.Fatal(err)
		}
		defer f.Close()
		cmd = exec.Command("sh", append([]string{"-s", "--"}, args...)...)
		cmd.Stdin = f
	} else {
		cmd = exec.Command("sh", append([]string{script}, args...)...)
	}
	cmd.Env = []string{
		"PATH=" + e.stubDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"TMPDIR=" + e.t.TempDir(),
		"SYSMIN_INSTALL_ROOT=" + e.root,
		"STUB_LOG=" + e.log, "STUB_STATE=" + e.state, "STUB_UID=" + e.uid, "STUB_TPM2=" + e.tpm2,
	}
	if e.noDock {
		cmd.Env = append(cmd.Env, "STUB_NO_DOCKER=1")
	}
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *env) mustRun(args ...string) string {
	e.t.Helper()
	out, err := e.run("", false, args...)
	if err != nil {
		e.t.Fatalf("install.sh failed: %v\n%s", err, out)
	}
	return out
}

func (e *env) calls() string { return readFile(e.t, e.log) }

// assertUntouched fails when the installer changed the fake root or ran a system command.
func (e *env) assertUntouched() {
	e.t.Helper()
	if entries, _ := os.ReadDir(e.root); len(entries) != 0 {
		e.t.Errorf("root changed: %v", entries)
	}
	if calls := e.calls(); calls != "" {
		e.t.Errorf("system commands ran:\n%s", calls)
	}
}

func TestEmbeddedKeyMatchesAllowedSigners(t *testing.T) {
	m := signerLine.FindStringSubmatch(readFile(t, "install.sh"))
	if m == nil {
		t.Fatal("install.sh has no signer_line")
	}
	allowed := strings.TrimSpace(readFile(t, filepath.Join("..", ".github", "release-allowed-signers")))
	if m[1] != allowed {
		t.Errorf("embedded key %q differs from .github/release-allowed-signers %q", m[1], allowed)
	}
	if !strings.HasPrefix(m[1], release.Namespace+" ssh-ed25519 ") {
		t.Errorf("unexpected signer line %q", m[1])
	}
}

func TestInstallNeedsRoot(t *testing.T) {
	e := newEnv(t)
	e.uid = "1000"
	e.publish("v1.0.0", "program", releaseOptions{})
	out, err := e.run("", false)
	if err == nil || !strings.Contains(out, "must run as root") {
		t.Fatalf("err = %v\n%s", err, out)
	}
	e.assertUntouched()
}

func TestInstallRejectsBadReleaseWithoutChange(t *testing.T) {
	other := releasetest.NewSigner(t)
	tests := map[string]struct {
		opts    releaseOptions
		embed   bool // run the unpatched script, which trusts only the real release key
		message string
	}{
		"wrong checksum":       {releaseOptions{wrongChecksum: true}, false, "SHA-256 mismatch"},
		"foreign signature":    {releaseOptions{signer: other}, false, "signature"},
		"embedded key differs": {releaseOptions{}, true, "signature"},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.publish("v1.0.0", "program", tc.opts)
			script := ""
			if tc.embed {
				script = "install.sh"
			}
			out, err := e.run(script, false)
			if err == nil || !strings.Contains(out, tc.message) {
				t.Fatalf("err = %v\n%s", err, out)
			}
			e.assertUntouched()
		})
	}
}

func TestInstallAndReinstall(t *testing.T) {
	e := newEnv(t)
	e.publish("v1.0.0", "program one", releaseOptions{})
	out := e.mustRun()
	if !strings.Contains(out, "v1.0.0") {
		t.Errorf("output:\n%s", out)
	}

	binary := filepath.Join(e.root, "usr/local/bin/sysmin")
	unit := filepath.Join(e.root, "etc/systemd/system/sysmin.service")
	if got := readFile(t, binary); got != "program one" {
		t.Errorf("binary = %q", got)
	}
	if got := readFile(t, unit); !strings.Contains(got, "v1.0.0") {
		t.Errorf("unit = %q", got)
	}
	for path, want := range map[string]os.FileMode{
		binary: 0o755, unit: 0o644,
		filepath.Join(e.root, "etc/sysmin"): 0o750, filepath.Join(e.root, "var/lib/sysmin"): 0o700,
	} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != want {
			t.Errorf("%s: %v, %v (want %v)", path, info, err, want)
		}
	}
	calls := e.calls()
	for _, want := range []string{
		"groupadd --system sysmin", "useradd --system --gid sysmin --groups docker --no-create-home",
		"chown sysmin:sysmin " + filepath.Join(e.root, "etc/sysmin"), "systemctl daemon-reload", "systemctl enable --now sysmin",
	} {
		if !strings.Contains(calls, want) {
			t.Errorf("calls lack %q:\n%s", want, calls)
		}
	}
	if strings.Contains(calls, "restart") {
		t.Errorf("fresh install restarted:\n%s", calls)
	}

	e.publish("v1.0.1", "program two", releaseOptions{})
	if err := os.WriteFile(e.log, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e.mustRun()
	if got := readFile(t, binary); got != "program two" {
		t.Errorf("binary after reinstall = %q", got)
	}
	calls = e.calls()
	if strings.Contains(calls, "useradd") || strings.Contains(calls, "groupadd") || strings.Contains(calls, "usermod") {
		t.Errorf("reinstall recreated the account:\n%s", calls)
	}
	if !strings.Contains(calls, "systemctl restart sysmin") || strings.Contains(calls, "enable --now") {
		t.Errorf("reinstall calls:\n%s", calls)
	}
	for _, dir := range []string{"usr/local/bin", "etc/systemd/system"} {
		entries, _ := os.ReadDir(filepath.Join(e.root, dir))
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") {
				t.Errorf("staging left behind: %s/%s", dir, entry.Name())
			}
		}
	}
}

func TestInstallFromStdinWithOptions(t *testing.T) {
	e := newEnv(t)
	e.publish("v1.2.3-rc.1", "candidate", releaseOptions{})
	e.publish("v1.0.0", "stable", releaseOptions{})
	out, err := e.run("", true, "--version", "v1.2.3-rc.1", "--auto-unlock", "host")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got := readFile(t, filepath.Join(e.root, "usr/local/bin/sysmin")); got != "candidate" {
		t.Errorf("binary = %q", got)
	}
	if !strings.Contains(out, "sudo sysmin vault bind") {
		t.Errorf("no bind hint:\n%s", out)
	}
	if out, err := e.run("", false, "--version", "latest"); err == nil || !strings.Contains(out, "invalid version") {
		t.Errorf("invalid version: %v\n%s", err, out)
	}
	// Without a version the newest published release (the stable one here) is installed.
	e.mustRun()
	if got := readFile(t, filepath.Join(e.root, "usr/local/bin/sysmin")); got != "stable" {
		t.Errorf("latest binary = %q", got)
	}
}

func TestInstallPreflightFailuresChangeNothing(t *testing.T) {
	t.Run("no docker group", func(t *testing.T) {
		e := newEnv(t)
		e.noDock = true
		e.publish("v1.0.0", "program", releaseOptions{})
		out, err := e.run("", false)
		if err == nil || !strings.Contains(out, "group docker does not exist") {
			t.Fatalf("err = %v\n%s", err, out)
		}
		e.assertUntouched()
	})
	t.Run("tpm2 requested without tpm2", func(t *testing.T) {
		e := newEnv(t)
		e.publish("v1.0.0", "program", releaseOptions{})
		out, err := e.run("", false, "--auto-unlock", "tpm2")
		if err == nil || !strings.Contains(out, "no usable TPM2") {
			t.Fatalf("err = %v\n%s", err, out)
		}
		e.assertUntouched()
	})
	t.Run("invalid unlock mode", func(t *testing.T) {
		e := newEnv(t)
		e.publish("v1.0.0", "program", releaseOptions{})
		if out, err := e.run("", false, "--auto-unlock", "magic"); err == nil || !strings.Contains(out, "invalid --auto-unlock") {
			t.Fatalf("err = %v\n%s", err, out)
		}
		e.assertUntouched()
	})
}

func TestInstallTPM2Hint(t *testing.T) {
	e := newEnv(t)
	e.tpm2 = "yes"
	e.publish("v1.0.0", "program", releaseOptions{})
	out, err := e.run("", false, "--auto-unlock", "tpm2")
	if err != nil || !strings.Contains(out, "sudo sysmin vault bind") {
		t.Fatalf("err = %v\n%s", err, out)
	}
}

func TestReleaseIsReproducible(t *testing.T) {
	for _, tool := range []string{"go", "tar", "gzip", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	build := func() string {
		out := filepath.Join(t.TempDir(), "dist")
		cmd := exec.Command("sh", "release.sh", "v0.0.1-test", out)
		cmd.Env = append(os.Environ(), "SOURCE_DATE_EPOCH=1700000000")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("release.sh: %v\n%s", err, b)
		}
		return out
	}
	first, second := build(), build()
	sums := readFile(t, filepath.Join(first, "checksums.txt"))
	if sums != readFile(t, filepath.Join(second, "checksums.txt")) {
		t.Errorf("checksums differ between runs:\n%s\n%s", sums, readFile(t, filepath.Join(second, "checksums.txt")))
	}
	for _, name := range []string{"sysmin_v0.0.1-test_linux_amd64.tar.gz", "sysmin_v0.0.1-test_linux_arm64.tar.gz", "install.sh"} {
		if !strings.Contains(sums, "  "+name+"\n") {
			t.Errorf("checksums.txt lacks %s:\n%s", name, sums)
		}
	}

	f, err := os.Open(filepath.Join(first, "sysmin_v0.0.1-test_linux_amd64.tar.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(gz)
	seen := map[string]bool{}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		seen[strings.TrimPrefix(h.Name, "./")] = true
		if h.Uid != 0 || h.Gid != 0 || h.ModTime.Unix() != 1700000000 {
			t.Errorf("%s: owner %d:%d mtime %v", h.Name, h.Uid, h.Gid, h.ModTime)
		}
	}
	for _, want := range []string{"bin/sysmin", "share/doc/sysmin/LICENSE", "lib/systemd/system/sysmin.service"} {
		if !seen[want] {
			t.Errorf("archive lacks %s: %v", want, seen)
		}
	}
}
