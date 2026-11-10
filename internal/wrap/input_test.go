package wrap

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/Har2yQn78/rtlwrap/internal/termstate"
)

func TestTransformInput(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		swap     bool
		expected string
	}{
		{"swap disabled leaves left arrow unchanged", "\x1b[D", false, "\x1b[D"},
		{"swap disabled leaves right arrow unchanged", "\x1b[C", false, "\x1b[C"},
		{"basic left to right", "\x1b[D", true, "\x1b[C"},
		{"basic right to left", "\x1b[C", true, "\x1b[D"},
		{"app mode left to right", "\x1bOD", true, "\x1bOC"},
		{"app mode right to left", "\x1bOC", true, "\x1bOD"},
		{"shift left to shift right", "\x1b[1;2D", true, "\x1b[1;2C"},
		{"shift right to shift left", "\x1b[1;2C", true, "\x1b[1;2D"},
		{"option left to option right", "\x1b[1;3D", true, "\x1b[1;3C"},
		{"option right to option left", "\x1b[1;3C", true, "\x1b[1;3D"},
		{"ctrl left to ctrl right", "\x1b[1;5D", true, "\x1b[1;5C"},
		{"ctrl right to ctrl left", "\x1b[1;5C", true, "\x1b[1;5D"},
		{"meta b to meta f", "\x1bb", true, "\x1bf"},
		{"meta f to meta b", "\x1bf", true, "\x1bb"},
		{"up arrow untouched", "\x1b[A", true, "\x1b[A"},
		{"down arrow untouched", "\x1b[B", true, "\x1b[B"},
		{"hebrew text untouched", "שלום עולם", true, "שלום עולם"},
		{"english text untouched", "hello world", true, "hello world"},
		{"multiple arrows in one chunk", "\x1b[D\x1b[C\x1b[D", true, "\x1b[C\x1b[D\x1b[C"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := string(transformInput([]byte(tc.input), tc.swap))
			if got != tc.expected {
				t.Fatalf("transformInput(%q, %v) = %q; want %q", tc.input, tc.swap, got, tc.expected)
			}
		})
	}
}

func TestIncompleteEscapeLen(t *testing.T) {
	tests := []struct {
		name     string
		input    []byte
		expected int
	}{
		{"empty", []byte{}, 0},
		{"plain text", []byte("hello"), 0},
		{"complete CSI arrow", []byte("\x1b[D"), 0},
		{"complete 2-byte meta b", []byte("\x1bb"), 0},
		{"trailing ESC", []byte("abc\x1b"), 1},
		{"trailing CSI prefix", []byte("abc\x1b["), 2},
		{"trailing CSI with param", []byte("abc\x1b[1;3"), 5},
		{"trailing SS3 prefix", []byte("abc\x1bO"), 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := incompleteEscapeLen(tc.input)
			if got != tc.expected {
				t.Fatalf("incompleteEscapeLen(%q) = %d; want %d", tc.input, got, tc.expected)
			}
		})
	}
}

func TestForwardInputSwapped(t *testing.T) {
	r, w := io.Pipe()
	var out bytes.Buffer

	done := make(chan error, 1)
	go func() {
		done <- forwardInput(&out, r, swapAll, 20*time.Millisecond)
	}()

	// Send Left Arrow, Right Arrow, and Hebrew text
	if _, err := w.Write([]byte("\x1b[D")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("\x1b[C")); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("שלום")); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	want := "\x1b[C\x1b[Dשלום"
	if out.String() != want {
		t.Fatalf("forwardInput got %q; want %q", out.String(), want)
	}
}

func TestForwardInputStandaloneEscape(t *testing.T) {
	r, w := io.Pipe()
	var out bytes.Buffer

	done := make(chan error, 1)
	go func() {
		done <- forwardInput(&out, r, swapAll, 20*time.Millisecond)
	}()

	// Send standalone ESC
	if _, err := w.Write([]byte{0x1b}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond) // Wait for timeout flush
	if _, err := w.Write([]byte("a")); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	want := "\x1ba"
	if out.String() != want {
		t.Fatalf("forwardInput got %q; want %q", out.String(), want)
	}
}

