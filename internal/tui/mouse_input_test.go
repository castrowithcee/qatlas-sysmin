package tui

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
)

// chunkReader hands out its chunks one per Read, the way a terminal behind SSH or tmux delivers a short read,
// then reports the end of the input. It waits gap before each of the first paused chunks after the first.
type chunkReader struct {
	mu     sync.Mutex
	chunks [][]byte
	gap    time.Duration
	paused int
	reads  int
}

// readerPause is a gap between two reads longer than the input reader waits for the rest of a sequence; the
// reader then reports what it has, the way it meets a report a slow connection split. splitPause is barely
// longer: should the reader still wait, it gets the report whole, which makes no text either.
const (
	readerPause = 2 * uv.DefaultEscTimeout
	splitPause  = uv.DefaultEscTimeout + 10*time.Millisecond
)

// testClock is the clock of the filter in a test; a pause in the input moves it on.
type testClock struct{ offset atomic.Int64 }

func (c *testClock) now() time.Time { return time.Now().Add(time.Duration(c.offset.Load())) }

// pause is a key that is a gap in the input instead: when the event loop meets it, the real timers run out and
// the filter's clock moves on by a second. Handled there, not by the reader, which runs ahead of the loop.
const pause = "\x1b[23~"

func (r *chunkReader) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.chunks) == 0 {
		return 0, io.EOF
	}
	if r.reads > 0 && r.reads < r.paused {
		time.Sleep(r.gap)
	}
	r.reads++
	n := copy(p, r.chunks[0])
	if n < len(r.chunks[0]) {
		r.chunks[0] = r.chunks[0][n:]
	} else {
		r.chunks = r.chunks[1:]
	}
	return n, nil
}

// inputEnd is a key no screen uses; the filter turns it into the end of the program, so every key before it has
// been handled when Run returns.
const inputEnd = "\x1b[24~"

// runInput feeds the chunks to a real program around model, readerPause apart, and returns once all of them
// were handled. The production filter runs first; keys are the messages that reached the model.
func runInput(t *testing.T, model *Model, chunks ...string) (keys []string) {
	t.Helper()
	return runPaused(t, model, readerPause, chunks...)
}

// runPaused is runInput with the chunks gap apart.
func runPaused(t *testing.T, model *Model, gap time.Duration, chunks ...string) (keys []string) {
	t.Helper()
	clock := &testClock{}
	reader := &chunkReader{gap: gap, paused: len(chunks)}
	for _, c := range chunks {
		reader.chunks = append(reader.chunks, []byte(c))
	}
	reader.chunks = append(reader.chunks, []byte(inputEnd))
	fragments := newFragmentFilter(clock.now)
	var seen []string
	observe := func(m tea.Model, msg tea.Msg) tea.Msg {
		if key, ok := msg.(tea.KeyPressMsg); ok && key.Code == tea.KeyF11 {
			clock.offset.Add(int64(time.Second))
			time.Sleep(2 * bracketHold)
			return nil
		}
		msg = fragments.filter(m, msg)
		if key, ok := msg.(tea.KeyPressMsg); ok && key.Code == tea.KeyF12 {
			return tea.QuitMsg{}
		}
		switch msg := msg.(type) {
		case tea.MouseMsg:
			seen = append(seen, "mouse")
		case tea.KeyPressMsg:
			seen = append(seen, msg.String())
		case keysMsg:
			for _, key := range msg {
				seen = append(seen, key.String())
			}
		}
		return msg
	}
	p := tea.NewProgram(model, tea.WithInput(reader), tea.WithOutput(io.Discard), tea.WithFilter(observe),
		tea.WithoutSignals())
	fragments.send = p.Send
	done := make(chan error, 1)
	go func() { _, err := p.Run(); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("program: %v", err)
		}
	case <-time.After(10 * time.Second):
		p.Kill()
		t.Fatal("the program did not end")
	}
	return seen
}

// splitReports are mouse reports as a terminal sends them: wheel and left click, press and release.
// The motion and release reports move nothing, so the focus stays on the field for the whole stream.
var splitReports = []string{"\x1b[<65;12;5M", "\x1b[<64;3;40M", "\x1b[<0;70;9M\x1b[<0;70;9m", "\x1b[<35;120;40M", "\x1b[<0;7;19m"}

