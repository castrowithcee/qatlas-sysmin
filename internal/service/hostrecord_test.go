package service

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

const (
	testJournalID = "20300102T030405.000000Z-0a1b2c"
	testUndoID    = "20300102T030400.000000Z-ffeedd"
)

func hostEntries(t *testing.T, c *client.Client) []control.LogEntry {
	t.Helper()
	res, err := c.LogEntries(control.LogListParams{})
	if err != nil {
		t.Fatal(err)
	}
	return res.Entries
}

func recordOf(t *testing.T, p control.HostRecordParams) (uint32, auditlog.Host, string, error) {
	t.Helper()
	params, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	return hostRecord(params)
}

func TestHostRecordNamesTheHumanBehindSudo(t *testing.T) {
	uid, host, code, err := recordOf(t, control.HostRecordParams{
		Action: "demo.set", Result: control.HostResultApplied, SudoUID: 1000, JournalID: testJournalID, UndoOf: testUndoID,
	})
	if err != nil || uid != 1000 || code != "" ||
		host != (auditlog.Host{ActionID: "demo.set", JournalID: testJournalID, UndoOf: testUndoID}) {
		t.Fatalf("record = %d, %+v, %q, %v", uid, host, code, err)
	}
}

func TestHostRecordOfAChangeThatDidNotStand(t *testing.T) {
	for result, want := range map[string]string{
		control.HostResultFailed:   "change-failed",
		control.HostResultRestored: "change-restored",
	} {
		if _, _, code, err := recordOf(t, control.HostRecordParams{Action: "demo.set", Result: result, JournalID: testJournalID}); err != nil || code != want {
			t.Errorf("%s: code = %q, %v", result, code, err)
		}
	}
}

func TestHostRecordRejectsAFreeFormRequest(t *testing.T) {
	for _, p := range []control.HostRecordParams{
		{Action: "Demo Set; rm", Result: "applied", JournalID: testJournalID},
		{Action: "demo.set", Result: "pending", JournalID: testJournalID},
		{Action: "demo.set", Result: "applied", JournalID: "not-an-id"},
		{Action: "demo.set", Result: "applied", JournalID: testJournalID, UndoOf: "x"},
		{Action: strings.Repeat("a", 80) + ".b", Result: "applied", JournalID: testJournalID},
	} {
		var ce *control.Error
		if _, _, _, err := recordOf(t, p); !errors.As(err, &ce) || ce.Code != control.CodeBadRequest {
			t.Fatalf("%+v: err = %v", p, err)
		}
	}
	if _, _, _, err := hostRecord(json.RawMessage(`[1]`)); err == nil {
		t.Fatal("a request that is no object was accepted")
	}
}

func TestHostRecordRefusesEveryPeerButRootAndLogsTheRefusal(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("the test process is root")
	}
	e := newEnv(t)
	e.start(t)
	c := e.dial(t)
	err := c.RecordHostChange(control.HostRecordParams{Action: "demo.set", Result: "applied", SudoUID: 0, JournalID: testJournalID})
	wantCode(t, err, control.CodeNotRoot)
	var refusal *control.LogEntry
	for _, en := range hostEntries(t, c) {
		if en.Action == auditlog.OperationHostChange || en.Host != nil {
			t.Fatalf("a refused peer left a host entry: %+v", en)
		}
		if en.Action == auditlog.OperationControlRefuse && en.ErrorCode == control.CodeNotRoot {
			refusal = &en
		}
	}
	if refusal == nil || refusal.ClientUID == nil || *refusal.ClientUID != uint32(os.Getuid()) || refusal.Result != "failure" {
		t.Fatalf("refusal = %+v", refusal)
	}
}