func TestForwardInputSplitEscape(t *testing.T) {
	r, w := io.Pipe()
	var out bytes.Buffer

	done := make(chan error, 1)
	go func() {
		done <- forwardInput(&out, r, swapAll, 50*time.Millisecond)
	}()

	// Send \x1b[ in first write, D in second write immediately
	if _, err := w.Write([]byte("\x1b[")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if _, err := w.Write([]byte("D")); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	// \x1b[D should be combined and transformed to \x1b[C
	want := "\x1b[C"
	if out.String() != want {
		t.Fatalf("forwardInput split escape got %q; want %q", out.String(), want)
	}
}

func swapAll(b []byte) []byte { return transformInput(b, true) }

func TestMakeTranslator(t *testing.T) {
	t.Setenv("RTLWRAP_SWAP_ARROWS", "")
	t.Setenv("RTLWRAP_NO_SWAP_ARROWS", "")
	ltr := &termstate.Caret{Map: []int{0, 1, 2}, X: 1}
	rtl := &termstate.Caret{Map: []int{2, 1, 0}, X: 1}
	for _, tc := range []struct {
		name     string
		options  Options
		caret    *termstate.Caret
		language string
		in, want string
	}{
		{"unknown caret, Hebrew layout", Options{}, nil, "he", "\x1b[D", "\x1b[C"},
		{"unknown caret, English layout", Options{}, nil, "en", "\x1b[D", "\x1b[D"},
		{"RTL caret ignores layout", Options{}, rtl, "en", "\x1b[D", "\x1b[C"},
		{"LTR caret ignores layout", Options{}, ltr, "he", "\x1b[D", "\x1b[D"},
		{"modified arrow swaps by caret", Options{}, rtl, "en", "\x1b[1;3D", "\x1b[1;3C"},
		{"word jump swaps by caret", Options{}, rtl, "en", "\x1bb", "\x1bf"},
		{"forced", Options{SwapArrows: true}, ltr, "en", "\x1b[D", "\x1b[C"},
		{"disabled", Options{DisableSwapArrows: true}, rtl, "he", "\x1b[D", "\x1b[D"},
		{"disabled wins", Options{DisableSwapArrows: true, SwapArrows: true}, rtl, "he", "\x1b[D", "\x1b[D"},
		{"other input first", Options{}, rtl, "en", "a\x1b[D", "a\x1b[C"},
	} {
		translate := makeTranslator(tc.options, func() *termstate.Caret { return tc.caret }, func() string { return tc.language })
		if got := string(translate([]byte(tc.in))); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}

	t.Setenv("RTLWRAP_SWAP_ARROWS", "1")
	if got := string(makeTranslator(Options{}, func() *termstate.Caret { return ltr }, nil)([]byte("\x1b[D"))); got != "\x1b[C" {
		t.Errorf("RTLWRAP_SWAP_ARROWS: got %q", got)
	}
	t.Setenv("RTLWRAP_NO_SWAP_ARROWS", "1")
	if got := string(makeTranslator(Options{}, func() *termstate.Caret { return rtl }, nil)([]byte("\x1b[D"))); got != "\x1b[D" {
		t.Errorf("RTLWRAP_NO_SWAP_ARROWS: got %q", got)
	}
}

// Before the child's response renders, a repeated arrow continues from the
// position the previous one was sent to instead of the stale rendered caret.
func TestTranslatorPredictsUnrenderedMoves(t *testing.T) {
	t.Setenv("RTLWRAP_SWAP_ARROWS", "")
	t.Setenv("RTLWRAP_NO_SWAP_ARROWS", "")
	// "ab" then an RTL run: boundary visual columns 0 1 4 3 2.
	caret := &termstate.Caret{Map: []int{0, 1, 4, 3, 2}, X: 1}
	translate := makeTranslator(Options{}, func() *termstate.Caret { return caret }, func() string { return "en" })
	for _, want := range []string{"\x1b[C\x1b[C\x1b[C", "\x1b[D"} {
		if got := string(translate([]byte("\x1b[C"))); got != want {
			t.Errorf("got %q, want %q", got, want)
		}
	}
}

// Drive the renderer to every caret position, translate a physical arrow, and
// apply the resulting logical moves: the caret must land on the visually
// nearest boundary in the arrow's direction, whatever the layout and however
// Hebrew and English are mixed.
func TestArrowNavigationVisualDirection(t *testing.T) {
	t.Setenv("RTLWRAP_SWAP_ARROWS", "")
	t.Setenv("RTLWRAP_NO_SWAP_ARROWS", "")
	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	for _, text := range []string{
		"שלום",
		"hello",
		"שלום hello",
		"hello שלום",
		"שלום hello world עולם",
		"hello שלום עולם world",
		"❯ שלום hello עולם", // Claude Code prompt
	} {
		for _, language := range []string{"he", "en"} {
			t.Run(language+"/"+text, func(t *testing.T) {
				runes := []rune(text)
				for x := 0; x <= len(runes); x++ {
					e := termstate.NewInline(io.Discard, 60, 2, 0)
					e.SetRTL(true)
					if _, err := e.Write([]byte(text + strings.Repeat("\b", len(runes)-x))); err != nil {
						t.Fatal(err)
					}
					caret := e.Caret()
					for key, dx := range map[byte]int{'D': -1, 'C': 1} {
						translate := makeTranslator(Options{}, e.Caret, func() string { return language })
						out := string(translate([]byte{0x1b, '[', key}))
						n, ok := caret.Step(dx)
						if !ok {
							if out != "" {
								t.Errorf("caret %d key %c: at visual edge, want empty suppressed output, got %q", x, key, out)
							}
							continue
						}
						got := x + strings.Count(out, "\x1b[C") - strings.Count(out, "\x1b[D")
						if got != x+n {
							t.Errorf("caret %d key %c: moved to %d, want %d (visual %d -> %d)",
								x, key, got, x+n, caret.Map[x], caret.Map[x+n])
						}
						if (caret.Map[x+n]-caret.Map[x])*dx <= 0 {
							t.Errorf("caret %d key %c: visual %d -> %d, wrong direction", x, key, caret.Map[x], caret.Map[x+n])
						}
						for j, v := range caret.Map {
							if d := (v - caret.Map[x]) * dx; d > 0 && d < (caret.Map[x+n]-caret.Map[x])*dx {
								t.Errorf("caret %d key %c: skipped boundary %d at visual %d", x, key, j, v)
							}
						}
					}
				}
			})
		}
	}
}

func TestCaretStepNearest(t *testing.T) {
	// "ab" then an RTL run: boundary visual columns 0 1 4 3 2.
	c := &termstate.Caret{Map: []int{0, 1, 4, 3, 2}, X: 1}
	for _, tc := range []struct{ x, dx, want int }{
		{1, 1, 3}, {4, 1, -1}, {3, 1, -1}, {2, -1, 1}, {4, -1, -3}, {1, -1, -1},
	} {
		c.X = tc.x
		if n, ok := c.Step(tc.dx); !ok || n != tc.want {
			t.Errorf("x %d dx %d: step %d %v, want %d", tc.x, tc.dx, n, ok, tc.want)
		}
	}
	c.X = 2
	if _, ok := c.Step(1); ok {
		t.Error("right edge: expected no step")
	}
}
