package shape

import (
	"strings"
	"testing"
)

// Golden cases captured from fribidi and eyeball-verified in Phase 0.
// Shape strips fribidi's zero-width U+FEFF lam-alef fillers. Digits and LTR
// runs stay in place; only the RTL runs are reordered and shaped to
// presentation forms.
var golden = []struct {
	name, in, want string
}{
	{"ascii passthrough", "hello world", "hello world"},
	{"empty", "", ""},
	{"pure persian", "سلام", "ﻡﻼﺳ"},
	{"persian words", "سلام دنیا", "ﺎﯿﻧﺩ ﻡﻼﺳ"},
	{"persian with digits", "قیمت 100 تومان", "ﻥﺎﻣﻮﺗ 100 ﺖﻤﯿﻗ"},
	{"mixed ltr run", "کد: git commit", "git commit :ﺪﮐ"},
	{"emoji and zwnj", "می‌روم 👍", "👍 ﻡﻭﺭ‌ﯽﻣ"},
}

func TestShapeGolden(t *testing.T) {
	for _, c := range golden {
		if got := Shape(c.in); got != c.want {
			t.Errorf("%s: Shape(%q)\n got %q\nwant %q", c.name, c.in, got, c.want)
		}
	}
}

// Hard Unicode cases must survive shaping, not get dropped or mangled.
func TestShapePreservesNonArabic(t *testing.T) {
	for _, r := range []string{"👍", "👨‍👩‍👧", "é"} { // emoji, ZWJ family, combining acute
		if out := Shape("x " + r); !strings.Contains(out, r) {
			t.Errorf("Shape dropped/altered %q: got %q", r, out)
		}
	}
}

// Per-line: base direction is detected independently, newlines preserved.
func TestShapeMultiline(t *testing.T) {
	out := Shape("hello\nسلام\nworld")
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %q", len(lines), out)
	}
	if lines[0] != "hello" || lines[2] != "world" {
		t.Errorf("LTR lines changed: %q", out)
	}
}

func TestShapeCompensatesForWarpPunctuationMirroring(t *testing.T) {
	logical := "דיוק מילים (WER): כמה מילים"
	t.Setenv("TERM_PROGRAM", "")
	standard := "םילימ המכ :(WER) םילימ קויד"
	if got := Shape(logical); got != standard {
		t.Errorf("standard mirrored punctuation:\n got %q\nwant %q", got, standard)
	}

	t.Setenv("TERM_PROGRAM", "WarpTerminal")
	want := "םילימ המכ :)WER( םילימ קויד"
	if got := Shape(logical); got != want {
		t.Errorf("Warp mirrored punctuation compensation:\n got %q\nwant %q", got, want)
	}
}
