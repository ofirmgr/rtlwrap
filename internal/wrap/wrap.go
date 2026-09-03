package wrap

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
)

// Run spawns argv on a PTY, forwarding the user's keystrokes unchanged and
// shaping the child's output through the pipe. It blocks until the child
// exits and returns the child's error (an *exec.ExitError carries its code).
func Run(argv []string) error {
	// Raw mode first: the cursor query below reads the terminal's reply from
	// stdin, which needs the line discipline out of the way, and it has to run
	// before the child exists so the reply cannot be mistaken for the child's
	// input.
	tty := term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
	if tty {
		old, err := term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return err
		}
		defer term.Restore(int(os.Stdin.Fd()), old)
	}

	var startRow int
	var typed []byte // keystrokes that arrived while the query was in flight
	if tty {
		startRow, typed = queryCursorRow(os.Stdin, os.Stdout, 250*time.Millisecond)
	}

	c := exec.Command(argv[0], argv[1:]...)
	ptmx, err := pty.Start(c)
	if err != nil {
		return err
	}
	defer ptmx.Close()

	// A real terminal gets the grid renderer on the normal screen too, so apps
	// that repaint in place (an input line, a status bar) are reshaped against
	// the live screen. Without a TTY there is no grid to repaint: fall back to
	// the scrolling renderer, which streams.
	// Size the engine from the PTY before it renders anything: the PTY winsize
	// is what the child draws into, and an engine created at the wrong size
	// would have to invalidate its grid on the first resize.
	_ = pty.InheritSize(os.Stdin, ptmx)
	cols, rows := 80, 24
	if r, c, err := pty.Getsize(ptmx); err == nil { // Getsize reports rows first
		cols, rows = c, r
	}

	var d *dispatcher
	if tty {
		d = newInlineDispatcher(os.Stdout, cols, rows, startRow)
	} else {
		d = newDispatcher(os.Stdout, cols, rows)
	}

	// Forward later terminal resizes to the child's PTY and re-size the engine
	// from the PTY, not from os.Stdout: an embedded terminal (e.g. Zed) can
	// report a different size for stdout than the PTY the child sees.
	resize := func() {
		_ = pty.InheritSize(os.Stdin, ptmx)
		if rows, cols, err := pty.Getsize(ptmx); err == nil {
			d.Resize(cols, rows)
		}
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			resize()
		}
	}()
	defer signal.Stop(ch)

	// Keystrokes are sent as-is (the child expects logical order); only the
	// child's output is shaped.
	go func() {
		if len(typed) > 0 {
			_, _ = ptmx.Write(typed)
		}
		_, _ = io.Copy(ptmx, os.Stdin)
	}()

	_, _ = io.Copy(d, ptmx) // returns when the child closes the PTY
	_ = d.Close()

	return c.Wait()
}

// queryCursorRow asks the terminal where the cursor is (DSR 6) and returns its
// 0-based row, so the grid renderer can anchor itself to the line the child
// starts on instead of assuming the top of the screen.
//
// Anything the user typed while the reply was in flight is returned as leftover
// and must be forwarded to the child. On a terminal that does not answer (or
// whose stdin takes no read deadline) it returns 0 with no leftover: the grid
// then anchors at the top row, which is where a full-screen child starts anyway.
func queryCursorRow(in, out *os.File, timeout time.Duration) (int, []byte) {
	// Probe deadline support before sending the query: without it the reply
	// would sit in stdin and reach the child as if the user had typed it.
	if err := in.SetReadDeadline(time.Time{}); err != nil {
		return 0, nil
	}
	if _, err := out.WriteString("\x1b[6n"); err != nil {
		return 0, nil
	}
	if err := in.SetReadDeadline(time.Now().Add(timeout)); err != nil {
		return 0, nil
	}
	defer in.SetReadDeadline(time.Time{})

	var acc []byte
	buf := make([]byte, 64)
	for {
		n, err := in.Read(buf)
		acc = append(acc, buf[:n]...)
		if row, rest, ok := parseCPR(acc); ok {
			return row, rest
		}
		if err != nil {
			return 0, nil // timed out or closed: the reply never came
		}
	}
}

// parseCPR pulls a cursor position report (ESC [ <row> ; <col> R) out of b,
// returning the 0-based row and b with the report removed.
func parseCPR(b []byte) (row int, rest []byte, ok bool) {
	i := bytes.Index(b, []byte("\x1b["))
	if i < 0 {
		return 0, nil, false
	}
	end := bytes.IndexByte(b[i:], 'R')
	if end < 0 {
		return 0, nil, false // still arriving
	}
	end += i
	semi := bytes.IndexByte(b[i:end], ';')
	if semi < 0 {
		return 0, nil, false
	}
	n := 0
	for _, c := range b[i+2 : i+semi] {
		if c < '0' || c > '9' {
			return 0, nil, false
		}
		n = n*10 + int(c-'0')
	}
	rest = append(append([]byte{}, b[:i]...), b[end+1:]...)
	if n > 0 {
		n-- // the report is 1-based
	}
	return n, rest, true
}
