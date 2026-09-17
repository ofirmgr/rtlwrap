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
	vt           vt10x.Terminal
	w            io.Writer
	cols, rows   int
	prev         []string        // last-emitted content per row, for diffing
	inline       bool            // normal screen: preserve the real terminal's scrollback
	scrolled     [][]vt10x.Glyph // rows pushed off the top since the last render, oldest first
	rowObserver  func(logical, visual []rune, visualToLogical []int)
	language     string
	badgeRow     int // one-based; zero means no label is on screen
	badgeLine    string
	owned        []bool // rows whose contents are known, excluding pre-existing shell output
	synchronized bool   // apply child updates without displaying an incomplete frame
}

// SetSynchronizedOutput defers painting until the dispatcher completes the frame.
func (e *Engine) SetSynchronizedOutput(enabled bool) { e.synchronized = enabled }

// SetLanguage selects the display-only input label. Render with Write(nil).
func (e *Engine) SetLanguage(language string) { e.language = language }

func (e *Engine) eraseBadge(buf *bytes.Buffer) {
	if e.badgeRow == 0 {
		return
	}
	fmt.Fprintf(buf, "\x1b[%d;1H\x1b[2K%s", e.badgeRow, e.badgeLine)
	e.badgeRow = 0
}

// ClearLanguageBadge restores covered text without changing the child cursor.
func (e *Engine) ClearLanguageBadge() error {
	if e.badgeRow == 0 {
		return nil
	}
	var buf bytes.Buffer
	buf.WriteString("\x1b7\x1b[?7l")
	e.eraseBadge(&buf)
	buf.WriteString("\x1b[?7h\x1b8")
	_, err := e.w.Write(buf.Bytes())
	return err
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
		owned:  make([]bool, rows),
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
	e.owned = nil // The child explicitly took ownership of the whole screen.
}

// Resize resizes the virtual terminal and forces a full repaint next render.
func (e *Engine) Resize(cols, rows int) {
	// SIGWINCH arrives after host reflow. Old coordinates no longer identify
	// the covered cells, so never restore an old row into the resized screen.
	// The child's next repaint clears any label retained by terminal reflow.
	e.badgeRow = 0
	e.badgeLine = ""
	e.vt.Resize(cols, rows)
	e.cols, e.rows = cols, rows
	e.prev = make([]string, rows)
	e.scrolled = nil // the resize itself reflowed both screens
	if e.inline {
		e.owned = make([]bool, rows)
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
	if e.synchronized {
		return nil
	}
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
	e.eraseBadge(&buf) // Restore before scrolling so labels never enter scrollback.

	// Push the rows that left the grid through the real terminal's top row and
	// scroll, so they enter its scrollback exactly as the user would have seen
	// them — including a burst that scrolled more rows than the screen holds,
	// where no amount of repainting could recover them.
	if len(e.scrolled) > 0 {
		out := e.scrolled
		e.scrolled = nil
		for _, cells := range out {
			line, _, _ := e.renderCells(cells, cols, 0)
			if line != e.prev[0] { // already on the top row: no need to repaint
				buf.WriteString("\x1b[1;1H\x1b[2K")
				buf.WriteString(line)
			}
			fmt.Fprintf(&buf, "\x1b[%d;1H\n", rows) // scroll: top row -> scrollback
			copy(e.prev, e.prev[1:])
			if e.owned != nil {
				copy(e.owned, e.owned[1:])
				e.owned[rows-1] = true
			}
			e.prev[rows-1] = "" // scrolled in blank: repaint whatever lands here
		}
	}

	caretMaps := make([][]int, rows) // logical insertion boundaries to visual columns
	pads := make([]int, rows)        // per-row left pad of a right-aligned RTL row
	for y := 0; y < rows; y++ {
		line, carets, pad := e.renderRow(y, cols)
		caretMaps[y], pads[y] = carets, pad
		if line == e.prev[y] {
			continue
		}
		e.prev[y] = line
		if e.owned != nil {
			e.owned[y] = true
		}
		fmt.Fprintf(&buf, "\x1b[%d;1H\x1b[2K", y+1) // move to row start, clear it
		buf.WriteString(line)
	}

	cur := e.vt.Cursor()
	cx := cur.X
	if cur.Y >= 0 && cur.Y < rows {
		cx = visualX(caretMaps[cur.Y], cur.X, cols) + pads[cur.Y]
		cx = min(max(cx, 0), cols-1)
	}
	label := ""
	switch e.language {
	case "he":
		label = "he"
	case "en":
		label = "en"
	}
	if label != "" && e.vt.CursorVisible() && cols >= 2 && cur.Y > 0 && cur.Y < rows &&
		(e.owned == nil || e.owned[cur.Y-1]) {
		x := min(max(cx, 0), cols-2)
		e.badgeRow, e.badgeLine = cur.Y, e.prev[cur.Y-1]
		fmt.Fprintf(&buf, "\x1b[%d;%dH\x1b[0;2m%s\x1b[0m", cur.Y, x+1, label)
	}
	buf.WriteString("\x1b[?7h") // re-enable autowrap
	fmt.Fprintf(&buf, "\x1b[%d;%dH", cur.Y+1, cx+1)
	if e.vt.CursorVisible() {
		buf.WriteString("\x1b[?25h")
	}

	_, err := e.w.Write(buf.Bytes())
	return err
}

// renderRow builds the SGR-encoded visual string for grid row y and returns it
// with that row's logical insertion-boundary map.
func (e *Engine) renderRow(y, cols int) (string, []int, int) {
	cells := make([]vt10x.Glyph, cols)
	for x := 0; x < cols; x++ {
		cells[x] = e.vt.Cell(x, y)
	}
	// Spaces before the insertion point belong to the editable text, too.
	minUsed := 0
	if cur := e.vt.Cursor(); cur.Y == y {
		minUsed = min(cur.X, cols)
	}
	return e.renderCells(cells, cols, minUsed)
}

// renderCells builds the SGR-encoded visual string for one row of glyphs (from
// the grid, or from a row that has scrolled off it), its insertion-boundary map,
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
func (e *Engine) renderCells(cells []vt10x.Glyph, cols, minUsed int) (string, []int, int) {
	type attr struct {
		fg, bg vt10x.Color
		mode   int16
	}
	logical := make([]rune, cols)
	attrs := make([]attr, cols)
	used := minUsed
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
			used = max(used, x+1)
		}
	}

	vis, v2l, rtl, carets := shape.ShapeRunesLayout(logical[:used])
	if e.rowObserver != nil {
		e.rowObserver(logical[:used], vis, v2l)
	}
	// The dropped tail still needs map entries: the cursor can sit in it.
	for x := used + 1; x <= cols; x++ {
		carets = append(carets, x)
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
	return sb.String(), carets, pad
}

// visualX maps a logical column to its visual column in a reshaped row.
//
// ponytail: does not subtract stripped U+FEFF fillers to the cursor's left, so
// in a row containing lam-alef ligatures the cursor can sit one cell off per
// ligature. Rare; fix by counting skipped fillers when it actually bites.
func visualX(carets []int, logicalX, cols int) int {
	if logicalX >= 0 && logicalX < len(carets) {
		return min(carets[logicalX], cols-1)
	}
	return min(logicalX, cols-1)
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
