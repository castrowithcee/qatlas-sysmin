package auditlog

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
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
		"host on another action": {Action: OperationConfigRead, Result: ResultSuccess, Host: ok},
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

func TestFieldsAreValidated(t *testing.T) {
	good := Fields{ClientUID: 1000, Action: OperationConfigRead, Result: ResultSuccess, Duration: time.Second}
	if err := good.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(*Fields){
		"unknown action":       func(f *Fields) { f.Action = "made.up" },
		"removed action":       func(f *Fields) { f.Action = "deploy.run" },
		"unknown result":       func(f *Fields) { f.Result = "maybe" },
		"failure without code": func(f *Fields) { f.Result = ResultFailure },
		"failure free text":    func(f *Fields) { f.Result, f.ErrorCode = ResultFailure, "Not Allowed" },
		"success with code":    func(f *Fields) { f.ErrorCode = "denied" },
		"negative duration":    func(f *Fields) { f.Duration = -1 },
		"too long a duration":  func(f *Fields) { f.Duration = MaxDuration + time.Millisecond },
	}
	for name, mutate := range cases {
		f := good
		mutate(&f)
		if f.Validate() == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	failure := good
	failure.Result, failure.ErrorCode, failure.Duration = ResultFailure, "permission-denied", MaxDuration
	if err := failure.Validate(); err != nil {
		t.Errorf("valid failure refused: %v", err)
	}
}

func TestEntryRefusesASignature(t *testing.T) {
	e := Entry{Action: OperationConfigRead, Result: ResultSuccess, MAC: "x"}
	if e.Validate() == nil {
		t.Error("an entry with a signature is accepted for writing")
	}
}

func TestAppendRefusesAnInvalidEntry(t *testing.T) {
	dir := t.TempDir()
	logger, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := logger.Append(Fields{Action: "made.up", Result: ResultSuccess}); err == nil {
		t.Fatal("Append accepted an unknown action")
	}
	if files, _ := filepath.Glob(filepath.Join(dir, "*.jsonl")); len(files) != 0 {
		t.Errorf("wrote files %v", files)
	}
	if _, err := Open(""); err == nil {
		t.Error("an empty directory is accepted")
	}
}
