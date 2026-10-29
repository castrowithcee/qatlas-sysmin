// Package selfupdatetest serves a local stand-in for the GitHub releases API of the sysmin repository.
package selfupdatetest

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-sysmin/internal/release"
	"github.com/castrowithcee/qatlas-sysmin/internal/release/releasetest"
)

// Release describes one published release. Zero values give a correctly signed stable release for the
// architecture Arch.
type Release struct {
	Tag        string
	Prerelease bool
	Draft      bool
	// Program is the content of bin/sysmin in the archive.
	Program string
	// Signer signs checksums.txt; nil means the server's signer.
	Signer ssh.Signer
	// Namespace is the signature namespace; empty means the release namespace.
	Namespace string
	// BadChecksum lists a wrong checksum for the archive.
	BadChecksum bool
	// NoSignature leaves checksums.txt.sig out of the release.
	NoSignature bool
}

// Server is the fake API. Its URL is the release source base.
type Server struct {
	*httptest.Server
	// Signer is the key the releases are signed with by default; add its public key to the client under test.
	Signer   ssh.Signer
	arch     string
	releases []Release
	assets   map[string]map[string][]byte
}

// New starts a server for archives of the given architecture and publishes releases, oldest first.
func New(t testing.TB, arch string, releases ...Release) *Server {
	t.Helper()
	s := &Server{Signer: releasetest.NewSigner(t), arch: arch, assets: map[string]map[string][]byte{}}
	for _, r := range releases {
		s.releases = append(s.releases, r)
		s.assets[r.Tag] = s.build(t, r)
	}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Server.Close)
	return s
}

func (s *Server) build(t testing.TB, r Release) map[string][]byte {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct{ name, body string }{{"bin/sysmin", r.Program}, {"share/doc/sysmin/LICENSE", "license"}} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: 0o755, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("sysmin_%s_linux_%s.tar.gz", r.Tag, s.arch)
	sum := sha256.Sum256(buf.Bytes())
	if r.BadChecksum {
		sum[0] ^= 0xff
	}
	checksums := []byte(hex.EncodeToString(sum[:]) + "  " + name + "\n")
	signer := r.Signer
	if signer == nil {
		signer = s.Signer
	}
	namespace := r.Namespace
	if namespace == "" {
		namespace = release.Namespace
	}
	assets := map[string][]byte{name: buf.Bytes(), "checksums.txt": checksums}
	if !r.NoSignature {
		assets["checksums.txt.sig"] = releasetest.Sign(t, signer, namespace, "sha256", checksums)
	}
	return assets
}

func (s *Server) body(r Release) map[string]any {
	var assets []map[string]string
	for name := range s.assets[r.Tag] {
		assets = append(assets, map[string]string{"name": name, "url": s.URL + "/assets/" + r.Tag + "/" + name})
	}
	return map[string]any{"tag_name": r.Tag, "draft": r.Draft, "prerelease": r.Prerelease, "assets": assets}
}

func (s *Server) serve(w http.ResponseWriter, req *http.Request) {
	path := req.URL.Path
	switch {
	case path == "/releases/latest":
		for i := len(s.releases) - 1; i >= 0; i-- {
			if r := s.releases[i]; !r.Prerelease && !r.Draft {
				_ = json.NewEncoder(w).Encode(s.body(r))
				return
			}
		}
		http.NotFound(w, req)
	case path == "/releases":
		all := []map[string]any{}
		if req.URL.Query().Get("page") == "1" {
			for _, r := range s.releases {
				all = append(all, s.body(r))
			}
		}
		_ = json.NewEncoder(w).Encode(all)
	case strings.HasPrefix(path, "/releases/tags/"):
		for _, r := range s.releases {
			if r.Tag == strings.TrimPrefix(path, "/releases/tags/") {
				_ = json.NewEncoder(w).Encode(s.body(r))
				return
			}
		}
		http.NotFound(w, req)
	case strings.HasPrefix(path, "/assets/"):
		tag, name, _ := strings.Cut(strings.TrimPrefix(path, "/assets/"), "/")
		if data, ok := s.assets[tag][name]; ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, req)
	default:
		http.NotFound(w, req)
	}
}
