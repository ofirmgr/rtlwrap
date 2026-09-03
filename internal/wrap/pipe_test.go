package wrap

import (
	"bytes"
	"testing"

	"github.com/Har2yQn78/rtlwrap/internal/shape"
)

// feed writes in through a pipe in the given chunk sizes and returns output.
func feed(in string, chunk int) string {
	var buf bytes.Buffer
	p := newPipe(&buf)
	b := []byte(in)
	if chunk <= 0 {
		_, _ = p.Write(b)
	} else {
		for i := 0; i < len(b); i += chunk {
			end := i + chunk
			if end > len(b) {
				end = len(b)
			}
			_, _ = p.Write(b[i:end])
		}
	}
	_ = p.Close()
	return buf.String()
}

func TestPipePassesAnsiThrough(t *testing.T) {
	in := "\x1b[31mhello\x1b[0m\n"
	if got := feed(in, 0); got != in { // pure LTR: bytes unchanged
		t.Errorf("ANSI/LTR mangled:\n got %q\nwant %q", got, in)
	}
}

func TestPipeShapesText(t *testing.T) {
	in := "سلام\n"
	want := shape.Shape("سلام") + "\n" // newline is CONTROL, passed through
	if got := feed(in, 0); got != want {
		t.Errorf("shape mismatch:\n got %q\nwant %q", got, want)
	}
}

// RTL text split across writes must still shape as one line (not per-fragment).
func TestPipeRTLAcrossChunks(t *testing.T) {
	in := "سلام دنیا\n"
	want := feed(in, 0)
	for _, ch := range []int{1, 2, 3} {
		if got := feed(in, ch); got != want {
			t.Errorf("chunk=%d: RTL split shaped differently:\n got %q\nwant %q", ch, got, want)
		}
	}
}

// Color reset around a whole RTL line is the common case and must work.
func TestPipeColoredRTLLine(t *testing.T) {
	in := "\x1b[1mسلام\x1b[0m\n"
	want := "\x1b[1m" + shape.Shape("سلام") + "\x1b[0m\n"
	if got := feed(in, 0); got != want {
		t.Errorf("colored RTL line:\n got %q\nwant %q", got, want)
	}
}

// The cursor report tells the inline engine which row the child starts on.
// Keystrokes that arrive while it is in flight belong to the child.
func TestParseCPR(t *testing.T) {
	row, rest, ok := parseCPR([]byte("\x1b[12;5R"))
	if !ok || row != 11 || len(rest) != 0 {
		t.Errorf("parseCPR = %d, %q, %v; want 11, \"\", true", row, rest, ok)
	}
	row, rest, ok = parseCPR([]byte("ab\x1b[1;1Rcd"))
	if !ok || row != 0 || string(rest) != "abcd" {
		t.Errorf("parseCPR = %d, %q, %v; want 0, \"abcd\", true", row, rest, ok)
	}
	if _, _, ok := parseCPR([]byte("\x1b[12;")); ok {
		t.Error("parseCPR accepted a partial report")
	}
}
