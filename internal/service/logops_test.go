package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/client"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
)

// logEnv is a started service whose log already holds a few entries.
func logEnv(t *testing.T) (*env, *client.Client) {
	t.Helper()
	e := newEnv(t)
	e.start(t)
	c := e.dial(t)
	for i := 0; i < 4; i++ {
		if _, err := c.Status(); err != nil {
			t.Fatal(err)
		}
	}
	_ = c.Call("no.such-op", nil, nil)
	return e, c
}

func (e *env) logFile(t *testing.T) string {
	t.Helper()
	files, _ := filepath.Glob(filepath.Join(e.cfg.StateDir, "logs", "*.jsonl"))
	if len(files) != 1 {
		t.Fatalf("log files = %v", files)
	}
	return files[0]
}

func TestLogListFiltersAndPages(t *testing.T) {
	e, c := logEnv(t)
	dir := filepath.Join(e.cfg.StateDir, "logs")
	l := auditlog.New(dir, auditlog.DefaultRetentionDays)
	for range 3 {
		if err := l.Append(auditlog.Fields{ClientUID: 7, Action: auditlog.OperationConfigApply, Result: auditlog.ResultSuccess}); err != nil {
			t.Fatal(err)
		}
	}

	all, err := c.LogEntries(control.LogListParams{})
	if err != nil || len(all.Entries) < 8 || all.Next != "" {
		t.Fatalf("all = %d entries, next %q, %v", len(all.Entries), all.Next, err)
	}
	for i := 1; i < len(all.Entries); i++ {
		if all.Entries[i].Seq >= all.Entries[i-1].Seq {
			t.Fatalf("not newest first: %+v", all.Entries)
		}
	}
	if top := all.Entries[0]; top.Action != "config.apply" || top.Result != "success" || top.ClientUID == nil || top.Time.IsZero() {
		t.Fatalf("entry = %+v", top)
	}
	if last := all.Entries[len(all.Entries)-1]; last.Seq != 1 || last.Signed {
		t.Fatalf("oldest entry = %+v", last)
	}

	var paged []uint64
	for after := ""; ; {
		page, err := c.LogEntries(control.LogListParams{Limit: 3, After: after})
		if err != nil || len(page.Entries) > 3 {
			t.Fatalf("page = %+v, %v", page, err)
		}
		for _, en := range page.Entries {
			paged = append(paged, en.Seq)
		}
		if after = page.Next; after == "" {
			break
		}
	}
	// The paging requests append entries of their own, which sit above the first page, so only the
	// oldest entries are compared.
	if len(paged) < len(all.Entries) || paged[len(paged)-1] != 1 {
		t.Fatalf("paged %v, listed %d", paged, len(all.Entries))
	}
	for i := 1; i < len(paged); i++ {
		if paged[i] >= paged[i-1] {
			t.Fatalf("pages overlap or are unordered: %v", paged)
		}
	}

	byAction, err := c.LogEntries(control.LogListParams{Action: "config.apply", Result: "success"})
	if err != nil || len(byAction.Entries) != 3 || byAction.Entries[0].Signed || byAction.Entries[0].ClientUID == nil || *byAction.Entries[0].ClientUID != 7 {
		t.Fatalf("by action = %+v, %v", byAction, err)
	}
	removed, err := c.LogEntries(control.LogListParams{Action: "deploy.run"})
	if err != nil || len(removed.Entries) != 0 {
		t.Fatalf("by removed action = %+v, %v", removed, err)
	}
	failed, err := c.LogEntries(control.LogListParams{Result: "failure"})
	if err != nil || len(failed.Entries) == 0 || failed.Entries[0].ErrorCode == "" {
		t.Fatalf("failed = %+v, %v", failed, err)
	}
	future, err := c.LogEntries(control.LogListParams{From: "2999-01-01T00:00:00Z"})
	if err != nil || len(future.Entries) != 0 {
		t.Fatalf("future = %+v, %v", future, err)
	}
	past, err := c.LogEntries(control.LogListParams{To: "2000-01-01T00:00:00Z"})
	if err != nil || len(past.Entries) != 0 {
		t.Fatalf("past = %+v, %v", past, err)
	}
	now, err := c.LogEntries(control.LogListParams{From: "2000-01-01T00:00:00Z", To: "2999-01-01T00:00:00Z", Limit: 500})
	if err != nil || len(now.Entries) < 8 {
		t.Fatalf("range = %d, %v", len(now.Entries), err)
	}
}

