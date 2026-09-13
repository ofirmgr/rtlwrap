package copytext

import (
	"sync"
	"testing"

	"github.com/Har2yQn78/rtlwrap/internal/shape"
)

func addShaped(t *testing.T, s *Store, logical string) string {
	t.Helper()
	visual, mapping, _ := shape.ShapeRunesDir([]rune(logical))
	s.Add([]rune(logical), visual, mapping)
	return string(visual)
}

func TestRestoreShapedHebrewMixedAndNumbers(t *testing.T) {
	s := New(8)
	for _, logical := range []string{"שלום עולם", "קוד: git 123"} {
		visual := addShaped(t, s, logical)
		if got, ok := s.Restore(visual); !ok || got != logical {
			t.Fatalf("Restore(%q) = (%q, %v), want (%q, true)", visual, got, ok, logical)
		}
	}
}

func TestRestorePartialVisualSelection(t *testing.T) {
	s := New(4)
	visual := addShaped(t, s, "שלום עולם")
	selected := string([]rune(visual)[:3]) // visual "םלו" maps to logical "ולם".
	if got, ok := s.Restore(selected); !ok || got != "ולם" {
		t.Fatalf("Restore(%q) = (%q, %v), want (\"ולם\", true)", selected, got, ok)
	}
}

func TestRestoreMultilinePreservesEndingsBlankLinesAndPadding(t *testing.T) {
	s := New(8)
	first := "  שלום  "
	second := "קוד: git 123"
	input := addShaped(t, s, first) + "\r\n\n" + addShaped(t, s, second) + "\n"
	want := first + "\r\n\n" + second + "\n"
	if got, ok := s.Restore(input); !ok || got != want {
		t.Fatalf("Restore(%q) = (%q, %v), want (%q, true)", input, got, ok, want)
	}
}

func TestRestoreMultilineAllowsUnchangedEnglishAndGridPadding(t *testing.T) {
	s := New(8)
	hebrew := "שלום"
	visual := addShaped(t, s, hebrew)
	english := addShaped(t, s, "report 123")
	input := "   " + visual + "  \n" + english + "\n"
	want := "   " + hebrew + "  \nreport 123\n"
	if got, ok := s.Restore(input); !ok || got != want {
		t.Fatalf("Restore(%q) = (%q, %v), want (%q, true)", input, got, ok, want)
	}
}

func TestRestoreRejectsUnknownAlreadyLogicalAndAmbiguous(t *testing.T) {
	s := New(8)
	if got, ok := s.Restore("missing"); ok || got != "missing" {
		t.Fatalf("unknown = (%q, %v)", got, ok)
	}
	s.Add([]rune("ab"), []rune("אב"), []int{0, 1})
	// "אב" is a valid visual row, but a separate row says it is already logical.
	s.Add([]rune("אב"), []rune("בא"), []int{1, 0})
	if got, ok := s.Restore("אב"); ok || got != "אב" {
		t.Fatalf("known logical collision = (%q, %v)", got, ok)
	}
	if got, ok := s.Restore("  אב  "); ok || got != "  אב  " {
		t.Fatalf("padded known logical collision = (%q, %v)", got, ok)
	}

	ambiguous := New(8)
	ambiguous.Add([]rune("one"), []rune("xyz"), []int{0, 1, 2})
	ambiguous.Add([]rune("two"), []rune("xyz"), []int{0, 1, 2})
	if got, ok := ambiguous.Restore("xyz"); ok || got != "xyz" {
		t.Fatalf("ambiguous = (%q, %v)", got, ok)
	}

	padded := New(8)
	padded.Add([]rune("a  b"), []rune(" אב "), []int{0, 1, 2, 3})
	padded.Add([]rune("cd"), []rune("אב"), []int{0, 1})
	if got, ok := padded.Restore(" אב "); ok || got != " אב " {
		t.Fatalf("exact and padded candidates = (%q, %v)", got, ok)
	}
}

func TestStoreSkipsUnsafeRowsAndEvictsOldRows(t *testing.T) {
	s := New(1)
	s.Add([]rune("ab"), []rune{'a', '\ufeff'}, []int{0, 1})
	if got, ok := s.Restore("a\ufeff"); ok || got != "a\ufeff" {
		t.Fatalf("filler row = (%q, %v)", got, ok)
	}
	s.Add([]rune("ab"), []rune("xy"), []int{0, 1})
	s.Add([]rune("cd"), []rune("zw"), []int{0, 1})
	if got, ok := s.Restore("xy"); ok || got != "xy" {
		t.Fatalf("evicted row = (%q, %v)", got, ok)
	}
	if got, ok := s.Restore("zw"); !ok || got != "cd" {
		t.Fatalf("new row = (%q, %v)", got, ok)
	}
}

func TestStoreRefreshingDuplicateKeepsItRecent(t *testing.T) {
	s := New(2)
	s.Add([]rune("ab"), []rune("xy"), []int{0, 1})
	s.Add([]rune("cd"), []rune("zw"), []int{0, 1})
	s.Add([]rune("ab"), []rune("xy"), []int{0, 1}) // refresh; do not consume a new slot
	s.Add([]rune("ef"), []rune("uv"), []int{0, 1})
	if got, ok := s.Restore("zw"); ok || got != "zw" {
		t.Fatalf("old row after duplicate refresh = (%q, %v)", got, ok)
	}
	if got, ok := s.Restore("xy"); !ok || got != "ab" {
		t.Fatalf("refreshed row = (%q, %v)", got, ok)
	}
}

func TestStoreConcurrentAddAndRestore(t *testing.T) {
	s := New(16)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				s.Add([]rune("abc"), []rune("cba"), []int{2, 1, 0})
				_, _ = s.Restore("cba")
			}
		}()
	}
	wg.Wait()
}
