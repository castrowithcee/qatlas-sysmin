package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManpageCoversCommandsAndTopics(t *testing.T) {
	withVersion(t, "v1.2.3")
	dir := t.TempDir()
	if err := WriteManpage(dir, time.Date(2026, time.March, 5, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "qsysmin.1"))
	if err != nil {
		t.Fatal(err)
	}
	page := string(b)
	for _, want := range []string{
		`.TH "QSYSMIN" "1" "March 2026" "qsysmin v1.2.3"`, ".SH NAME", ".SH HELP TOPICS",
		`.SS "qsysmin update"`, `.SS "qsysmin uninstall"`, `.SS "qsysmin host apply"`, ".SS host\\-admin",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %q", want)
		}
	}
	for _, topic := range helpTopics {
		if !strings.Contains(page, ".SS "+strings.ReplaceAll(topic.Name, "-", `\-`)) {
			t.Errorf("page lacks help topic %s", topic.Name)
		}
	}
	for _, line := range strings.Split(page, "\n") {
		if strings.HasPrefix(line, "'") || strings.Contains(line, "\x00") {
			t.Errorf("suspicious roff line %q", line)
		}
	}
}

func TestRoffTextNeutralisesControlCharacters(t *testing.T) {
	tests := map[string]string{
		".SH INJECTED":  `\&.SH INJECTED`,
		"'cmd":          `\&'cmd`,
		`back\slash`:    `back\eslash`,
		"a-b":           `a\-b`,
		"plain text":    "plain text",
		`.TH \fBx\fP-1`: `\&.TH \efBx\efP\-1`,
	}
	for in, want := range tests {
		if got := roffText(in); got != want {
			t.Errorf("roffText(%q) = %q, want %q", in, got, want)
		}
	}
	if got := quoteArg(`a"b`); got != `"a\(dqb"` {
		t.Errorf("quoteArg = %q", got)
	}
}

func TestManpageIsReproducible(t *testing.T) {
	date := time.Unix(1700000000, 0)
	var pages [2]string
	for i := range pages {
		dir := t.TempDir()
		if err := WriteManpage(dir, date); err != nil {
			t.Fatal(err)
		}
		b, _ := os.ReadFile(filepath.Join(dir, "qsysmin.1"))
		pages[i] = string(b)
	}
	if pages[0] != pages[1] {
		t.Error("two runs wrote different pages")
	}
}
