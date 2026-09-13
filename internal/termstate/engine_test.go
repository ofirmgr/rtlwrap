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
	// Logical insertion boundaries for a four-character RTL word.
	v2l := []int{4, 3, 2, 1, 0}
	for logical, wantVis := range map[int]int{0: 4, 3: 1, 2: 2, 4: 0} {
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

// A row's blank padding is not text: reordering it with the row's RTL run
// would scatter the blanks through the line. The run is moved to the right
// edge with a cursor-forward move over the cleared row instead, which is where
// an RTL paragraph belongs.
func TestRTLRowRightAligned(t *testing.T) {
	var buf bytes.Buffer
	e := NewInline(&buf, 20, 2, 0)
	if _, err := e.Write([]byte("שלום")); err != nil {
		t.Fatal(err)
	}
	want := "\x1b[1;1H\x1b[2K\x1b[16C\x1b[0m" + shape.Shape("שלום")
	if !strings.Contains(buf.String(), want) {
		t.Errorf("RTL row not right-aligned\n got %q\nwant substr %q", buf.String(), want)
	}
}

// An LTR row keeps column 1: only an RTL paragraph is right-aligned.
func TestLTRRowNotAligned(t *testing.T) {
	var buf bytes.Buffer
	e := NewInline(&buf, 20, 2, 0)
	if _, err := e.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\x1b[1;1H\x1b[2K\x1b[0mhello") {
		t.Errorf("LTR row not painted at column 1: %q", buf.String())
	}
}

// The cursor has to follow the line it sits on: after a right-aligned RTL row
// it lands at the left edge of the word, after its final logical character.
func TestRTLCursorFollowsAlignment(t *testing.T) {
	var buf bytes.Buffer
	e := NewInline(&buf, 20, 2, 0)
	if _, err := e.Write([]byte("שלום")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "\x1b[1;17H") {
		t.Errorf("cursor not moved to the aligned row end\ngot %q", buf.String())
	}
}

// Insertion positions must follow Hebrew typing, including spaces and edits,
// rather than jumping to the blank cells after the logical text.
func TestHebrewInsertionCaret(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		want        int
	}{
		{"one letter", "ש", 19},
		{"word", "שלום", 16},
		{"space", "שלום ", 15},
		{"second word", "שלום ע", 14},
		{"inside word", "שלום\x1b[D", 17},
		{"word start", "שלום\r", 19},
		{"LTR prompt", "a> שלום", 3},
		{"LTR control", "> hello", 7},
		{"digits", "a> שלום 12", 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			e := NewInline(&out, 20, 3, 0)
			if _, err := e.Write([]byte(tc.input)); err != nil {
				t.Fatal(err)
			}
			want := fmt.Sprintf("\x1b[1;%dH\x1b[?25h", tc.want+1)
			if !strings.HasSuffix(out.String(), want) {
				t.Fatalf("caret suffix want %q, got %q", want, out.String())
			}
		})
	}
}

// Separate PTY writes must retain the same insertion affinity on every key.
func TestHebrewCaretEveryKeystroke(t *testing.T) {
	for _, warp := range []string{"", "WarpTerminal"} {
		t.Run(warp, func(t *testing.T) {
			t.Setenv("TERM_PROGRAM", warp)
			var out bytes.Buffer
			e := NewInline(&out, 30, 3, 0)
			for i, r := range []rune("שלום עולם") {
				out.Reset()
				if _, err := e.Write([]byte(string(r))); err != nil {
					t.Fatal(err)
				}
				want := fmt.Sprintf("\x1b[1;%dH\x1b[?25h", 30-i)
				if !strings.HasSuffix(out.String(), want) {
					t.Fatalf("key %d: want %q, got %q", i, want, out.String())
				}
				out.Reset()
				if _, err := e.Write(nil); err != nil {
					t.Fatal(err)
				}
				if !strings.HasSuffix(out.String(), want) {
					t.Fatalf("idle repaint after key %d moved caret", i)
				}
			}
		})
	}
}

// Codex animates Braille dots in blank cells, including the one between its
// prompt and the typed text. A dot there must not flip the row to LTR and move
// the caret to the other side of the screen for one animation frame.
func TestBrailleAnimationKeepsHebrewCaret(t *testing.T) {
	var out bytes.Buffer
	e := NewInline(&out, 30, 3, 0)
	if _, err := e.Write([]byte("› שלום")); err != nil {
		t.Fatal(err)
	}
	const want = "\x1b[1;25H\x1b[?25h"
	if !strings.HasSuffix(out.String(), want) {
		t.Fatalf("baseline caret want %q, got %q", want, out.String())
	}
	for _, frame := range []string{"\x1b[1;2H⠁\x1b[1;7H", "\x1b[1;25H⠄\x1b[1;7H", "\x1b[1;2H \x1b[1;7H"} {
		out.Reset()
		if _, err := e.Write([]byte(frame)); err != nil {
			t.Fatal(err)
		}
		if !strings.HasSuffix(out.String(), want) {
			t.Fatalf("frame %q moved caret: %q", frame, out.String())
		}
	}
	out.Reset()
	if _, err := e.Write([]byte("\x1b[1;2H⠂\x1b[1;7H")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "⠂") {
		t.Fatalf("Braille glyph not emitted: %q", out.String())
	}
}
