package clipboard

import (
	"sync"
	"testing"
	"time"
)

type mockSource struct {
	mu            sync.Mutex
	sample        clipboardSample
	isForeground  bool
	replaceCalls  int
	closed        bool
	mutateOnWrite bool
}

func (m *mockSource) snapshot() (clipboardSample, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sample, nil
}

func (m *mockSource) foreground() (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.isForeground, nil
}

func (m *mockSource) textIfCurrent(change int64) (clipboardSample, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sample, m.sample.changeCount == change, nil
}

func (m *mockSource) replaceIfCurrent(change int64, text string) (int64, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.replaceCalls++
	if m.mutateOnWrite {
		m.sample.changeCount++
	}
	if !m.isForeground || m.sample.changeCount != change {
		return m.sample.changeCount, false, nil
	}
	m.sample.changeCount++
	m.sample.text = text
	m.sample.hasText = true
	return m.sample.changeCount, true, nil
}

func (m *mockSource) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
}

func (m *mockSource) update(sample clipboardSample) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sample = sample
}

func TestWatcherIgnoresExistingAndUnchangedClipboard(t *testing.T) {
	source := &mockSource{sample: clipboardSample{changeCount: 7, text: "old", hasText: true}, isForeground: true}
	calls := 0
	w := watcher{source: source, restore: func(string) (string, bool) { calls++; return "new", true }}
	if err := w.initialize(); err != nil {
		t.Fatal(err)
	}
	if err := w.poll(); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || source.replaceCalls != 0 {
		t.Fatalf("existing or unchanged clipboard was processed: restore=%d replace=%d", calls, source.replaceCalls)
	}
}

func TestWatcherSuppressesOwnWrite(t *testing.T) {
	source := &mockSource{sample: clipboardSample{changeCount: 1, text: "old", hasText: true}, isForeground: true}
	calls := 0
	w := watcher{source: source, restore: func(string) (string, bool) { calls++; return "fixed", true }}
	if err := w.initialize(); err != nil {
		t.Fatal(err)
	}
	source.update(clipboardSample{changeCount: 2, text: "copied", hasText: true})
	if err := w.poll(); err != nil {
		t.Fatal(err)
	}
	if err := w.poll(); err != nil {
		t.Fatal(err)
	}
	if calls != 1 || source.replaceCalls != 1 {
		t.Fatalf("own write was processed again: restore=%d replace=%d", calls, source.replaceCalls)
	}
}

func TestWatcherFailsClosedWhenTerminalNotForeground(t *testing.T) {
	source := &mockSource{sample: clipboardSample{changeCount: 1}, isForeground: false}
	calls := 0
	w := watcher{source: source, restore: func(string) (string, bool) { calls++; return "fixed", true }}
	if err := w.initialize(); err != nil {
		t.Fatal(err)
	}
	source.update(clipboardSample{changeCount: 2, text: "copied", hasText: true})
	if err := w.poll(); err != nil {
		t.Fatal(err)
	}
	if calls != 0 || source.replaceCalls != 0 {
		t.Fatalf("unexpected processing counts: restore=%d replace=%d", calls, source.replaceCalls)
	}
}

func TestWatcherDoesNotOverwriteChangedClipboard(t *testing.T) {
	source := &mockSource{sample: clipboardSample{changeCount: 1}, isForeground: true, mutateOnWrite: true}
	w := watcher{source: source, restore: func(string) (string, bool) { return "fixed", true }}
	if err := w.initialize(); err != nil {
		t.Fatal(err)
	}
	source.update(clipboardSample{changeCount: 2, text: "copied", hasText: true})
	if err := w.poll(); err != nil {
		t.Fatal(err)
	}
	source.mu.Lock()
	defer source.mu.Unlock()
	if source.sample.text == "fixed" {
		t.Fatal("replacement overwrote a clipboard changed after read")
	}
}

func TestStopWaitsForWorkerExit(t *testing.T) {
	source := &mockSource{sample: clipboardSample{changeCount: 1}, isForeground: true}
	stop := startSource(source, func(string) (string, bool) { return "", false }, time.Hour)
	stop()
	source.mu.Lock()
	defer source.mu.Unlock()
	if !source.closed {
		t.Fatal("stop returned before worker closed source")
	}
}