// cutPoints are the places a report is cut at in a test: after its escape byte, inside its head, in the middle and
// before its last byte; every byte would take far longer for the same cases.
func cutPoints(report string) []int {
	return []int{1, 2, 3, len(report) / 2, len(report) - 1}
}

func filterWithText(t *testing.T) *Model {
	t.Helper()
	m := frameModel(t, 120, 40)
	press(t, m, "/")
	typeText(t, m, "abc")
	if !m.typing() || m.list.input.Value() != "abc" {
		t.Fatalf("the filter does not read text: typing %v, value %q", m.typing(), m.list.input.Value())
	}
	return m
}

func fieldValue(m *Model) string { return m.list.input.Value() }

// A report split between two reads, with a pause after which the input reader gives up waiting for its rest,
// makes no text at any point of the split. A report cut right after its escape byte leaves a lone escape,
// which is a real key and clears the filter; no character of the report may remain in it.
func TestSplitMouseReportsMakeNoTextInAField(t *testing.T) {
	for _, report := range splitReports {
		for _, i := range append([]int{0, len(report)}, cutPoints(report)...) {
			t.Run(fmt.Sprintf("%q/%d", report, i), func(t *testing.T) {
				m := filterWithText(t)
				keys := runPaused(t, m, splitPause, report[:i], report[i:])
				if got := fieldValue(m); got != "abc" && got != "" {
					t.Errorf("field %q after keys %q", got, keys)
				}
			})
		}
	}
}

// A report cut at any byte, the cut after its escape byte included, reaches the model as a mouse event when the
// pieces follow each other within the input reader's escape wait: the reader joins them, and no key and no text
// comes of it. A machine that stalls the reader for longer than that wait splits the report for real, so a run
// is repeated twice before it counts as failed; a missing join fails every run.
func TestMouseReportsSplitWithinTheEscapeWaitStayMouseEvents(t *testing.T) {
	const quick = 5 * time.Millisecond
	for _, report := range splitReports {
		stream := report + report
		want := 2 * strings.Count(report, "\x1b")
		for _, i := range cutPoints(report) {
			t.Run(fmt.Sprintf("%q/%d", report, i), func(t *testing.T) {
				var keys []string
				for try := 0; try < 3; try++ {
					m := filterWithText(t)
					keys = runPaused(t, m, quick, stream[:i], stream[i:])
					if fieldValue(m) != "abc" {
						t.Fatalf("field changed after keys %q", keys)
					}
					if len(keys) == want && strings.Count(strings.Join(keys, ","), "mouse") == want {
						return
					}
				}
				t.Errorf("keys %q, want %d mouse events", keys, want)
			})
		}
	}
}

// An escape without anything after it is a key: it clears the filter.
func TestALoneEscapeStillLeavesTheFilter(t *testing.T) {
	m := filterWithText(t)
	if keys := runInput(t, m, "\x1b", pause); len(keys) != 1 || keys[0] != "esc" || m.list.editing || fieldValue(m) != "" {
		t.Errorf("keys %q, editing %v, field %q, want esc and a cleared filter", keys, m.list.editing, fieldValue(m))
	}
}

func TestTypedCharactersOfAReportReachTheField(t *testing.T) {
	const paste = "\x1b[200~[<65;12;5M\x1b[201~"
	for _, c := range []struct {
		name   string
		chunks []string
		want   string
	}{
		{"typed in one read", []string{"[<65;12;5M"}, "abc[<65;12;5M"},
		{"typed one by one", []string{"[", "<", "6", "5", ";", "M"}, "abc[<65;M"},
		{"after a pause", []string{"\x1b[", pause, "<65;12;5M"}, "abc[<65;12;5M"},
		{"alt+[ on its own", []string{"\x1b[", pause, "x"}, "abc[x"},
		{"alt+[ and a letter", []string{"\x1b[", "x"}, "abc[x"},
		{"alt+[ twice", []string{"\x1b[", "\x1b[", pause, "x"}, "abc[[x"},
		{"digits after a report", []string{"\x1b[<35;12;5M", "12;5M"}, "abc12;5M"},
		{"paste of a report", []string{paste}, "abc[<65;12;5M"},
		{"paste after a piece", []string{"\x1b[", "<65", paste}, "abc[<65;12;5M"},
		{"text after a report", []string{"\x1b[", "<65;12;5Mxy"}, "abcxy"},
		{"text after a pause", []string{"\x1b[", "<65;1", pause, "2;5M"}, "abc2;5M"},
	} {
		// The model is made here, where the test owns the global render state, so the runs can overlap: each
		// only waits for the input reader's timers.
		m := filterWithText(t)
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			runInput(t, m, append(c.chunks, pause)...)
			if got := fieldValue(m); got != c.want {
				t.Errorf("field = %q, want %q", got, c.want)
			}
		})
	}
}

