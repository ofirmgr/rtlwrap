package termstate

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Har2yQn78/rtlwrap/internal/vt10x"
)

// Badge byte sequences: blue "he", red "en".
const (
	heBadge = "\x1b[0;34mhe\x1b[0m"
	enBadge = "\x1b[0;31men\x1b[0m"
)

func TestLanguageBadgeRestoresTextAndCursor(t *testing.T) {
	var out bytes.Buffer
	e := New(&out, 20, 4)
	screen := vt10x.New(vt10x.WithSize(20, 4))
	e.SetLanguage("he")
	_, _ = e.Write([]byte("underlying text\r\nabc"))
	_, _ = screen.Write(out.Bytes())
	if screen.Cell(3, 0).Char != 'h' || screen.Cell(4, 0).Char != 'e' {
		t.Fatal("Hebrew label not above caret")
	}
	if cur := screen.Cursor(); cur.X != 3 || cur.Y != 1 {
		t.Fatalf("label moved caret: %+v", cur)
	}
	out.Reset()
	e.SetLanguage("en")
	_, _ = e.Write([]byte("\x1b[3;6H"))
	_, _ = screen.Write(out.Bytes())
	if screen.Cell(3, 0).Char != 'e' || screen.Cell(4, 0).Char != 'r' {
		t.Fatal("underlying text not restored")
	}
	if screen.Cell(5, 1).Char != 'e' || screen.Cell(6, 1).Char != 'n' {
		t.Fatal("English label not above new caret")
	}
	out.Reset()
	if err := e.ClearLanguageBadge(); err != nil {
		t.Fatal(err)
	}
	_, _ = screen.Write(out.Bytes())
	if screen.Cell(5, 1).Char != ' ' {
		t.Fatal("label remains after cleanup")
	}
	if cur := screen.Cursor(); cur.X != 5 || cur.Y != 2 {
		t.Fatalf("cleanup moved caret: %+v", cur)
	}
}

func TestLanguageBadgeUsesMixedTextVisualCaret(t *testing.T) {
	var out bytes.Buffer
	e := New(&out, 30, 4)
	e.SetLanguage("he")
	_, _ = e.Write([]byte("\x1b[2;1HPermit מגרש 123 approved\x1b[2;9H"))
	screen := vt10x.New(vt10x.WithSize(30, 4))
	_, _ = screen.Write(out.Bytes())
	cur := screen.Cursor()
	if screen.Cell(min(cur.X, 28), cur.Y-1).Char != 'h' {
		t.Fatal("badge does not follow mapped visual caret")
	}
	if cur.X == 8 {
		t.Fatal("test did not exercise reordered caret")
	}
}

func TestLanguageBadgeFollowsDrawnCaret(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		reordered   bool
	}{
		{"ltr", "\x1b[?25l\x1b[2;1H> abcd\x1b[7m \x1b[0m\x1b[4;1H", false},
		{"rtl", "\x1b[?25l\x1b[2;1H> שלום\x1b[7m \x1b[0m\x1b[4;1H", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			e := New(&out, 30, 4)
			e.SetLanguage("en")
			_, _ = e.Write([]byte("above row text that is long\r\n"))
			_, _ = e.Write([]byte(tc.input))
			screen := vt10x.New(vt10x.WithSize(30, 4))
			_, _ = screen.Write(out.Bytes())
			caretX := -1
			for x := 0; x < 30; x++ {
				if screen.Cell(x, 1).Mode&attrReverse != 0 {
					caretX = x
				}
			}
			if caretX < 0 {
				t.Fatal("drawn caret not rendered")
			}
			if (caretX != 6) != tc.reordered {
				t.Fatalf("drawn caret at column %d, reordered=%v", caretX, tc.reordered)
			}
			x := min(caretX, 28)
			if screen.Cell(x, 0).Char != 'e' || screen.Cell(x+1, 0).Char != 'n' {
				t.Fatalf("label not above drawn caret at column %d", caretX)
			}
			if e.Caret() != nil {
				t.Fatal("drawn caret fed arrow navigation")
			}
		})
	}
}

func TestLanguageBadgeIgnoresAmbiguousDrawnCaret(t *testing.T) {
	var out bytes.Buffer
	e := New(&out, 20, 4)
	e.SetLanguage("he")
	_, _ = e.Write([]byte("text\r\n\x1b[?25l\x1b[7m menu \x1b[0m> \x1b[7m \x1b[0m"))
	if strings.Contains(out.String(), heBadge) {
		t.Fatal("label rendered for several reverse-video cells")
	}
}

func TestLanguageBadgeHiddenAtUnsafePositions(t *testing.T) {
	for _, tc := range []struct {
		name, input, language string
		cols                  int
	}{
		{"top", "hello", "he", 20},
		{"hidden", "\x1b[2;3H\x1b[?25l", "he", 20},
		{"unsupported", "\x1b[2;3H", "fr", 20},
		{"narrow", "\x1b[2;1H", "en", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			e := New(&out, tc.cols, 4)
			e.SetLanguage(tc.language)
			_, _ = e.Write([]byte(tc.input))
			if strings.Contains(out.String(), heBadge) || strings.Contains(out.String(), enBadge) {
				t.Fatal("unsafe label rendered")
			}
		})
	}
	var out bytes.Buffer
	e := NewInline(&out, 20, 4, 2)
	e.SetLanguage("he")
	_, _ = e.Write([]byte("prompt"))
	if strings.Contains(out.String(), heBadge) {
		t.Fatal("label overwrote unknown shell row")
	}
}

func TestLanguageBadgeDoesNotEnterScrollbackOrCopyObserver(t *testing.T) {
	var out bytes.Buffer
	e := NewInline(&out, 12, 3, 0)
	var observed strings.Builder
	e.SetRowObserver(func(logical, visual []rune, _ []int) {
		observed.WriteString(string(logical))
		observed.WriteString(string(visual))
	})
	e.SetLanguage("he")
	screen := vt10x.New(vt10x.WithSize(12, 3))
	var scrolled strings.Builder
	screen.SetScrollCallback(func(lines [][]vt10x.Glyph) {
		for _, line := range lines {
			for _, cell := range line {
				scrolled.WriteRune(cell.Char)
			}
		}
	})
	_, _ = e.Write([]byte("original\r\ninput"))
	_, _ = screen.Write(out.Bytes())
	out.Reset()
	_, _ = e.Write([]byte("\r\nnext\r\nlast"))
	_, _ = screen.Write(out.Bytes())
	if strings.Contains(scrolled.String()+observed.String(), "he") {
		t.Fatal("display label leaked into scrollback or copy observer")
	}
	if !strings.Contains(scrolled.String(), "original") {
		t.Fatal("underlying scrollback text lost")
	}
}

func TestLanguageResizeDoesNotRestoreStaleRow(t *testing.T) {
	var out bytes.Buffer
	e := NewInline(&out, 20, 4, 0)
	e.SetLanguage("en")
	_, _ = e.Write([]byte("long underlying row\r\nprompt"))
	if e.badgeRow == 0 {
		t.Fatal("test requires a visible badge")
	}
	out.Reset()
	e.Resize(10, 4)
	if out.Len() != 0 {
		t.Fatal("resize wrote stale pre-reflow text")
	}
	if err := e.ClearLanguageBadge(); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Fatal("cleanup used stale coordinates")
	}
	_, _ = e.Write([]byte("\x1b[2J\x1b[Hfresh\r\nprompt"))
	if !strings.Contains(out.String(), enBadge) {
		t.Fatal("label did not return after child repaint")
	}
}
