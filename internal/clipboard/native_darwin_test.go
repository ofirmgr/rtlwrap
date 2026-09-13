//go:build darwin && cgo

package clipboard

import (
	"fmt"
	"testing"
	"time"

	"github.com/Har2yQn78/rtlwrap/internal/copytext"
	"github.com/Har2yQn78/rtlwrap/internal/shape"
)

func TestNamedPasteboardRoundTrip(t *testing.T) {
	// This deliberately uses a process-unique named pasteboard, never the user's
	// general pasteboard. Named test boards do not require a terminal ancestor.
	source, err := newNamedNativeSource(fmt.Sprintf("rtlwrap.clipboard.test.%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	defer source.close()

	logical := "שלום"
	visual, mapping, _ := shape.ShapeRunesDir([]rune(logical))
	store := copytext.New(1)
	store.Add([]rune(logical), visual, mapping)
	if err := source.testExternalCopy("unrelated text"); err != nil {
		t.Fatal(err)
	}

	w := watcher{source: source, restore: store.Restore}
	if err := w.initialize(); err != nil {
		t.Fatal(err)
	}
	if err := source.testExternalCopy(string(visual)); err != nil {
		t.Fatal(err)
	}
	seen, err := source.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	read, current, err := source.textIfCurrent(seen.changeCount)
	if err != nil || !current || !read.hasText {
		t.Fatalf("named plain-text read before watcher: count=%d current=%v hasText=%v text=%q err=%v", seen.changeCount, current, read.hasText, read.text, err)
	}
	if err := w.poll(); err != nil {
		t.Fatal(err)
	}
	after, err := source.snapshot()
	if err != nil {
		t.Fatal(err)
	}
	got, err := source.testText()
	if err != nil || got != logical || after.changeCount <= seen.changeCount {
		t.Fatalf("roundtrip text=%q count=%d previous=%d err=%v", got, after.changeCount, seen.changeCount, err)
	}
}

func TestNamedPasteboardSkipsFileClipboard(t *testing.T) {
	source, err := newNamedNativeSource(fmt.Sprintf("rtlwrap.clipboard.file-test.%d", time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	defer source.close()
	w := watcher{source: source, restore: func(string) (string, bool) { t.Fatal("file clipboard reached restore"); return "", false }}
	if err := w.initialize(); err != nil {
		t.Fatal(err)
	}
	if err := source.testExternalFile(); err != nil {
		t.Fatal(err)
	}
	if err := w.poll(); err != nil {
		t.Fatal(err)
	}
}
