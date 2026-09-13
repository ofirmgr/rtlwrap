package wrap

import (
	"bytes"
	"io"
	"sync"
	"time"

	"github.com/Har2yQn78/rtlwrap/internal/parser"
	"github.com/Har2yQn78/rtlwrap/internal/termstate"
)

const cursorSettleDelay = 50 * time.Millisecond

var showCursorSequence = []byte("\x1b[?25h")

// deferredCursorWriter suppresses the cursor-show sequence Engine appends to
// each repaint. The dispatcher restores it after output has been quiet long
// enough that intermediate bidi cursor positions will not be visible.
type deferredCursorWriter struct {
	out           io.Writer
	showRequested bool
}

func (w *deferredCursorWriter) Write(p []byte) (int, error) {
	originalLen := len(p)
	w.showRequested = bytes.HasSuffix(p, showCursorSequence)
	if w.showRequested {
		p = p[:len(p)-len(showCursorSequence)]
	}
	n, err := w.out.Write(p)
	if n == len(p) {
		return originalLen, err
	}
	return n, err
}

func (w *deferredCursorWriter) takeShowRequest() bool {
	requested := w.showRequested
	w.showRequested = false
	return requested
}

type dispatcher struct {
	mu          sync.Mutex // guards alt/engine/size against the SIGWINCH resize
	tk          parser.Tokenizer
	out         io.Writer
	pipe        *pipe             // scrolling renderer, used when there is no grid
	main        *termstate.Engine // normal-screen grid renderer (nil: use pipe)
	engine      *termstate.Engine // alt-screen grid renderer, nil outside alt
	alt         bool
	cols        int
	rows        int
	buf         []byte // bytes accrued for the current consumer within one Write
	rowObserver func([]rune, []rune, []int)
	gridOut     *deferredCursorWriter
	cursorTimer *time.Timer
	cursorEpoch uint64
	cursorDelay time.Duration
}

// observeRows attaches copy restoration to both grids. Call before Write.
func (d *dispatcher) observeRows(observer func([]rune, []rune, []int)) {
	d.rowObserver = observer
	if d.main != nil {
		d.main.SetRowObserver(observer)
	}
}

// newDispatcher renders the normal screen with the scrolling pipe. Used when
// the real terminal is not a TTY (a pipe or a file has no grid to repaint).
func newDispatcher(out io.Writer, cols, rows int) *dispatcher {
	return &dispatcher{
		out:         out,
		pipe:        newPipe(out),
		cols:        cols,
		rows:        rows,
		gridOut:     &deferredCursorWriter{out: out},
		cursorDelay: cursorSettleDelay,
	}
}

// newInlineDispatcher renders the normal screen with a grid engine anchored at
// startRow, so apps that repaint in place (an input line being typed into, a
// status line, a spinner) are reshaped against the live screen instead of
// per-fragment. startRow is the real cursor's 0-based row at startup.
func newInlineDispatcher(out io.Writer, cols, rows, startRow int) *dispatcher {
	d := newDispatcher(out, cols, rows)
	d.main = termstate.NewInline(d.gridOut, cols, rows, startRow)
	return d
}

func (d *dispatcher) Write(chunk []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, t := range d.tk.Push(chunk) {
		if t.Kind == parser.ANSI {
			if passthrough(t.Bytes) {
				// Layout-neutral and meant for the real terminal (mouse
				// reporting, bracketed paste, cursor shape, OSC titles and
				// clipboard). The grid renderer never re-emits these, so hand
				// them over directly, then let the virtual terminal see them
				// too so its mode state stays in step.
				if err := d.flush(); err != nil {
					return 0, err
				}
				if _, err := d.out.Write(t.Bytes); err != nil {
					return 0, err
				}
				d.buf = append(d.buf, t.Bytes...)
				continue
			}
			if clearsScreen(t.Bytes) && d.main != nil && !d.alt {
				// The child wants the whole screen gone, including the rows the
				// inline engine has been leaving alone because the shell wrote
				// them. Force the next render to paint every row.
				d.main.Invalidate()
			}
			if enter, ok := altToggle(t.Bytes); ok {
				if err := d.flush(); err != nil { // hand off buffered span
					return 0, err
				}
				if _, err := d.out.Write(t.Bytes); err != nil { // switch real buffers
					return 0, err
				}
				d.setAlt(enter)
				continue
			}
		}
		d.buf = append(d.buf, t.Bytes...)
	}
	return len(chunk), d.flush()
}

// Close flushes held bytes at EOF. Call once when the child closes the PTY.
func (d *dispatcher) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	for _, t := range d.tk.Flush() {
		d.buf = append(d.buf, t.Bytes...)
	}
	if err := d.flush(); err != nil {
		return err
	}
	if d.main != nil {
		d.cancelCursorShow()
		_, err := d.out.Write(showCursorSequence)
		return err
	}
	if d.engine != nil {
		d.cancelCursorShow()
	}
	return d.pipe.Close()
}

