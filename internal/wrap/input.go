package wrap

import (
	"errors"
	"io"
	"os"
	"time"

	"github.com/Har2yQn78/rtlwrap/internal/termstate"
)

// transformInput swaps Left and Right arrow keys in chunk when swap is true.
//
// Supported sequences:
// - \x1b[...D <-> \x1b[...C (standard and modified CSI arrows, e.g. Left/Right, Option+Left/Right)
// - \x1bOD <-> \x1bOC (application cursor keys mode)
// - \x1bb <-> \x1bf (Option+Left/Right in word navigation)
func transformInput(b []byte, swap bool) []byte {
	if !swap || len(b) == 0 {
		return b
	}

	var out []byte
	i := 0
	for i < len(b) {
		if b[i] != 0x1b {
			if out != nil {
				out = append(out, b[i])
			}
			i++
			continue
		}

		if i+1 < len(b) {
			// Alt+b / Alt+f (Option+Left / Option+Right in word navigation)
			if b[i+1] == 'b' {
				if out == nil {
					out = append(make([]byte, 0, len(b)), b[:i]...)
				}
				out = append(out, 0x1b, 'f')
				i += 2
				continue
			}
			if b[i+1] == 'f' {
				if out == nil {
					out = append(make([]byte, 0, len(b)), b[:i]...)
				}
				out = append(out, 0x1b, 'b')
				i += 2
				continue
			}

			// SS3: \x1bOD <-> \x1bOC
			if b[i+1] == 'O' && i+2 < len(b) {
				if b[i+2] == 'D' {
					if out == nil {
						out = append(make([]byte, 0, len(b)), b[:i]...)
					}
					out = append(out, 0x1b, 'O', 'C')
					i += 3
					continue
				}
				if b[i+2] == 'C' {
					if out == nil {
						out = append(make([]byte, 0, len(b)), b[:i]...)
					}
					out = append(out, 0x1b, 'O', 'D')
					i += 3
					continue
				}
			}

			// CSI: \x1b[ ...
			if b[i+1] == '[' {
				j := i + 2
				for j < len(b) && b[j] >= 0x20 && b[j] <= 0x3f {
					j++
				}
				if j < len(b) {
					final := b[j]
					if final == 'D' {
						if out == nil {
							out = append(make([]byte, 0, len(b)), b[:i]...)
						}
						out = append(out, b[i:j]...)
						out = append(out, 'C')
						i = j + 1
						continue
					}
					if final == 'C' {
						if out == nil {
							out = append(make([]byte, 0, len(b)), b[:i]...)
						}
						out = append(out, b[i:j]...)
						out = append(out, 'D')
						i = j + 1
						continue
					}
					if out != nil {
						out = append(out, b[i:j+1]...)
					}
					i = j + 1
					continue
				}
			}
		}

		if out != nil {
			out = append(out, b[i])
		}
		i++
	}

	if out == nil {
		return b
	}
	return out
}

// incompleteEscapeLen returns the number of trailing bytes that form an
// incomplete escape sequence at the end of b, or 0 if complete.
func incompleteEscapeLen(b []byte) int {
	if len(b) == 0 {
		return 0
	}
	start := 0
	if len(b) > 16 {
		start = len(b) - 16
	}
	lastEsc := -1
	for k := len(b) - 1; k >= start; k-- {
		if b[k] == 0x1b {
			lastEsc = k
			break
		}
	}
	if lastEsc < 0 {
		return 0
	}

	sub := b[lastEsc:]
	if len(sub) == 1 {
		return 1
	}
	if sub[1] == 'O' && len(sub) == 2 {
		return 2
	}
	if sub[1] == '[' {
		for idx := 2; idx < len(sub); idx++ {
			if sub[idx] >= 0x40 && sub[idx] <= 0x7e {
				return 0
			}
		}
		return len(sub)
	}
	return 0
}

