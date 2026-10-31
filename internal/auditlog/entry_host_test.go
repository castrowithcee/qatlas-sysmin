package auditlog

import (
	"strings"
	"testing"
)

func TestHostChangeFieldsAreValidated(t *testing.T) {
	ok := &Host{ActionID: "demo.set", JournalID: "20300102T030405.000000Z-0a1b2c"}
	valid := Fields{ClientUID: 1000, Action: OperationHostChange, Result: ResultSuccess, Host: ok}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}
	refused := Fields{ClientUID: 5, Action: OperationHostChange, Result: ResultFailure, ErrorCode: "bad-request"}
	if err := refused.Validate(); err != nil {
		t.Fatalf("a rejected request has no host: %v", err)
	}
	for name, f := range map[string]Fields{
		"success without host":   {Action: OperationHostChange, Result: ResultSuccess},
		"host on another action": {Action: OperationDeployRun, Result: ResultSuccess, Host: ok},
		"free text action":       {Action: OperationHostChange, Result: ResultSuccess, Host: &Host{ActionID: "set the password", JournalID: ok.JournalID}},
		"long action":            {Action: OperationHostChange, Result: ResultSuccess, Host: &Host{ActionID: strings.Repeat("a", 70) + ".b", JournalID: ok.JournalID}},
		"bad journal id":         {Action: OperationHostChange, Result: ResultSuccess, Host: &Host{ActionID: "demo.set", JournalID: "x"}},
		"bad undo id":            {Action: OperationHostChange, Result: ResultSuccess, Host: &Host{ActionID: "demo.set", JournalID: ok.JournalID, UndoOf: "x"}},
	} {
		if f.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestHostChangeEntryIsChainedAndSigned(t *testing.T) {
	key := testKey(t, fakeVaultKey)
	logger, _ := signedTestLogger(t, key, day(1))
	host := &Host{ActionID: "demo.set", JournalID: "20300102T030405.000000Z-0a1b2c"}
	mustAppend(t, logger, OperationDeployRun)
	if err := logger.Append(Fields{ClientUID: 1000, Action: OperationHostChange, Result: ResultSuccess, Host: host}); err != nil {
		t.Fatal(err)
	}
	mustAppend(t, logger, OperationDeployRun)
	if report := verify(t, logger.dir, key); report.Broken {
		t.Fatalf("report = %+v", report)
	}
	page, err := Read(logger.dir, Query{Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, e := range page.Entries {
		if e.Action == OperationHostChange {
			found = e.Host != nil && *e.Host == *host && e.MAC != "" && *e.ClientUID == 1000 && e.Project == ""
		}
	}
	if !found {
		t.Fatalf("entries = %+v", page.Entries)
	}
}
