package termstate

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Har2yQn78/rtlwrap/internal/vt10x"

	"github.com/Har2yQn78/rtlwrap/internal/shape"
)

// vt10x attribute bits (mirrors the unexported consts in vt10x/state.go).
const (
	attrReverse   = 1 << 0
	attrUnderline = 1 << 1
	attrBold      = 1 << 2
	attrItalic    = 1 << 4
	attrBlink     = 1 << 5
)

// Engine feeds child output into a vt10x virtual terminal and renders the grid
// to w, reshaping RTL rows. Not safe for concurrent Write; drive it from one
// goroutine (the output copy loop).
type Engine struct {
	vt          vt10x.Terminal
	w           io.Writer
	cols, rows  int
	prev        []string        // last-emitted content per row, for diffing
	inline      bool            // normal screen: preserve the real terminal's scrollback
	scrolled    [][]vt10x.Glyph // rows pushed off the top since the last render, oldest first
	rowObserver func(logical, visual []rune, visualToLogical []int)
}

// SetRowObserver observes the text and mapping used to render each row,
// including rows entering scrollback. The callback must copy retained slices.
// Set it before Write; like Engine, this method is not concurrency safe.
func (e *Engine) SetRowObserver(observer func([]rune, []rune, []int)) {
	e.rowObserver = observer
}

// New returns an Engine rendering a cols×rows virtual terminal to w, for an
// alternate-screen session: the real terminal just cleared to its alt buffer,
// so the grid and the screen both start blank and every row is ours to paint.
func New(w io.Writer, cols, rows int) *Engine {
	return &Engine{
		vt:   vt10x.New(vt10x.WithSize(cols, rows)),
		w:    w,
		cols: cols,
		rows: rows,
		prev: make([]string, rows),
	}
}

// NewInline returns an Engine for the normal screen, where the rows above the
// child's starting point hold the shell's existing output and the real
// terminal owns a scrollback the child expects to keep filling.
//
// startRow is the real cursor's 0-based row when the child starts; the virtual
// cursor is moved there so the grid and the screen agree on coordinates. Rows
// the child has not written keep their blank rendering in prev, so they are
// never repainted and whatever the shell left there survives.
//
// When rows scroll off the top, the engine scrolls the real terminal by the
// same amount instead of repainting: the lines leaving the screen are the ones
// it already painted, so they enter the real scrollback correctly shaped.
func NewInline(w io.Writer, cols, rows, startRow int) *Engine {
	e := &Engine{
		vt:     vt10x.New(vt10x.WithSize(cols, rows)),
		w:      w,
		cols:   cols,
		rows:   rows,
		prev:   make([]string, rows),
		inline: true,
	}
	e.vt.SetScrollCallback(func(lines [][]vt10x.Glyph) {
		e.scrolled = append(e.scrolled, lines...)
	})
	if startRow > 0 {
		if startRow > rows-1 {
			startRow = rows - 1
		}
		_, _ = e.vt.Write([]byte(strings.Repeat("\n", startRow)))
	}
	e.seedPrev()
	return e
}

// seedPrev records the current grid as already on screen, so the first render
// emits only what the child changes.
func (e *Engine) seedPrev() {
	e.vt.Lock()
	defer e.vt.Unlock()
	for y := 0; y < e.rows; y++ {
		e.prev[y], _, _ = e.renderRow(y, e.cols)
	}
	e.scrolled = nil
}

// Invalidate forgets what the screen is assumed to show, so the next render
// paints every row. The inline engine starts out assuming the rows it has not
// written still hold the shell's output; a child that clears the screen expects
// those to go, and only a full repaint can do that.
func (e *Engine) Invalidate() {
	e.prev = make([]string, e.rows)
}

// Resize resizes the virtual terminal and forces a full repaint next render.
func (e *Engine) Resize(cols, rows int) {
	e.vt.Resize(cols, rows)
	e.cols, e.rows = cols, rows
	e.prev = make([]string, rows)
	e.scrolled = nil // the resize itself reflowed both screens
	if e.inline {
		// Take the reflowed grid as already on screen rather than repainting
		// it: rows the child never wrote hold the shell's own output, and
		// painting the grid's blanks over them would erase it. The child gets
		// SIGWINCH and repaints what it owns.
		e.seedPrev()
	}
	// Alt screen: leave prev empty ("" matches no rendered row) for a full
	// repaint, which is what the child's own alt buffer expects.
}

