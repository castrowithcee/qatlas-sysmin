package service

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

const (
	testJournalID = "20300102T030405.000000Z-0a1b2c"
	testUndoID    = "20300102T030400.000000Z-ffeedd"
)

func asPeer(uid uint32) func(*Config) {
	return func(cfg *Config) {
		cfg.Credentials = func(net.Conn) (daemon.Peer, error) {
			return daemon.Peer{PID: int32(os.Getpid()), UID: uid, GID: uint32(os.Getgid())}, nil
		}
	}
}

func hostEntries(t *testing.T, c *client.Client) []control.LogEntry {
	t.Helper()
	res, err := c.LogEntries(control.LogListParams{})
	if err != nil {
		t.Fatal(err)
	}
	return res.Entries
}

func TestHostRecordFromRootWritesASignedEntryForTheHuman(t *testing.T) {
	e := newEnv(t, true, true, asPeer(0))
	e.start(t)
	c := e.dial(t)
	// No admin session and no passphrase: the sudo call is the proof.
	err := c.RecordHostChange(control.HostRecordParams{
		Action: "demo.set", Result: control.HostResultApplied, SudoUID: 1000, JournalID: testJournalID, UndoOf: testUndoID,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got *control.LogEntry
	for _, en := range hostEntries(t, c) {
		if en.Action == auditlog.OperationHostChange {
			got = &en
			break
		}
	}
	if got == nil || got.ClientUID == nil || *got.ClientUID != 1000 || got.Result != "success" || got.Project != "" ||
		!got.Signed || got.Host == nil || *got.Host != (control.LogHost{ActionID: "demo.set", JournalID: testJournalID, UndoOf: testUndoID}) {
		t.Fatalf("entry = %+v", got)
	}
	raw, err := os.ReadFile(e.logFile(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.Contains(line, `"host.change"`) {
			var en map[string]any
			if err := json.Unmarshal([]byte(line), &en); err != nil {
				t.Fatal(err)
			}
			for _, key := range []string{"sentence", "commands", "args", "secret"} {
				if _, ok := en[key]; ok {
					t.Fatalf("entry carries %q: %s", key, line)
				}
			}
		}
	}
}

func TestHostRecordOfAChangeThatDidNotStand(t *testing.T) {
	e := newEnv(t, true, true, asPeer(0))
	e.start(t)
	c := e.dial(t)
	for _, res := range []string{control.HostResultFailed, control.HostResultRestored} {
		if err := c.RecordHostChange(control.HostRecordParams{Action: "demo.set", Result: res, JournalID: testJournalID}); err != nil {
			t.Fatal(err)
		}
	}
	var codes []string
	for _, en := range hostEntries(t, c) {
		if en.Action == auditlog.OperationHostChange && en.Result == "failure" && en.ClientUID != nil && *en.ClientUID == 0 {
			codes = append(codes, en.ErrorCode)
		}
	}
	if len(codes) != 2 || codes[0] != "change-restored" || codes[1] != "change-failed" {
		t.Fatalf("codes = %v", codes)
	}
}

func TestHostRecordRejectsAFreeFormRequest(t *testing.T) {
	e := newEnv(t, true, true, asPeer(0))
	e.start(t)
	c := e.dial(t)
	for _, p := range []control.HostRecordParams{
		{Action: "Demo Set; rm", Result: "applied", JournalID: testJournalID},
		{Action: "demo.set", Result: "pending", JournalID: testJournalID},
		{Action: "demo.set", Result: "applied", JournalID: "not-an-id"},
		{Action: "demo.set", Result: "applied", JournalID: testJournalID, UndoOf: "x"},
		{Action: strings.Repeat("a", 80) + ".b", Result: "applied", JournalID: testJournalID},
	} {
		var ce *control.Error
		if err := c.RecordHostChange(p); !errors.As(err, &ce) || ce.Code != control.CodeBadRequest {
			t.Fatalf("%+v: err = %v", p, err)
		}
	}
	for _, en := range hostEntries(t, c) {
		if en.Host != nil {
			t.Fatalf("a rejected request left a host entry: %+v", en)
		}
	}
}

func TestHostRecordRefusesEveryPeerButRootAndLogsTheRefusal(t *testing.T) {
	e := newEnv(t, true, true, asPeer(1000))
	e.start(t)
	c := e.dial(t)
	err := c.RecordHostChange(control.HostRecordParams{Action: "demo.set", Result: "applied", SudoUID: 0, JournalID: testJournalID})
	var ce *control.Error
	if !errors.As(err, &ce) || ce.Code != control.CodeNotRoot {
		t.Fatalf("err = %v", err)
	}
	var refusal *control.LogEntry
	for _, en := range hostEntries(t, c) {
		if en.Action == auditlog.OperationHostChange || en.Host != nil {
			t.Fatalf("a refused peer left a host entry: %+v", en)
		}
		if en.Action == auditlog.OperationControlRefuse && en.ErrorCode == control.CodeNotRoot {
			refusal = &en
		}
	}
	if refusal == nil || refusal.ClientUID == nil || *refusal.ClientUID != 1000 || refusal.Result != "failure" {
		t.Fatalf("refusal = %+v", refusal)
	}
	files, _ := filepath.Glob(filepath.Join(e.cfg.StateDir, "logs", "*.jsonl"))
	if len(files) == 0 {
		t.Fatal("no log")
	}
}
