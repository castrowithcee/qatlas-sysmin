//go:build e2efakes

package main

import "net/http"

const (
	certFile = "/certs/server.crt"
	keyFile  = "/certs/server.key"
)

// serveTLS listens on addr with the shared test certificate.
func serveTLS(addr string, h http.Handler) error {
	return (&http.Server{Addr: addr, Handler: h}).ListenAndServeTLS(certFile, keyFile)
}
