package wrap

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Har2yQn78/rtlwrap/internal/copytext"
	"github.com/Har2yQn78/rtlwrap/internal/shape"
)

func TestDispatchDefersCursorUntilStreamingSettles(t *testing.T) {
	var output bytes.Buffer
	d := newInlineDispatcher(&output, 40, 3, 0)
	d.cursorDelay = time.Hour

	if _, err := d.Write([]byte("מילים")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\x1b[?25h") {
		t.Fatalf("cursor shown inside streaming repaint: %q", output.String())
	}
	staleEpoch := d.cursorEpoch

	output.Reset()
	if _, err := d.Write([]byte(" נוספות")); err != nil {
		t.Fatal(err)
	}
	d.showCursor(staleEpoch)
	if strings.Contains(output.String(), "\x1b[?25h") {
		t.Fatalf("stale cursor timer exposed an intermediate position: %q", output.String())
	}
	d.showCursor(d.cursorEpoch)
	if !strings.Contains(output.String(), "\x1b[?25h") {
		t.Fatalf("cursor not shown after stream settled: %q", output.String())
	}

	output.Reset()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "\x1b[?25h") {
		t.Fatalf("cursor not restored at close: %q", output.String())
	}
}

// The clipboard cache must see the same text the renderer paints, including
// normal scrollback and a temporary alternate screen, without retaining ANSI.
func TestCopyRestorationAcrossScreens(t *testing.T) {
	var output bytes.Buffer
	d := newInlineDispatcher(&output, 40, 3, 0)
	d.cursorDelay = time.Hour
	defer func() { _ = d.Close() }()
	copies := copytext.New(64)
	d.observeRows(copies.Add)
	for _, chunk := range []string{
		"\x1b[31mשלום עולם\x1b[0m\r\n",
		"line two\r\nline three\r\nline four\r\n",
		"\x1b[?1049h\x1b[Hstatus: מחיר 123 USD",
		"\x1b[?1049l",
	} {
		if _, err := d.Write([]byte(chunk)); err != nil {
			t.Fatal(err)
		}
	}
	for _, original := range []string{"שלום עולם", "status: מחיר 123 USD"} {
		visual := shape.Shape(original)
		if !strings.Contains(output.String(), visual) {
			t.Fatalf("fixture was not emitted: %q", visual)
		}
		if got, ok := copies.Restore(visual); !ok || got != original {
			t.Fatalf("Restore(%q) = %q, %v; want %q", visual, got, ok, original)
		}
	}
	if _, ok := copies.Restore("unrelated clipboard text"); ok {
		t.Fatal("unrelated clipboard text must remain unchanged")
	}
}

// dispatch routes normal output to the pipe and alt-screen output to the
// termstate engine, passing the alt toggles through to switch real buffers.
func TestDispatchRoutesAltScreen(t *testing.T) {
	var buf bytes.Buffer
	d := newDispatcher(&buf, 20, 5)
	_, _ = d.Write([]byte("normal رفت\n"))      // pipe path (scrolling)
	_, _ = d.Write([]byte("\x1b[?1049h"))       // enter alt → passthrough
	_, _ = d.Write([]byte("\x1b[H\x1b[2Jسلام")) // engine path (grid redraw)
	_, _ = d.Write([]byte("\x1b[?1049l"))       // exit alt → passthrough
	_ = d.Close()

	out := buf.String()
	if !strings.Contains(out, "\x1b[?1049h") || !strings.Contains(out, "\x1b[?1049l") {
		t.Fatalf("alt toggles not passed through: %q", out)
	}
	if !strings.Contains(out, shape.Shape("رفت")) {
		t.Errorf("pipe did not shape scrolling RTL: %q", out)
	}
	if !strings.Contains(out, shape.Shape("سلام")) {
		t.Errorf("engine did not reshape alt-screen RTL: %q", out)
	}
}

// A toggle split across two writes must still be recognized (tokenizer holds
// the incomplete escape).
func TestDispatchToggleSplitAcrossWrites(t *testing.T) {
	var buf bytes.Buffer
	d := newDispatcher(&buf, 20, 5)
	_, _ = d.Write([]byte("\x1b[?10"))
	_, _ = d.Write([]byte("49h\x1b[H\x1b[2Jسلام"))
	_ = d.Close()
	if !strings.Contains(buf.String(), shape.Shape("سلام")) {
		t.Errorf("split toggle broke alt detection: %q", buf.String())
	}
}

