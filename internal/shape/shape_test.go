package shape

import (
	"fmt"
	"strings"
	"testing"

	"github.com/benoitkugler/textprocessing/fribidi"
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

// Warp mirrors paired punctuation according to the bidi levels it resolves
// from the already-reordered text. Simulate that final rendering step so the
// compensation is checked for both LTR and RTL bracket contents.
func TestWarpCompensationMatchesStandardVisual(t *testing.T) {
	cases := []string{
		"דיוק מילים (WER): כמה מילים",
		"לפני שם מותרות רק אותיות שימוש (כמו ל', ב', ו').",
		"מילים לא ודאיות (uncertain): מותר להחליף",
		"[בדיקה] {ערך} (טקסט)",
		"1. tests/test_ground_truth_schema.ts בודק טקסט (בעברית)",
		"prefix עברית (English) המשך",
		"prefix עברית (טקסט) suffix",
		"operator הוא אופיר (שם), reviewedBy נדרש",
	}
	for _, logical := range cases {
		t.Setenv("TERM_PROGRAM", "")
		want := Shape(logical)
		t.Setenv("TERM_PROGRAM", "WarpTerminal")
		got := simulateWarpMirroring(Shape(logical))
		if got != want {
			t.Errorf("Warp display mismatch for %q:\n got %q\nwant %q", logical, got, want)
		}
	}
}

func simulateWarpMirroring(visual string) string {
	runes := []rune(visual)
	types := make([]fribidi.CharType, len(runes))
	brackets := make([]fribidi.BracketType, len(runes))
	for i, r := range runes {
		types[i] = fribidi.GetBidiType(r)
		brackets[i] = fribidi.GetBracket(r)
	}
	base := fribidi.ParType(fribidi.ON)
	levels, _ := fribidi.GetParEmbeddingLevels(types, brackets, &base)
	fribidi.Shape(fribidi.ShapeMirroring, levels, nil, runes)
	return string(runes)
}

// Each insertion boundary maps to the cell where the next character is drawn:
// left of the preceding RTL character, right of the preceding LTR one.
func TestCaretsPointAtNextCell(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []int
	}{
		{"שלום", []int{3, 2, 1, 0, -1}},
		{"abc", []int{0, 1, 2, 3}},
	} {
		_, _, _, got := ShapeRunesLayout([]rune(tc.in))
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("%q: carets = %v, want %v", tc.in, got, tc.want)
		}
	}
}
