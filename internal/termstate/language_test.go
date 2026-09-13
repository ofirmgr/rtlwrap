package termstate

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Har2yQn78/rtlwrap/internal/vt10x"
)

func TestLanguageBadgeRestoresTextAndCursor(t *testing.T) {
	var out bytes.Buffer
	e := New(&out, 20, 4)
	screen := vt10x.New(vt10x.WithSize(20, 4))
	e.SetLanguage("he")
	_, _ = e.Write([]byte("underlying text\r\nabc"))
	_, _ = screen.Write(out.Bytes())
	if screen.Cell(3, 0).Char != 'ʰ' || screen.Cell(4, 0).Char != 'ᵉ' {
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
	if screen.Cell(5, 1).Char != 'ᵉ' || screen.Cell(6, 1).Char != 'ⁿ' {
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
	if screen.Cell(min(cur.X, 28), cur.Y-1).Char != 'ʰ' {
		t.Fatal("badge does not follow mapped visual caret")
	}
	if cur.X == 8 {
		t.Fatal("test did not exercise reordered caret")
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
			if strings.ContainsAny(out.String(), "ʰᵉⁿ") {
				t.Fatal("unsafe label rendered")
			}
		})
	}
	var out bytes.Buffer
	e := NewInline(&out, 20, 4, 2)
	e.SetLanguage("he")
	_, _ = e.Write([]byte("prompt"))
	if strings.Contains(out.String(), "ʰᵉ") {
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
	if strings.ContainsAny(scrolled.String()+observed.String(), "ʰᵉⁿ") {
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
	if !strings.Contains(out.String(), "ᵉⁿ") {
		t.Fatal("label did not return after child repaint")
	}
}
