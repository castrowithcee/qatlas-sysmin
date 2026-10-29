// Package registrytest is a small registry server for tests of code that reads registries: a distribution API
// with a bearer token flow, a few repositories and the manifests and labels of their tags.
package registrytest

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Bearer is the token the server's token endpoint hands out.
const Bearer = "demo-bearer-value-not-real"

// Config describes the server.
type Config struct {
	// User and Token are what the token endpoint accepts as basic credentials.
	User, Token string
	// Repos maps a repository name such as "demo/app" to its tags.
	Repos map[string][]string
	// Labels are the image labels of every tag.
	Labels map[string]string
}

// Server is a running test registry.
type Server struct {
	// URL is the base URL of the server.
	URL string

	cfg  Config
	srv  *httptest.Server
	mu   sync.Mutex
	reqs []string
}

// New starts a server that stops with the test.
func New(t testing.TB, cfg Config) *Server {
	t.Helper()
	s := &Server{cfg: cfg}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	s.URL = s.srv.URL
	t.Cleanup(s.srv.Close)
	return s
}

// Requests returns "METHOD path" of every request so far.
func (s *Server) Requests() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reqs...)
}

func sum(b []byte) string { d := sha256.Sum256(b); return fmt.Sprintf("sha256:%x", d[:]) }

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.reqs = append(s.reqs, r.Method+" "+r.URL.Path)
	s.mu.Unlock()
	if r.URL.Path == "/token" {
		if u, p, ok := r.BasicAuth(); !ok || u != s.cfg.User || p != s.cfg.Token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"token": Bearer})
		return
	}
	rest, ok := strings.CutPrefix(r.URL.Path, "/v2/")
	if !ok {
		http.NotFound(w, r)
		return
	}
	if r.Header.Get("Authorization") != "Bearer "+Bearer {
		w.Header().Set("WWW-Authenticate", fmt.Sprintf(`Bearer realm="%s/token",service="demo.registry"`, s.URL))
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	for name, tags := range s.cfg.Repos {
		switch {
		case rest == name+"/tags/list":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": name, "tags": tags})
			return
		case strings.HasPrefix(rest, name+"/manifests/"):
			s.manifest(w, r, strings.TrimPrefix(rest, name+"/manifests/"), tags)
			return
		case strings.HasPrefix(rest, name+"/blobs/"):
			cfg, _ := json.Marshal(map[string]any{"config": map[string]any{"Labels": s.cfg.Labels}})
			_, _ = w.Write(cfg)
			return
		}
	}
	http.NotFound(w, r)
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request, ref string, tags []string) {
	known := false
	for _, t := range tags {
		known = known || t == ref
	}
	if !known {
		http.NotFound(w, r)
		return
	}
	cfg, _ := json.Marshal(map[string]any{"config": map[string]any{"Labels": s.cfg.Labels}})
	doc, _ := json.Marshal(map[string]any{
		"mediaType": "application/vnd.oci.image.manifest.v1+json",
		"config":    map[string]any{"digest": sum(cfg)},
		"tag":       ref,
	})
	w.Header().Set("Docker-Content-Digest", sum(doc))
	w.Header().Set("Content-Type", "application/vnd.oci.image.manifest.v1+json")
	if r.Method != http.MethodHead {
		_, _ = w.Write(doc)
	}
}
