package wrap

import (
	"bytes"
	"io"
	"testing"
	"time"
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
		done <- forwardInput(&out, r, func() bool { return true }, 20*time.Millisecond)
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
		done <- forwardInput(&out, r, func() bool { return true }, 20*time.Millisecond)
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
		done <- forwardInput(&out, r, func() bool { return true }, 50*time.Millisecond)
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

func TestMakeShouldSwap(t *testing.T) {
	// Options take precedence
	if fn := makeShouldSwap(Options{SwapArrows: true}); !fn() {
		t.Errorf("expected SwapArrows: true to return true")
	}
	if fn := makeShouldSwap(Options{DisableSwapArrows: true, SwapArrows: true}); fn() {
		t.Errorf("expected DisableSwapArrows: true to take precedence")
	}

	// Env vars
	t.Setenv("RTLWRAP_SWAP_ARROWS", "1")
	if fn := makeShouldSwap(Options{}); !fn() {
		t.Errorf("expected RTLWRAP_SWAP_ARROWS to return true")
	}
	t.Setenv("RTLWRAP_NO_SWAP_ARROWS", "1")
	if fn := makeShouldSwap(Options{}); fn() {
		t.Errorf("expected RTLWRAP_NO_SWAP_ARROWS to take precedence")
	}
}

