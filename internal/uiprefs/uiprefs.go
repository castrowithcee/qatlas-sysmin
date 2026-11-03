// Package uiprefs names where the device-local preferences of the terminal editor live: tui.yaml in the
// per-user folder ~/.qatlas/qsysmin, apart from the service configuration, so the file is never part of an
// export or import. The kit's uiprefs package reads and writes the file under the schema of the editor.
package uiprefs

import (
	"os"
	"path/filepath"
)

// UserDir returns the per-user qsysmin folder, ~/.qatlas/qsysmin.
func UserDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".qatlas", "qsysmin"), nil
}