// visualArrows rewrites each unmodified Left/Right in b into the logical moves
// that reach the visually adjacent position of caret c. Other input, and arrows
// c cannot place, go through transformInput(..., swap). It returns the caret
// predicted after b, or nil once b holds other input or an unplaced arrow.
func visualArrows(b []byte, c *termstate.Caret, swap bool) ([]byte, *termstate.Caret) {
	var out []byte
	start := 0
	for i := 0; i+2 < len(b); i++ {
		if b[i] != 0x1b || (b[i+1] != '[' && b[i+1] != 'O') || (b[i+2] != 'D' && b[i+2] != 'C') {
			continue
		}
		if start < i {
			out = append(out, transformInput(b[start:i], swap)...)
			c = nil
		}
		dx := -1
		if b[i+2] == 'C' {
			dx = 1
		}
		if n, ok := c.Step(dx); ok {
			key, count := byte('C'), n
			if n < 0 {
				key, count = 'D', -n
			}
			for range count {
				out = append(out, 0x1b, b[i+1], key)
			}
			c = &termstate.Caret{Map: c.Map, X: c.X + n}
		} else if c == nil {
			out = append(out, transformInput(b[i:i+3], swap)...)
		}
		start = i + 3
		i += 2
	}
	if start < len(b) {
		out = append(out, transformInput(b[start:], swap)...)
		c = nil
	}
	return out, c
}

// makeTranslator returns the keystroke rewrite for options. By default plain
// arrows move to the visually adjacent caret position, and other horizontal
// keys swap while the caret sits in RTL text; with the caret unknown they swap
// while currentLanguage is Hebrew. Call the result from one goroutine.
func makeTranslator(options Options, caret func() *termstate.Caret, currentLanguage func() string) func([]byte) []byte {
	if options.DisableSwapArrows || os.Getenv("RTLWRAP_NO_SWAP_ARROWS") != "" {
		return func(b []byte) []byte { return b }
	}
	if options.SwapArrows || os.Getenv("RTLWRAP_SWAP_ARROWS") != "" {
		return func(b []byte) []byte { return transformInput(b, true) }
	}
	// Until the child's response is rendered, later arrows start from where
	// the previous ones were sent, so key repeat does not reuse a stale caret.
	var last, predicted *termstate.Caret
	return func(b []byte) []byte {
		rendered := caret()
		c := rendered
		if rendered != nil && rendered == last {
			c = predicted
		}
		dir := rendered.Direction()
		swap := dir < 0 || dir == 0 && currentLanguage() == "he"
		out, next := visualArrows(b, c, swap)
		last, predicted = rendered, next
		return out
	}
}

// forwardInput streams bytes from src to dst through translate. An incomplete
// trailing escape sequence is held for up to escapeTimeout before being flushed
// as-is.
func forwardInput(dst io.Writer, src io.Reader, translate func([]byte) []byte, escapeTimeout time.Duration) error {
	ch := make(chan []byte, 1)
	errCh := make(chan error, 1)
	done := make(chan struct{})
	defer close(done)

	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := src.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case ch <- chunk:
				case <-done:
					return
				}
			}
			if err != nil {
				select {
				case errCh <- err:
				case <-done:
				}
				return
			}
		}
	}()

	var pending []byte
	var timer *time.Timer
	var timerC <-chan time.Time

	flushPending := func() error {
		if len(pending) == 0 {
			return nil
		}
		out := translate(pending)
		pending = nil
		_, err := dst.Write(out)
		return err
	}

	for {
		select {
		case chunk := <-ch:
			if timer != nil {
				timer.Stop()
				timer = nil
				timerC = nil
			}
			data := append(pending, chunk...)
			pending = nil

			inc := incompleteEscapeLen(data)
			var toProcess []byte
			if inc > 0 {
				splitAt := len(data) - inc
				toProcess = data[:splitAt]
				pending = data[splitAt:]
				timer = time.NewTimer(escapeTimeout)
				timerC = timer.C
			} else {
				toProcess = data
			}

			if len(toProcess) > 0 {
				out := translate(toProcess)
				if _, err := dst.Write(out); err != nil {
					return err
				}
			}

		case <-timerC:
			timer = nil
			timerC = nil
			if err := flushPending(); err != nil {
				return err
			}

		case err := <-errCh:
			if timer != nil {
				timer.Stop()
			}
			_ = flushPending()
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}
