// Package clipboard optionally restores terminal-shaped text after it is copied.
// It never records clipboard text and observes only the lifetime of its watcher.
package clipboard

import (
	"context"
	"errors"
	"sync"
	"time"
)

const (
	pollInterval = 100 * time.Millisecond
	maxTextBytes = 1 << 20
)

// Start watches new plain-text clipboard values. It ignores the clipboard value
// that existed at startup. restore receives a new value and returns a replacement
// only when it should be copied back. Calling stop waits for the watcher to exit.
func Start(restore func(string) (string, bool)) (stop func(), err error) {
	if restore == nil {
		return nil, errors.New("clipboard: restore function is required")
	}

	source, err := newNativeSource()
	if err != nil {
		return nil, err
	}
	w := watcher{source: source, restore: restore}
	if err := w.initialize(); err != nil {
		source.close()
		return nil, err
	}
	return startSource(source, restore, pollInterval), nil
}

type clipboardSample struct {
	changeCount int64
	text        string
	hasText     bool
}

// clipboardSource's replacement operation rechecks the pasteboard change count
// and foreground terminal before writing. NSPasteboard exposes no atomic CAS,
// so this narrows, but cannot eliminate, a concurrent-copy race.
type clipboardSource interface {
	snapshot() (clipboardSample, error)
	foreground() (bool, error)
	textIfCurrent(changeCount int64) (clipboardSample, bool, error)
	replaceIfCurrent(changeCount int64, text string) (newChangeCount int64, replaced bool, err error)
	close()
}

type watcher struct {
	source          clipboardSource
	restore         func(string) (string, bool)
	lastChangeCount int64
	ownChangeCount  int64
}

func (w *watcher) initialize() error {
	sample, err := w.source.snapshot()
	if err != nil {
		return err
	}
	w.lastChangeCount = sample.changeCount
	return nil
}

func (w *watcher) poll() error {
	sample, err := w.source.snapshot()
	if err != nil {
		return err
	}
	if sample.changeCount == w.lastChangeCount {
		return nil
	}
	w.lastChangeCount = sample.changeCount
	if sample.changeCount == w.ownChangeCount {
		w.ownChangeCount = 0
		return nil
	}
	foreground, err := w.source.foreground()
	if err != nil || !foreground {
		return err
	}
	sample, current, err := w.source.textIfCurrent(sample.changeCount)
	if err != nil || !current || !sample.hasText || len(sample.text) > maxTextBytes {
		return err
	}

	replacement, changed := w.restore(sample.text)
	if !changed || len(replacement) > maxTextBytes {
		return nil
	}
	newChangeCount, replaced, err := w.source.replaceIfCurrent(sample.changeCount, replacement)
	if err != nil || !replaced {
		return err
	}
	w.lastChangeCount = newChangeCount
	w.ownChangeCount = newChangeCount
	return nil
}

func startSource(source clipboardSource, restore func(string) (string, bool), interval time.Duration) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	w := &watcher{source: source, restore: restore}
	go func() {
		defer close(done)
		defer source.close()
		if err := w.initialize(); err != nil {
			return
		}
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				// Clipboard failures are deliberately fail-closed. The next poll
				// may succeed after a transient pasteboard or focus transition.
				_ = w.poll()
			}
		}
	}()

	var once sync.Once
	return func() {
		once.Do(func() {
			cancel()
			<-done
		})
	}
}
