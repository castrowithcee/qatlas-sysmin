package auditlog

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type countingReaderAt struct {
	data []byte
	read int64
}

func (c *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	n := copy(p, c.data[off:])
	c.read += int64(n)
	return n, nil
}

func TestLastLineFromMatchesSplitLines(t *testing.T) {
	long := strings.Repeat("x", 50)
	cases := map[string]string{
		"empty":            "",
		"only newlines":    "\n\n\n",
		"single no nl":     "abc",
		"single nl":        "abc\n",
		"two no final nl":  "abc\ndef",
		"trailing blanks":  "abc\ndef\n\n\n",
		"whitespace line":  "abc\n  \n",
		"long last line":   "abc\n" + long + "\n",
		"long only line":   long + long,
		"blank then long":  "a\n\n" + long + "\n\n",
		"newlines at edge": strings.Repeat("\n", 20) + "abc",
	}
	for name, content := range cases {
		for _, bs := range []int64{1, 3, 7, 16, 4096} {
			var want []byte
			if lines := splitLines([]byte(content)); len(lines) > 0 {
				want = lines[len(lines)-1]
			}
			got, err := lastLineFrom(&countingReaderAt{data: []byte(content)}, int64(len(content)), bs)
			if err != nil {
				t.Fatalf("%s/bs=%d: error = %v", name, bs, err)
			}
			if !bytes.Equal(got, want) || (got == nil) != (want == nil) {
				t.Fatalf("%s/bs=%d: got %q, want %q", name, bs, got, want)
			}
		}
	}
}

func TestLastLineOfFiles(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), fileMode); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if raw, err := lastLineOf(filepath.Join(dir, "missing")); err != nil || raw != nil {
		t.Fatalf("missing: raw=%q err=%v", raw, err)
	}
	if raw, err := lastLineOf(write("empty", "")); err != nil || raw != nil {
		t.Fatalf("empty: raw=%q err=%v", raw, err)
	}
	if raw, err := lastLineOf(write("nofinal", "{\"seq\":1}\n{\"seq\":2}")); err != nil || string(raw) != "{\"seq\":2}" {
		t.Fatalf("no final newline: raw=%q err=%v", raw, err)
	}
	// A damaged last line is returned as it is; deciding what it means is up to the caller.
	if raw, err := lastLineOf(write("bad", "{\"seq\":1}\n{\"seq\":2")); err != nil || string(raw) != "{\"seq\":2" {
		t.Fatalf("damaged last line: raw=%q err=%v", raw, err)
	}
	big := "{\"seq\":3,\"project\":\"" + strings.Repeat("y", 3*lastLineBlockSize) + "\"}"
	if raw, err := lastLineOf(write("big", "{\"seq\":1}\n"+big+"\n")); err != nil || string(raw) != big {
		t.Fatalf("line larger than a block: err=%v", err)
	}
}

// TestLastLineSkipsADamagedLastLine shows the chain continues from the last line that parses, before a
// damaged last line, and across files, while only the very last line is reported as damaged.
func TestLastLineSkipsADamagedLastLine(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), fileMode); err != nil {
			t.Fatal(err)
		}
	}
	write("2026-03-01.jsonl", "{\"seq\":1}\n{\"seq\":2}\n")
	write("2026-03-02.jsonl", "{\"seq\":3}\nnot json\n{\"seq\":4")
	files, err := dayFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	end, err := lastLine(dir, files)
	if err != nil || end.entry.Seq != 3 || string(end.raw) != "{\"seq\":3}" || string(end.damaged) != "{\"seq\":4" ||
		!end.unterminated || filepath.Base(end.damagedPath) != "2026-03-02.jsonl" {
		t.Fatalf("end = %+v, %v", end, err)
	}

	write("2026-03-02.jsonl", "torn\n")
	end, err = lastLine(dir, files)
	if err != nil || end.entry.Seq != 2 || string(end.damaged) != "torn" || end.unterminated {
		t.Fatalf("damaged only line: end = %+v, %v", end, err)
	}

	write("2026-03-02.jsonl", "{\"seq\":3}\n")
	end, err = lastLine(dir, files)
	if err != nil || end.entry.Seq != 3 || end.damaged != nil {
		t.Fatalf("intact: end = %+v, %v", end, err)
	}
}

// TestLastLineReadBytesIndependentOfSize shows the bytes read for the predecessor stay flat as the file grows.
func TestLastLineReadBytesIndependentOfSize(t *testing.T) {
	line := strings.Repeat("z", 200) + "\n"
	var reads []int64
	for _, n := range []int{100, 10000} {
		data := []byte(strings.Repeat(line, n))
		c := &countingReaderAt{data: data}
		if _, err := lastLineFrom(c, int64(len(data)), lastLineBlockSize); err != nil {
			t.Fatal(err)
		}
		reads = append(reads, c.read)
	}
	if reads[0] != reads[1] || reads[1] > 2*lastLineBlockSize {
		t.Fatalf("bytes read = %v, want equal and bounded", reads)
	}
}
