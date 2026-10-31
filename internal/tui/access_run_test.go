package tui

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

func TestRunDrawsNothingWithoutAccess(t *testing.T) {
	for _, denied := range []error{client.ErrReserved, client.ErrNotEnabled} {
		fb := newFake(control.VaultUnlocked)
		fb.down = denied
		dir := t.TempDir()
		in, err := os.Create(filepath.Join(dir, "in"))
		if err != nil {
			t.Fatal(err)
		}
		out, err := os.Create(filepath.Join(dir, "out"))
		if err != nil {
			t.Fatal(err)
		}
		err = Run(Options{Backend: fb, In: in, Out: out})
		if !errors.Is(err, denied) {
			t.Errorf("Run = %v, want %v", err, denied)
		}
		if st, _ := out.Stat(); st.Size() != 0 {
			t.Errorf("%v: %d bytes were written to the terminal", denied, st.Size())
		}
	}
}

func TestAccessTakenAwayQuitsTheInterface(t *testing.T) {
	fb := overviewFake()
	m := overviewModel(t, fb, 120, 40)
	fb.down = client.ErrReserved
	m.svc.pending = true
	cmd := m.takeStatus(statusMsg{err: client.ErrReserved})
	if cmd == nil {
		t.Fatal("no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("the interface does not quit")
	}
	if !errors.Is(m.denied, client.ErrReserved) {
		t.Errorf("denied = %v", m.denied)
	}
	if m.ovr.haveApprovals || m.ovr.haveProjects {
		t.Error("the overview still holds data")
	}
}

func TestErrorTextForAccessIsTheOneLine(t *testing.T) {
	useLanguage(t, "en")
	if got := errorText(client.ErrReserved); got != client.ReservedMessage {
		t.Errorf("reserved = %q", got)
	}
	if got := errorText(client.ErrNotEnabled); got == "" || got == client.NotEnabledMessage {
		t.Errorf("not enabled = %q", got)
	}
	useLanguage(t, "de")
	if got := errorText(client.ErrReserved); got != "sysmin ist Administratoren vorbehalten (sudo)" {
		t.Errorf("reserved (de) = %q", got)
	}
}
