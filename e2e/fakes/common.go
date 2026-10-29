//go:build e2efakes

package main

import (
	"crypto/subtle"
	"net/http"
	"os"
)

const (
	certFile = "/certs/server.crt"
	keyFile  = "/certs/server.key"
)

func env(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
}

// serveTLS listens on addr with the shared test certificate.
func serveTLS(addr string, h http.Handler) error {
	return (&http.Server{Addr: addr, Handler: h}).ListenAndServeTLS(certFile, keyFile)
}

// basicAuth wraps h and requires the given credentials.
func basicAuth(user, password string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, p, ok := r.BasicAuth()
		if !ok || subtle.ConstantTimeCompare([]byte(u), []byte(user)) != 1 ||
			subtle.ConstantTimeCompare([]byte(p), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="sysmine2e"`)
			http.Error(w, "authentication required", http.StatusUnauthorized)
			return
		}
		h.ServeHTTP(w, r)
	})
}