func TestLogListRejectsBadFilters(t *testing.T) {
	_, c := logEnv(t)
	for name, p := range map[string]control.LogListParams{
		"from format":    {From: "2030-01-01"},
		"to format":      {To: "yesterday"},
		"from after to":  {From: "2030-02-01T00:00:00Z", To: "2030-01-01T00:00:00Z"},
		"long action":    {Action: strings.Repeat("a", 65)},
		"action control": {Action: "a\nb"},
		"unknown result": {Result: "maybe"},
		"negative limit": {Limit: -1},
		"limit too big":  {Limit: control.LogMaxLimit + 1},
		"bad cursor":     {After: "../../etc/passwd/1"},
	} {
		if _, err := c.LogEntries(p); err == nil {
			t.Errorf("%s: accepted", name)
		} else {
			wantCode(t, err, control.CodeBadRequest)
		}
	}
	if err := c.Call(control.OpLogList, []int{1}, nil); err == nil {
		t.Error("array params accepted")
	}
}

func TestLogListAnswerStaysSmall(t *testing.T) {
	e, c := logEnv(t)
	l := auditlog.New(filepath.Join(e.cfg.StateDir, "logs"), auditlog.DefaultRetentionDays)
	for i := 0; i < control.LogMaxLimit+100; i++ {
		if err := l.Append(auditlog.Fields{ClientUID: 4000000000, Action: auditlog.OperationConfigApply,
			Result: auditlog.ResultFailure, ErrorCode: strings.Repeat("e", 64), Duration: auditlog.MaxDuration}); err != nil {
			t.Fatal(err)
		}
	}
	page, err := c.LogEntries(control.LogListParams{Limit: control.LogMaxLimit, Action: "config.apply"})
	if err != nil || len(page.Entries) != control.LogMaxLimit || page.Next == "" {
		t.Fatalf("page = %d entries, next %q, %v", len(page.Entries), page.Next, err)
	}
	rest, err := c.LogEntries(control.LogListParams{Limit: control.LogMaxLimit, Action: "config.apply", After: page.Next})
	if err != nil || len(rest.Entries) != 100 || rest.Next != "" {
		t.Fatalf("rest = %d entries, next %q, %v", len(rest.Entries), rest.Next, err)
	}
}

