//go:build e2efakes

package main

import (
	"errors"
	"os"
	"path/filepath"
	"time"
)

// runDemoApp is the application of the demo container. Without arguments it idles; "check" is the container's
// health check and succeeds only while the process can list and read the mounted folder (DEMO_DIR), so a
// permission change that locks it out makes the container unhealthy.
func runDemoApp(args []string) error {
	dir := os.Getenv("DEMO_DIR")
	if len(args) == 1 && args[0] == "check" {
		if dir == "" {
			return errors.New("DEMO_DIR is not set")
		}
		if _, err := os.ReadDir(dir); err != nil {
			return err
		}
		_, err := os.ReadFile(filepath.Join(dir, "data.txt"))
		return err
	}
	for {
		time.Sleep(time.Hour)
	}
}
