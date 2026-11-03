package uiprefs

import (
	"path/filepath"
	"testing"
)

func TestUserDirIsTheQsysminFolderOfTheHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir, err := UserDir()
	if err != nil || dir != filepath.Join(home, ".qatlas", "qsysmin") {
		t.Errorf("UserDir() = %q, %v", dir, err)
	}
}
