package wrap

import (
	"errors"
	"io"
	"os"
	"time"

	"github.com/Har2yQn78/rtlwrap/internal/inputlang"
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

func makeShouldSwap(options Options) func() bool {
	if options.DisableSwapArrows || os.Getenv("RTLWRAP_NO_SWAP_ARROWS") != "" {
		return func() bool { return false }
	}
	if options.SwapArrows || os.Getenv("RTLWRAP_SWAP_ARROWS") != "" {
		return func() bool { return true }
	}
	return func() bool {
		return inputlang.Current() == "he"
	}
}

// forwardInput streams bytes from src to dst, transforming arrow keys when
// shouldSwap returns true. An incomplete trailing escape sequence is held
// for up to escapeTimeout before being flushed as-is.
func forwardInput(dst io.Writer, src io.Reader, shouldSwap func() bool, escapeTimeout time.Duration) error {
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
		swap := shouldSwap()
		out := transformInput(pending, swap)
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
				swap := shouldSwap()
				out := transformInput(toProcess, swap)
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
