package shape

import (
	"os"
	"strings"

	"github.com/benoitkugler/textprocessing/fribidi"
)

// Shape reorders and Arabic-shapes s for a terminal with no bidi support.
// Base direction is auto-detected per line from the first strong character
// (fribidi.ON), so pure-LTR lines pass through unchanged.
//
// fribidi's one-shot transform assumes a single line, so we split on '\n'
// and shape each line independently.
func Shape(s string) string {
	if s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = shapeLine(line)
	}
	return strings.Join(lines, "\n")
}

func shapeLine(line string) string {
	if line == "" {
		return line
	}
	vis, _ := ShapeRunes([]rune(line))
	// fribidi inserts zero-width U+FEFF fillers where a lam-alef ligature
	// absorbed a character. They add a visible cell gap in some terminals and
	// carry no meaning for display, so strip them here (Shape doesn't need the
	// position map; ShapeRunes keeps the fillers for callers that do).
	return strings.ReplaceAll(string(vis), "\ufeff", "")
}

// ShapeRunes reorders and Arabic-shapes one line of logical-order runes,
// returning the visual runes and visualToLogical (visualToLogical[i] = logical
// index of visual rune i). Callers carrying per-cell attributes (termstate:
// colors, cursor) use the map to move them onto the reordered runes. Unlike
// Shape, the zero-width U+FEFF lam-alef fillers are kept — they hold the map 1:1
// with the input; skip FEFF runes at render.
//
// ponytail: no width/grapheme math. §4/§6 of the design doc call for
// rivo/uniseg (grapheme clustering) and mattn/go-runewidth (cell width); neither
// was built, so the precise math does not exist yet. Consequences (see
// docs/limitations.md): the termstate cursor can sit one cell off per lam-alef
// ligature to its left (visualX ignores the stripped U+FEFF fillers), and emoji
// / ZWJ / combining-mark widths are not exhaustively correct — visible cases
// pass, edge cases are unverified. Add a width.go backed by uniseg + runewidth
// when precise cursor/width math is needed (e.g. editing in an RTL input field).
func ShapeRunes(logical []rune) (visual []rune, visualToLogical []int) {
	vis, v2l, _ := ShapeRunesDir(logical)
	return vis, v2l
}

// ShapeRunesDir is ShapeRunes plus the paragraph base direction fribidi
// resolved for the line (rtl is true when the first strong character is RTL).
// Callers that lay the line out on a grid use it to right-align an RTL
// paragraph, which is where a bidi-aware renderer would put it.
func ShapeRunesDir(logical []rune) (visual []rune, visualToLogical []int, rtl bool) {
	visual, visualToLogical, rtl, _ = ShapeRunesLayout(logical)
	return
}

// ShapeRunesLayout also maps logical insertion boundaries to visual columns.
// A boundary maps to the cell after the preceding logical character in that
// character's direction: left of an RTL character, right of an LTR one. That is
// the cell where the next typed character is drawn, so a block cursor sits on
// empty space instead of covering the character just typed.
func ShapeRunesLayout(logical []rune) (visual []rune, visualToLogical []int, rtl bool, carets []int) {
	if len(logical) == 0 {
		return nil, nil, false, []int{0}
	}
	base := fribidi.ParType(fribidi.ON) // auto-detect base direction
	vis, _ := fribidi.LogicalToVisual(fribidi.DefaultFlags, neutralizeBraille(logical), &base)
	for x, li := range vis.VisualToLogical {
		if isBraille(logical[li]) {
			vis.Str[x] = logical[li]
		}
	}
	// Warp does not reorder RTL text, but its text renderer still mirrors paired
	// punctuation according to levels resolved from the bytes it receives. Those
	// bytes are already in visual order here, so pre-mirror exactly the glyphs
	// Warp will mirror on that second pass. Disabling FriBidi mirroring globally
	// is insufficient: in an LTR paragraph, the two sides of an RTL parenthesis
	// pair can resolve to different levels and leave a pair such as (עברית) with
	// one backwards side.
	if os.Getenv("TERM_PROGRAM") == "WarpTerminal" {
		preMirrorForWarp(vis.Str)
	}
	carets = make([]int, len(logical)+1)
	for x, li := range vis.VisualToLogical {
		if vis.EmbeddingLevels[li]%2 == 1 {
			carets[li+1] = x - 1
			if li == 0 {
				carets[0] = x
			}
		} else {
			carets[li+1] = x + 1
			if li == 0 {
				carets[0] = x
			}
		}
	}
	return vis.Str, vis.VisualToLogical, base.IsRtl(), carets
}

// Terminal programs draw spinners, charts, and animated dots with Braille
// patterns, which Unicode classifies as strong LTR. Codex, for example, animates
// dots in blank cells beside its prompt; a dot landing between "›" and typed
// Hebrew would flip the row to LTR for one frame and move the caret across the
// screen. Resolve them as neutral graphics, like box drawing. U+2500 is a
// non-mirrored, non-joining ON character, so it changes only the bidi class.
func neutralizeBraille(logical []rune) []rune {
	var out []rune
	for i, r := range logical {
		if !isBraille(r) {
			continue
		}
		if out == nil {
			out = append([]rune(nil), logical...)
		}
		out[i] = '─'
	}
	if out == nil {
		return logical
	}
	return out
}

func isBraille(r rune) bool { return r >= 0x2800 && r <= 0x28ff }

func preMirrorForWarp(visual []rune) {
	types := make([]fribidi.CharType, len(visual))
	brackets := make([]fribidi.BracketType, len(visual))
	for i, r := range visual {
		types[i] = fribidi.GetBidiType(r)
		brackets[i] = fribidi.GetBracket(r)
	}
	base := fribidi.ParType(fribidi.ON)
	levels, _ := fribidi.GetParEmbeddingLevels(types, brackets, &base)
	fribidi.Shape(fribidi.ShapeMirroring, levels, nil, visual)
}
