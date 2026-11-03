package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"github.com/castrowithcee/qatlas-kit/release/releasetest"
	"github.com/castrowithcee/qatlas-sysmin/internal/release"
)

const fakeRepoPath = "/repos/castrowithcee/qatlas-sysmin"

// fakeRelease describes one published release. Zero values give a correctly signed stable release.
type fakeRelease struct {
	Tag        string
	Prerelease bool
	Draft      bool
	// Program is the content of bin/qsysmin in the archive.
	Program string
	// Unit is the content of lib/systemd/system/qsysmin.service; empty means a unit naming the tag.
	Unit string
	// NoUnit leaves the unit out of the archive.
	NoUnit bool
	// Manpage is the content of share/man/man1/qsysmin.1; empty means a page naming the tag.
	Manpage string
	// NoManpage leaves the manpage out of the archive.
	NoManpage bool
	// Files replaces the archive content when set.
	Files []releasetest.File
	// Signer signs checksums.txt; nil means the server's signer.
	Signer ssh.Signer
	// Namespace is the signature namespace; empty means the release namespace.
	Namespace string
	// BadChecksum lists a wrong checksum for the archive.
	BadChecksum bool
	// NoSignature leaves checksums.txt.sig out of the release.
	NoSignature bool
	// ArchiveURL replaces the URL of the archive asset in the release metadata.
	ArchiveURL string
	// DuplicateArchive lists the archive asset twice.
	DuplicateArchive bool
}

// fakeServer is a local stand-in for the GitHub releases API of the qsysmin repository, with numeric asset
// IDs as GitHub has them.
type fakeServer struct {
	*httptest.Server
	// Signer signs the releases by default; its public key is added to the client under test.
	Signer   ssh.Signer
	releases []fakeRelease
	assets   map[int][]byte
	listed   map[string][]fakeAsset
}

type fakeAsset struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func newFakeServer(t testing.TB, arch string, releases ...fakeRelease) *fakeServer {
	t.Helper()
	s := &fakeServer{Signer: releasetest.NewSigner(t), assets: map[int][]byte{}, listed: map[string][]fakeAsset{}}
	s.Server = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.Server.Close)
	for _, r := range releases {
		s.publish(t, arch, r)
	}
	return s
}

// API is the release source base to hand to the client under test.
func (s *fakeServer) API() string { return s.URL + fakeRepoPath }

func (s *fakeServer) add(name string, body []byte) fakeAsset {
	id := len(s.assets) + 1
	s.assets[id] = body
	return fakeAsset{Name: name, URL: s.API() + "/releases/assets/" + strconv.Itoa(id)}
}

func (s *fakeServer) publish(t testing.TB, arch string, r fakeRelease) {
	name := archiveNamer(arch)(r.Tag)
	files := r.Files
	if files == nil {
		unit := r.Unit
		if unit == "" {
			unit = "[Unit]\nDescription=" + r.Tag + "\n"
		}
		files = []releasetest.File{{Name: "bin/qsysmin", Body: []byte(r.Program)}, {Name: "share/doc/qsysmin/LICENSE", Body: []byte("license")}}
		if !r.NoUnit {
			files = append(files, releasetest.File{Name: unitEntry, Body: []byte(unit)})
		}
		if !r.NoManpage {
			page := r.Manpage
			if page == "" {
				page = ".TH QSYSMIN 1 \"" + r.Tag + "\"\n"
			}
			files = append(files, releasetest.File{Name: manEntry, Body: []byte(page)})
		}
	}
	archive := releasetest.Archive(t, name, files...)
	checksums := releasetest.Checksums(map[string][]byte{name: archive})
	if r.BadChecksum {
		checksums = []byte(strings.Repeat("0", 64) + "  " + name + "\n")
	}
	signer := r.Signer
	if signer == nil {
		signer = s.Signer
	}
	namespace := r.Namespace
	if namespace == "" {
		namespace = release.Namespace
	}
	listed := []fakeAsset{s.add(name, archive), s.add("checksums.txt", checksums)}
	if r.ArchiveURL != "" {
		listed[0].URL = r.ArchiveURL
	}
	if r.DuplicateArchive {
		listed = append(listed, s.add(name, archive))
	}
	if !r.NoSignature {
		listed = append(listed, s.add("checksums.txt.sig", releasetest.Sign(t, signer, namespace, "sha512", checksums)))
	}
	s.releases = append(s.releases, r)
	s.listed[r.Tag] = listed
}

func (s *fakeServer) body(r fakeRelease) map[string]any {
	return map[string]any{"tag_name": r.Tag, "draft": r.Draft, "prerelease": r.Prerelease, "assets": s.listed[r.Tag]}
}

func (s *fakeServer) serve(w http.ResponseWriter, req *http.Request) {
	path := strings.TrimPrefix(req.URL.Path, fakeRepoPath)
	if path == req.URL.Path {
		http.NotFound(w, req)
		return
	}
	switch {
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
	case strings.HasPrefix(path, "/releases/assets/"):
		id, err := strconv.Atoi(strings.TrimPrefix(path, "/releases/assets/"))
		if data, ok := s.assets[id]; err == nil && ok {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, req)
	default:
		http.NotFound(w, req)
	}
}
