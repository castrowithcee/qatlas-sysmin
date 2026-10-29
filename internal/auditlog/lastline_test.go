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
	if _, raw, err := lastLineOf(filepath.Join(dir, "missing")); err != nil || raw != nil {
		t.Fatalf("missing: raw=%q err=%v", raw, err)
	}
	if _, raw, err := lastLineOf(write("empty", "")); err != nil || raw != nil {
		t.Fatalf("empty: raw=%q err=%v", raw, err)
	}
	if e, raw, err := lastLineOf(write("nofinal", "{\"seq\":1}\n{\"seq\":2}")); err != nil || raw == nil || e.Seq != 2 {
		t.Fatalf("no final newline: e=%+v raw=%q err=%v", e, raw, err)
	}
	if _, _, err := lastLineOf(write("bad", "{\"seq\":1}\n{\"seq\":2")); err == nil ||
		!strings.Contains(err.Error(), "not a valid log entry") {
		t.Fatalf("damaged last line: err = %v", err)
	}
	big := "{\"seq\":3,\"project\":\"" + strings.Repeat("y", 3*lastLineBlockSize) + "\"}"
	if e, raw, err := lastLineOf(write("big", "{\"seq\":1}\n"+big+"\n")); err != nil || e.Seq != 3 || string(raw) != big {
		t.Fatalf("line larger than a block: seq=%d err=%v", e.Seq, err)
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