// Resize updates the size used for new engines and resizes the active one.
func (d *dispatcher) Resize(cols, rows int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cols, d.rows = cols, rows
	if d.engine != nil {
		d.engine.Resize(cols, rows)
	}
	if d.main != nil {
		d.main.Resize(cols, rows)
	}
}

// flush sends the accrued span to whichever renderer is currently active.
func (d *dispatcher) flush() error {
	if len(d.buf) == 0 {
		return nil
	}
	var err error
	switch {
	case d.alt:
		_, err = d.engine.Write(d.buf)
		requested := d.gridOut.takeShowRequest()
		if err == nil {
			d.scheduleCursorShow(requested)
		} else {
			d.cancelCursorShow()
		}
	case d.main != nil:
		_, err = d.main.Write(d.buf)
		requested := d.gridOut.takeShowRequest()
		if err == nil {
			d.scheduleCursorShow(requested)
		} else {
			d.cancelCursorShow()
		}
	default:
		_, err = d.pipe.Write(d.buf)
	}
	d.buf = d.buf[:0]
	return err
}

func (d *dispatcher) setAlt(enter bool) {
	if enter == d.alt {
		return
	}
	d.cancelCursorShow()
	d.alt = enter
	if enter {
		// Fresh engine per alt session: the real terminal just cleared to its
		// alt buffer, so the grid starts blank and matches.
		d.engine = termstate.New(d.gridOut, d.cols, d.rows)
		d.engine.SetRowObserver(d.rowObserver)
	} else {
		d.engine = nil
		if d.main != nil {
			d.scheduleCursorShow(true)
		}
	}
}

// scheduleCursorShow resets the quiet-period timer after each grid repaint.
// d.mu must be held.
func (d *dispatcher) scheduleCursorShow(requested bool) {
	d.cancelCursorShow()
	if !requested {
		return
	}
	epoch := d.cursorEpoch
	d.cursorTimer = time.AfterFunc(d.cursorDelay, func() {
		d.showCursor(epoch)
	})
}

// cancelCursorShow invalidates both a pending timer and a callback already
// waiting for d.mu. d.mu must be held.
func (d *dispatcher) cancelCursorShow() {
	d.cursorEpoch++
	if d.cursorTimer != nil {
		d.cursorTimer.Stop()
		d.cursorTimer = nil
	}
}

func (d *dispatcher) showCursor(epoch uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if epoch != d.cursorEpoch {
		return
	}
	d.cursorTimer = nil
	_, _ = d.out.Write(showCursorSequence)
}

// clearsScreen reports whether b erases the whole screen: ED 2 or 3
// (CSI [?] 2J / 3J) or a full reset (ESC c).
func clearsScreen(b []byte) bool {
	if len(b) == 2 && b[0] == 0x1b && b[1] == 'c' { // RIS
		return true
	}
	if len(b) < 4 || b[0] != 0x1b || b[1] != '[' || b[len(b)-1] != 'J' {
		return false
	}
	body := b[2 : len(b)-1]
	if len(body) > 0 && body[0] == '?' { // DECSED, private form of ED
		body = body[1:]
	}
	return string(body) == "2" || string(body) == "3"
}

// passthrough reports whether b is an escape sequence the real terminal must
// receive verbatim: it changes no cell on the grid, and dropping it breaks
// features the grid renderer cannot reproduce.
func passthrough(b []byte) bool {
	if len(b) < 2 || b[0] != 0x1b {
		return false
	}
	if b[1] == ']' { // OSC: window title, hyperlinks, OSC 52 clipboard
		return true
	}
	if b[1] != '[' {
		return false
	}
	body := b[2 : len(b)-1]
	switch b[len(b)-1] {
	case 'h', 'l': // DECSET/DECRST
		if len(body) == 0 || body[0] != '?' {
			return false
		}
		switch string(body[1:]) {
		case "9", "1000", "1001", "1002", "1003", "1005", "1006", "1015", "1016": // mouse reporting
			return true
		case "1004": // focus reporting
			return true
		case "2004": // bracketed paste
			return true
		case "2026": // synchronized output: keep partial bidi redraws off screen
			return true
		}
	case 'q': // DECSCUSR (cursor shape) is "CSI <n> SP q"
		return len(body) > 0 && body[len(body)-1] == ' '
	}
	return false
}

// altToggle reports whether b is an alt-screen enter/exit private-mode set
// (ESC[?<n>h) or reset (ESC[?<n>l) for n in {1049, 1047, 47}. enter is true for
// the set form.
func altToggle(b []byte) (enter, ok bool) {
	if len(b) < 5 || b[0] != 0x1b || b[1] != '[' || b[2] != '?' {
		return false, false
	}
	last := b[len(b)-1]
	if last != 'h' && last != 'l' {
		return false, false
	}
	switch string(b[3 : len(b)-1]) {
	case "1049", "1047", "47":
		return last == 'h', true
	}
	return false, false
}