// Codex brackets redraws with synchronized-output markers. A redraw can span
// PTY reads and temporarily contain only part of an RTL row; the real terminal
// must not display those intermediate layouts.
func TestDispatchPreservesSynchronizedRedraw(t *testing.T) {
	for _, alt := range []bool{false, true} {
		t.Run(fmt.Sprintf("alt=%v", alt), func(t *testing.T) {
			var out bytes.Buffer
			d := newInlineDispatcher(&out, 40, 3, 0)
			d.cursorDelay = time.Hour
			defer func() { _ = d.Close() }()
			if alt {
				_, _ = d.Write([]byte("\x1b[?1049h"))
			}
			out.Reset()
			for _, chunk := range []string{
				"\x1b[?20", "26h",
				"\r\x1b[2K› שלום",
				" עולם 123 test",
				"\x1b[?202", "6l",
			} {
				if _, err := d.Write([]byte(chunk)); err != nil {
					t.Fatal(err)
				}
			}
			got := out.String()
			begin, end := strings.Index(got, "\x1b[?2026h"), strings.Index(got, "\x1b[?2026l")
			firstPaint, lastPaint := strings.Index(got, "\x1b[2K"), strings.LastIndex(got, "\x1b[2K")
			if begin < 0 || end < 0 || firstPaint <= begin || lastPaint >= end {
				t.Fatalf("redraws escaped synchronized-output boundary: begin=%d end=%d firstPaint=%d lastPaint=%d", begin, end, firstPaint, lastPaint)
			}
			if !strings.Contains(got[begin:end], shape.Shape("› שלום עולם 123 test")) {
				t.Fatal("completed mixed Hebrew/English row missing from synchronized frame")
			}
		})
	}
}

func TestAltToggle(t *testing.T) {
	cases := []struct {
		in        string
		enter, ok bool
	}{
		{"\x1b[?1049h", true, true},
		{"\x1b[?1049l", false, true},
		{"\x1b[?47h", true, true},
		{"\x1b[?25h", false, false}, // cursor show, not alt-screen
		{"\x1b[0m", false, false},   // SGR
	}
	for _, c := range cases {
		enter, ok := altToggle([]byte(c.in))
		if enter != c.enter || ok != c.ok {
			t.Errorf("altToggle(%q) = (%v,%v), want (%v,%v)", c.in, enter, ok, c.enter, c.ok)
		}
	}
}

// Sequences the grid renderer cannot reproduce must reach the terminal
// untouched; cell-affecting ones must not be forwarded behind its back.
func TestPassthroughClassification(t *testing.T) {
	pass := []string{
		"\x1b]0;title\x07",   // OSC title
		"\x1b]52;c;Zm9v\x07", // OSC 52 clipboard
		"\x1b[?1000h",        // mouse reporting
		"\x1b[?1006l",
		"\x1b[?2004h", // bracketed paste
		"\x1b[?1004h", // focus reporting
		"\x1b[5 q",    // DECSCUSR cursor shape
	}
	for _, s := range pass {
		if !passthrough([]byte(s)) {
			t.Errorf("passthrough(%q) = false, want true", s)
		}
	}
	block := []string{
		"\x1b[2J",     // clear screen
		"\x1b[H",      // cursor home
		"\x1b[?25l",   // cursor visibility: the engine drives this itself
		"\x1b[?1049h", // alt screen: handled by altToggle
		"\x1b[31m",    // color
		"\x1b[3q",     // not DECSCUSR (no intermediate space)
	}
	for _, s := range block {
		if passthrough([]byte(s)) {
			t.Errorf("passthrough(%q) = true, want false", s)
		}
	}
}

// A child clearing the screen must reach every row, including the ones the
// inline engine leaves alone because the shell, not the child, wrote them.
func TestClearsScreen(t *testing.T) {
	for _, s := range []string{"\x1b[2J", "\x1b[3J", "\x1b[?2J", "\x1bc"} {
		if !clearsScreen([]byte(s)) {
			t.Errorf("clearsScreen(%q) = false, want true", s)
		}
	}
	for _, s := range []string{"\x1b[J", "\x1b[0J", "\x1b[1J", "\x1b[2K", "\x1b[H"} {
		if clearsScreen([]byte(s)) {
			t.Errorf("clearsScreen(%q) = true, want false", s)
		}
	}
}