func TestLogVerifyIntactAndTampered(t *testing.T) {
	e, c := logEnv(t)
	got, err := c.VerifyLog()
	if err != nil || !got.Intact || len(got.Problems) != 0 || len(got.Days) != 1 ||
		got.Days[0].Signatures != "" || !got.Days[0].Intact || got.Days[0].Entries < 5 {
		t.Fatalf("intact = %+v, %v", got, err)
	}

	path := e.logFile(t)
	orig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(orig), "\n"), "\n")
	write := func(ls []string) {
		if err := os.WriteFile(path, []byte(strings.Join(ls, "\n")+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	// A changed line keeps its position but breaks the chain at the entry after it.
	changed := append([]string(nil), lines...)
	changed[2] = strings.Replace(changed[2], `"result":"success"`, `"result":"failure"`, 1)
	if changed[2] == lines[2] {
		t.Fatal("the line was not changed")
	}
	write(changed)
	got, err = c.VerifyLog()
	if err != nil || got.Intact || len(got.Problems) != 1 {
		t.Fatalf("changed = %+v, %v", got, err)
	}
	if p := got.Problems[0]; p.Code != control.LogProblemChain || p.Line != 4 || p.Seq != 4 || p.Date == "" {
		t.Fatalf("problem = %+v", p)
	}
	if d := got.Days[0]; d.Intact {
		t.Fatalf("day = %+v", d)
	}

	// A deleted line breaks the chain at the entry after it.
	write(append(append([]string(nil), lines[:1]...), lines[2:]...))
	got, err = c.VerifyLog()
	if err != nil || got.Intact || len(got.Problems) != 1 {
		t.Fatalf("deleted = %+v, %v", got, err)
	}
	if p := got.Problems[0]; p.Code != control.LogProblemChain || p.Line != 2 || p.Seq != 3 {
		t.Fatalf("problem = %+v", p)
	}

	// Garbage that is no entry is reported without its text.
	write(append(append([]string(nil), lines...), "not json /var/lib/secret"))
	got, err = c.VerifyLog()
	if err != nil || got.Intact || got.Problems[0].Code != control.LogProblemUnparsable || got.Problems[0].Line != len(lines)+1 {
		t.Fatalf("garbage = %+v, %v", got, err)
	}
	// The verify request above was logged after the garbage line, which documents that line, so the day is
	// intact again and counts the damage.
	got, err = c.VerifyLog()
	if err != nil || !got.Intact || len(got.Problems) != 0 || !got.Days[0].Intact || got.Days[0].Damaged != 1 {
		t.Fatalf("documented garbage = %+v, %v", got, err)
	}
}

func TestLogContinuesAfterATornLastLine(t *testing.T) {
	e, c := logEnv(t)
	path := e.logFile(t)
	read := func() []string {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	}
	torn := strings.Join(read(), "\n") + "\n" + `{"seq":99,"time":"2026-`
	if err := os.WriteFile(path, []byte(torn), 0o600); err != nil {
		t.Fatal(err)
	}
	before := len(read())

	if _, err := c.Status(); err != nil {
		t.Fatal(err)
	}
	after := read()
	if len(after) != before+2 || !strings.Contains(after[before], `"kind":"damaged-line"`) ||
		!strings.Contains(after[before+1], `"action":"control.status"`) {
		t.Fatalf("lines after the torn one = %q, want a marker and the logged request", after[before-1:])
	}
	got, err := c.VerifyLog()
	if err != nil || !got.Intact || len(got.Problems) != 0 || len(got.Days) != 1 || !got.Days[0].Intact ||
		got.Days[0].Damaged != 1 {
		t.Fatalf("verify = %+v, %v, want an intact day with one documented damage", got, err)
	}
}

// Entries an earlier version signed stay in the chain; their signatures are reported as not checked.
func TestLogVerifyReportsOldSignaturesAsNotChecked(t *testing.T) {
	e, c := logEnv(t)
	path := e.logFile(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	last := lines[len(lines)-1]
	var prev struct {
		Seq  uint64 `json:"seq"`
		Time string `json:"time"`
	}
	if err := json.Unmarshal([]byte(last), &prev); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(last))
	old := fmt.Sprintf(`{"seq":%d,"time":%q,"client_uid":1000,"action":"config.read","result":"success","prev_hash":%q,"mac":%q}`,
		prev.Seq+1, prev.Time, hex.EncodeToString(sum[:]), strings.Repeat("ab", 32))
	if err := os.WriteFile(path, append(data, []byte(old+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}

	// The next request is logged after the signed entry and chains from it without a break.
	got, err := c.VerifyLog()
	if err != nil || !got.Intact || len(got.Problems) != 0 || len(got.Days) != 1 ||
		got.Days[0].Signatures != control.LogSignaturesNotChecked {
		t.Fatalf("verify = %+v, %v, want an intact chain with signatures not checked", got, err)
	}
	list, err := c.LogEntries(control.LogListParams{Action: "config.read"})
	signed := 0
	for _, en := range list.Entries {
		if en.Signed {
			signed++
		}
	}
	if err != nil || signed != 1 {
		t.Fatalf("list = %+v, %v, want exactly the old entry marked signed", list, err)
	}
}

func TestLogVerifyEmptyLog(t *testing.T) {
	e := newEnv(t)
	e.start(t)
	got, err := e.dial(t).VerifyLog()
	if err != nil || !got.Intact || got.Days == nil {
		t.Fatalf("empty = %+v, %v", got, err)
	}
}

// An entry of the removed deployment area, with the project field it carried, stays listed and keeps the
// chain intact; the project is not shown and the removed action is still a valid filter.
func TestLogListShowsOldDeploymentEntry(t *testing.T) {
	e, c := logEnv(t)
	path := e.logFile(t)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	last := lines[len(lines)-1]
	var prev struct {
		Seq  uint64 `json:"seq"`
		Time string `json:"time"`
	}
	if err := json.Unmarshal([]byte(last), &prev); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(last))
	old := fmt.Sprintf(`{"seq":%d,"time":%q,"client_uid":1000,"action":"deploy.run","project":"demo-app","result":"success","prev_hash":%q,"mac":""}`,
		prev.Seq+1, prev.Time, hex.EncodeToString(sum[:]))
	if err := os.WriteFile(path, append(data, []byte(old+"\n")...), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := c.LogEntries(control.LogListParams{Action: "deploy.run"})
	if err != nil || len(got.Entries) != 1 || got.Entries[0].Seq != prev.Seq+1 {
		t.Fatalf("old entry = %+v, %v", got, err)
	}
	raw, err := json.Marshal(got)
	if err != nil || strings.Contains(string(raw), "project") || strings.Contains(string(raw), "demo-app") {
		t.Fatalf("answer exposes a project: %s, %v", raw, err)
	}
	if v, err := c.VerifyLog(); err != nil || !v.Intact {
		t.Fatalf("verify = %+v, %v", v, err)
	}
}