// Write feeds raw child bytes to the virtual terminal, then renders. It always
// reports len(p) consumed (vt10x parses the whole buffer).
func (e *Engine) Write(p []byte) (int, error) {
	if _, err := e.vt.Write(p); err != nil {
		return 0, err
	}
	return len(p), e.render()
}

func (e *Engine) render() error {
	e.vt.Lock()
	defer e.vt.Unlock()

	cols, rows := e.vt.Size()
	if cols != e.cols || rows != e.rows || len(e.prev) != rows {
		e.cols, e.rows, e.prev = cols, rows, make([]string, rows)
	}

	var buf bytes.Buffer
	// Hide cursor + disable autowrap during the repaint so a full-width row's
	// last column can't scroll the screen. Restored at the end.
	buf.WriteString("\x1b[?25l\x1b[?7l")

	// Push the rows that left the grid through the real terminal's top row and
	// scroll, so they enter its scrollback exactly as the user would have seen
	// them — including a burst that scrolled more rows than the screen holds,
	// where no amount of repainting could recover them.
	if len(e.scrolled) > 0 {
		out := e.scrolled
		e.scrolled = nil
		for _, cells := range out {
			line, _, _ := e.renderCells(cells, cols)
			if line != e.prev[0] { // already on the top row: no need to repaint
				buf.WriteString("\x1b[1;1H\x1b[2K")
				buf.WriteString(line)
			}
			fmt.Fprintf(&buf, "\x1b[%d;1H\n", rows) // scroll: top row -> scrollback
			copy(e.prev, e.prev[1:])
			e.prev[rows-1] = "" // scrolled in blank: repaint whatever lands here
		}
	}

	l2v := make([][]int, rows) // per-row visualToLogical, kept for cursor mapping
	pads := make([]int, rows)  // per-row left pad of a right-aligned RTL row
	for y := 0; y < rows; y++ {
		line, v2l, pad := e.renderRow(y, cols)
		l2v[y], pads[y] = v2l, pad
		if line == e.prev[y] {
			continue
		}
		e.prev[y] = line
		fmt.Fprintf(&buf, "\x1b[%d;1H\x1b[2K", y+1) // move to row start, clear it
		buf.WriteString(line)
	}

	buf.WriteString("\x1b[?7h") // re-enable autowrap
	cur := e.vt.Cursor()
	cx := cur.X
	if cur.Y >= 0 && cur.Y < rows {
		cx = visualX(l2v[cur.Y], cur.X, cols) + pads[cur.Y]
		if cx > cols-1 {
			cx = cols - 1
		}
	}
	fmt.Fprintf(&buf, "\x1b[%d;%dH", cur.Y+1, cx+1)
	if e.vt.CursorVisible() {
		buf.WriteString("\x1b[?25h")
	}

	_, err := e.w.Write(buf.Bytes())
	return err
}

// renderRow builds the SGR-encoded visual string for grid row y and returns it
// with that row's visualToLogical map (nil if the row had no runes).
func (e *Engine) renderRow(y, cols int) (string, []int, int) {
	cells := make([]vt10x.Glyph, cols)
	for x := 0; x < cols; x++ {
		cells[x] = e.vt.Cell(x, y)
	}
	return e.renderCells(cells, cols)
}

