//go:build e2efakes

package main

import (
	"fmt"
	"net/http"
)

// runDemoApp is the application inside the demo images the scenario pushes to the test registry. It answers
// GET / with its name and version and GET /health with 200, or with 500 when DEMO_HEALTH=fail, which is how
// the scenario builds a release whose health check fails.
func runDemoApp() error {
	name, version := env("DEMO_NAME", "demo"), env("DEMO_VERSION", "unknown")
	healthy := env("DEMO_HEALTH", "ok") != "fail"
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		if !healthy {
			http.Error(w, "unhealthy", http.StatusInternalServerError)
			return
		}
		fmt.Fprintln(w, "ok")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintf(w, "%s %s\n", name, version)
	})
	return http.ListenAndServe(env("DEMO_ADDR", ":8080"), mux)
}
