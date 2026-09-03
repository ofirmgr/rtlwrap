package termstate

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/Har2yQn78/rtlwrap/internal/shape"
)

// A cursor-positioned redraw (home + clear + Persian) must land the reshaped
// presentation forms in the output — the case the Phase 2 pipe can't handle.
func TestEngineReshapesRedraw(t *testing.T) {
	var buf bytes.Buffer
	e := New(&buf, 20, 3)
	if _, err := e.Write([]byte("\x1b[H\x1b[2Jسلام")); err != nil {
		t.Fatal(err)
	}
	want := shape.Shape("سلام") // "ﻡﻼﺳ"
	if !strings.Contains(buf.String(), want) {
		t.Errorf("reshaped RTL missing\n got %q\nwant substr %q", buf.String(), want)
	}
}

// A whole-run color must survive reshaping: the color moves onto the reordered
// runes via the visualToLogical map, staying attached to the same letters.
func TestEngineColorRemap(t *testing.T) {
	var buf bytes.Buffer
	e := New(&buf, 20, 3)
	if _, err := e.Write([]byte("\x1b[H\x1b[2J\x1b[31mسلام\x1b[0m")); err != nil {
		t.Fatal(err)
	}
	want := "\x1b[0;31m" + shape.Shape("سلام") // red SGR then the reshaped run
	if !strings.Contains(buf.String(), want) {
		t.Errorf("color not remapped onto reshaped run\n got %q\nwant substr %q", buf.String(), want)
	}
}

// Pure-LTR content passes through unreordered.
func TestEngineLTRPassthrough(t *testing.T) {
	var buf bytes.Buffer
	e := New(&buf, 20, 3)
	if _, err := e.Write([]byte("\x1b[H\x1b[2Jhello")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Errorf("LTR text mangled: %q", buf.String())
	}
}

// An unchanged frame re-renders no row bodies (only cursor repositioning).
func TestEngineDiffSkipsUnchanged(t *testing.T) {
	var buf bytes.Buffer
	e := New(&buf, 20, 3)
	_, _ = e.Write([]byte("\x1b[H\x1b[2Jhi"))
	buf.Reset()
	_, _ = e.Write([]byte("")) // no state change
	if strings.Contains(buf.String(), "hi") {
		t.Errorf("unchanged row was re-emitted: %q", buf.String())
	}
}

func TestVisualX(t *testing.T) {
	// visual rune 0 came from logical 3, rune 1 from logical 2, ...
	v2l := []int{3, 2, 1, 0}
	for logical, wantVis := range map[int]int{0: 3, 3: 0, 2: 1} {
		if got := visualX(v2l, logical, 20); got != wantVis {
			t.Errorf("visualX(logical=%d) = %d, want %d", logical, got, wantVis)
		}
	}
	if got := visualX(v2l, 99, 20); got != 19 { // past end clamps to cols-1
		t.Errorf("visualX past-end = %d, want 19", got)
	}
}

// An inline engine must not touch rows above the child's starting line: the
// shell's own output lives there.
func TestInlineLeavesRowsAboveStart(t *testing.T) {
	var buf bytes.Buffer
	e := NewInline(&buf, 20, 5, 2) // child starts on row 3 (0-based 2)
	if _, err := e.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, row := range []string{"\x1b[1;1H", "\x1b[2;1H"} {
		if strings.Contains(out, row) {
			t.Errorf("repainted a row above the start row: %q in %q", row, out)
		}
	}
	if !strings.Contains(out, "\x1b[3;1H") {
		t.Errorf("start row not painted\ngot %q", out)
	}
}

// Lines that scroll off the top must leave via a real scroll, so the terminal
// keeps them in its scrollback instead of having them painted over.
func TestInlineScrollEmitsNewlines(t *testing.T) {
	var buf bytes.Buffer
	e := NewInline(&buf, 20, 3, 0)
	for i := 0; i < 5; i++ {
		if _, err := e.Write([]byte("line\r\n")); err != nil {
			t.Fatal(err)
		}
	}
	out := buf.String()
	// The first two lines fill rows 1-2; each of the last three scrolls once.
	if n := strings.Count(out, "\x1b[3;1H\n"); n != 3 {
		t.Errorf("want 3 one-row scrolls at the bottom row, got %d\nout %q", n, out)
	}
}

// A burst that scrolls more rows than the screen holds must still put every
// line through the top row: those lines exist nowhere else afterwards.
func TestInlineBurstKeepsEveryLine(t *testing.T) {
	var buf bytes.Buffer
	e := NewInline(&buf, 20, 3, 0)
	var in strings.Builder
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&in, "line%d\r\n", i)
	}
	if _, err := e.Write([]byte(in.String())); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for i := 0; i < 10; i++ {
		if !strings.Contains(out, fmt.Sprintf("line%d", i)) {
			t.Errorf("line%d lost from the scrollback\nout %q", i, out)
		}
	}
}

// Hebrew typed one character at a time is the reported symptom: each keystroke
// arrives as its own fragment, so only a grid-backed reshape of the whole row
// can put the run in visual order.
func TestInlineIncrementalRTLTyping(t *testing.T) {
	var buf bytes.Buffer
	e := NewInline(&buf, 20, 3, 0)
	for _, r := range "שלום" {
		if _, err := e.Write([]byte(string(r))); err != nil {
			t.Fatal(err)
		}
	}
	want := shape.Shape("שלום")
	if !strings.Contains(buf.String(), want) {
		t.Errorf("row not reshaped after per-character writes\n got %q\nwant substr %q", buf.String(), want)
	}
}

// A row's blank padding is not text: reordering it with the row's RTL run would
// push the line to the right edge of the screen.
func TestRowPaddingNotReordered(t *testing.T) {
	var buf bytes.Buffer
	e := NewInline(&buf, 20, 2, 0)
	if _, err := e.Write([]byte("שלום")); err != nil {
		t.Fatal(err)
	}
	want := "\x1b[1;1H\x1b[2K\x1b[0m" + shape.Shape("שלום")
	if !strings.Contains(buf.String(), want) {
		t.Errorf("RTL row not painted at column 1\n got %q\nwant substr %q", buf.String(), want)
	}
}