// renderCells builds the SGR-encoded visual string for one row of glyphs (from
// the grid, or from a row that has scrolled off it), its visualToLogical map,
// and the left pad the line is printed at.
//
// Only the row's used prefix is reordered. A terminal row is padded to the full
// width with blanks, and those blanks are not text: feeding them to the bidi
// reorder would attach them to a trailing RTL run and push the whole line to
// the right edge. They are dropped instead — the row is painted after \x1b[2K,
// so a default-attribute blank paints nothing. A blank carrying background,
// underline or reverse does show, so it counts as used.
//
// A row whose resolved paragraph direction is RTL is then right-aligned: the
// line is printed pad cells in from the left so it ends at the right edge,
// which is where a bidi-aware renderer puts an RTL paragraph. The pad is a
// cursor-forward move over the just-cleared row, so it paints nothing itself.
func (e *Engine) renderCells(cells []vt10x.Glyph, cols int) (string, []int, int) {
	type attr struct {
		fg, bg vt10x.Color
		mode   int16
	}
	logical := make([]rune, cols)
	attrs := make([]attr, cols)
	used := 0
	for x := 0; x < cols; x++ {
		g := vt10x.Glyph{FG: vt10x.DefaultFG, BG: vt10x.DefaultBG}
		if x < len(cells) {
			g = cells[x]
		}
		r := g.Char
		if r == 0 {
			r = ' '
		}
		logical[x] = r
		attrs[x] = attr{g.FG, g.BG, g.Mode}
		if r != ' ' || g.BG != vt10x.DefaultBG || g.Mode&(attrUnderline|attrReverse) != 0 {
			used = x + 1
		}
	}

	vis, v2l, rtl := shape.ShapeRunesDir(logical[:used])
	if e.rowObserver != nil {
		e.rowObserver(logical[:used], vis, v2l)
	}
	// The dropped tail still needs map entries: the cursor can sit in it.
	for x := used; x < cols; x++ {
		v2l = append(v2l, x)
	}

	// Width in cells of what is actually painted: the U+FEFF fillers below
	// occupy none. ponytail: no wide-rune math (see shape.ShapeRunes), so a
	// row holding CJK or emoji right-aligns a cell short per wide rune.
	width := 0
	for _, r := range vis {
		if r != '\ufeff' {
			width++
		}
	}
	pad := 0
	if rtl && width < cols {
		pad = cols - width
	}

	var sb strings.Builder
	if pad > 0 {
		fmt.Fprintf(&sb, "\x1b[%dC", pad)
	}
	last := attr{fg: ^vt10x.Color(0)} // impossible value forces first SGR emit
	for i, r := range vis {
		if r == '\ufeff' { // lam-alef filler: no cell, keeps the map 1:1
			continue
		}
		a := attrs[v2l[i]]
		if a != last {
			sb.WriteString(sgr(a.fg, a.bg, a.mode))
			last = a
		}
		sb.WriteRune(r)
	}
	sb.WriteString("\x1b[0m")
	return sb.String(), v2l, pad
}

// visualX maps a logical column to its visual column in a reshaped row.
//
// ponytail: does not subtract stripped U+FEFF fillers to the cursor's left, so
// in a row containing lam-alef ligatures the cursor can sit one cell off per
// ligature. Rare; fix by counting skipped fillers when it actually bites.
func visualX(v2l []int, logicalX, cols int) int {
	for i, li := range v2l {
		if li == logicalX {
			return i
		}
	}
	if logicalX >= cols {
		return cols - 1
	}
	return logicalX
}

// sgr renders a full SGR sequence (leading reset, then attributes and colors)
// for the given cell. Resetting first each time is a few extra bytes but avoids
// tracking incremental attribute add/remove state.
func sgr(fg, bg vt10x.Color, mode int16) string {
	p := []string{"0"}
	if mode&attrBold != 0 {
		p = append(p, "1")
	}
	if mode&attrItalic != 0 {
		p = append(p, "3")
	}
	if mode&attrUnderline != 0 {
		p = append(p, "4")
	}
	if mode&attrBlink != 0 {
		p = append(p, "5")
	}
	if mode&attrReverse != 0 {
		p = append(p, "7")
	}
	p = append(p, colorParams(fg, true)...)
	p = append(p, colorParams(bg, false)...)
	return "\x1b[" + strings.Join(p, ";") + "m"
}

// colorParams renders one color as SGR params, or nil for the default (already
// covered by the leading reset). base is 30 for foreground, 40 for background.
func colorParams(c vt10x.Color, fg bool) []string {
	base := 40
	if fg {
		base = 30
	}
	switch {
	case c == vt10x.DefaultFG || c == vt10x.DefaultBG:
		return nil
	case c < 8:
		return []string{strconv.Itoa(base + int(c))}
	case c < 16:
		return []string{strconv.Itoa(base + 60 + int(c-8))} // bright: 90/100 range
	case c < 256:
		return []string{strconv.Itoa(base + 8), "5", strconv.Itoa(int(c))} // 256-color
	default: // truecolor packed as r<<16 | g<<8 | b
		r, g, b := (c>>16)&0xff, (c>>8)&0xff, c&0xff
		return []string{strconv.Itoa(base + 8), "2", strconv.Itoa(int(r)), strconv.Itoa(int(g)), strconv.Itoa(int(b))}
	}
}