// A paste reaches the focused text field whole, through the filter as through the event loop alone, and no
// key binding acts on what it holds.
func TestAPasteReachesTheFocusedField(t *testing.T) {
	m := filterWithText(t)
	runInput(t, m, "\x1b[200~ q enter\x1b[201~")
	if got := fieldValue(m); got != "abc q enter" || m.quitting || !m.list.editing {
		t.Errorf("after the paste the field holds %q, editing %v, quitting %v", got, m.list.editing, m.quitting)
	}
}

// heldFilter is a filter on a clock the test moves, which holds a "[" without a program: the release is sent
// by hand, so the order of the timer and the rest of the report is the test's choice.
func heldFilter() (*fragmentFilter, *time.Time) {
	now := time.Unix(1000, 0)
	f := newFragmentFilter(func() time.Time { return now })
	f.send = func(tea.Msg) {}
	return f, &now
}

// keyTexts lists what a filter result delivers as keys, in order.
func keyTexts(msg tea.Msg) []string {
	switch msg := msg.(type) {
	case nil:
		return nil
	case tea.KeyPressMsg:
		return []string{msg.String()}
	case keysMsg:
		var out []string
		for _, key := range msg {
			out = append(out, key.String())
		}
		return out
	}
	return []string{fmt.Sprintf("%T", msg)}
}

// The rest of a report that arrives while the "[" is still held, even late in the window, makes no key at all.
func TestHeldBracketSwallowsTheRestWithinTheWindow(t *testing.T) {
	f, now := heldFilter()
	if got := f.filter(nil, tea.KeyPressMsg{Code: '[', Mod: tea.ModAlt}); got != nil {
		t.Fatalf("alt+[ passed at once: %v", keyTexts(got))
	}
	*now = now.Add(bracketHold - time.Millisecond)
	if got := f.filter(nil, textKey("<65;12;5M")); got != nil {
		t.Errorf("the rest made keys %v", keyTexts(got))
	}
	if got := f.filter(nil, heldKeyMsg{f.id}); got != nil {
		t.Errorf("the release made keys %v", keyTexts(got))
	}
}

// A head that comes after the hold ended is text, whether or not the window would still take it.
func TestHeadAfterTheReleaseIsText(t *testing.T) {
	f, now := heldFilter()
	f.filter(nil, tea.KeyPressMsg{Code: tea.KeyEscape})
	f.filter(nil, textKey("["))
	*now = now.Add(bracketHold)
	if got := keyTexts(f.filter(nil, heldKeyMsg{f.id})); len(got) != 1 || got[0] != "[" {
		t.Fatalf("release: %v", got)
	}
	*now = now.Add(time.Millisecond)
	if got := keyTexts(f.filter(nil, textKey("<65;12;5M"))); len(got) != 1 || got[0] != "<65;12;5M" {
		t.Errorf("head after the release: %v", got)
	}
}

// A head after the window is text too, and the held "[" goes first.
func TestHeadAfterTheWindowIsText(t *testing.T) {
	f, now := heldFilter()
	f.filter(nil, tea.KeyPressMsg{Code: '[', Mod: tea.ModAlt})
	*now = now.Add(fragmentWindow + time.Millisecond)
	got := keyTexts(f.filter(nil, textKey("<65;12;5M")))
	if len(got) != 2 || got[0] != "alt+[" {
		t.Errorf("keys %v, want alt+[ then the text", got)
	}
}

// An alt+[ typed alone reaches the model once the hold ends.
func TestLoneAltBracketArrivesAfterTheHold(t *testing.T) {
	f, _ := heldFilter()
	f.filter(nil, tea.KeyPressMsg{Code: '[', Mod: tea.ModAlt})
	if got := keyTexts(f.filter(nil, heldKeyMsg{f.id})); len(got) != 1 || got[0] != "alt+[" {
		t.Errorf("keys %v, want alt+[", got)
	}
}
