package control

import (
	"bufio"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestReadLine(t *testing.T) {
	br := func(s string) *bufio.Reader { return bufio.NewReaderSize(strings.NewReader(s), 16) }
	long := strings.Repeat("a", 100)

	if got, err := ReadLine(br("hello\nrest"), 10); err != nil || string(got) != "hello" {
		t.Errorf("got %q, %v", got, err)
	}
	if got, err := ReadLine(br(long+"\n"), 100); err != nil || len(got) != 100 {
		t.Errorf("exact limit: %d, %v", len(got), err)
	}
	if _, err := ReadLine(br(long+"\n"), 99); !errors.Is(err, ErrLineTooLong) {
		t.Errorf("one over: %v", err)
	}
	if _, err := ReadLine(br(long), 50); !errors.Is(err, ErrLineTooLong) {
		t.Errorf("no newline over: %v", err)
	}
	if _, err := ReadLine(br("abc"), 50); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Errorf("partial: %v", err)
	}
	if _, err := ReadLine(br(""), 50); !errors.Is(err, io.EOF) {
		t.Errorf("empty: %v", err)
	}
}
