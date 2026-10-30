package tui

import (
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

func TestErrorTextNamesUnwritablePaths(t *testing.T) {
	err := &control.Error{Code: control.CodeNotWritable, Paths: []string{"/srv/example/shop/.env", "/srv/example/site"}}
	got := errorText(err)
	if !strings.Contains(got, "/srv/example/shop/.env, /srv/example/site") {
		t.Errorf("errorText = %q", got)
	}
}
