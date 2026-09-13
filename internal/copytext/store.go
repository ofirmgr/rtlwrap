// Package copytext restores logical-order text from terminal rows previously
// rendered by shape. It is deliberately row based: a multi-line clipboard
// selection is restored line by line and never used to infer an order between
// source paragraphs.
package copytext

import (
	"sort"
	"strings"
	"sync"
)

const (
	maxRows     = 512
	maxRowRunes = 8192
	filler      = '\ufeff'
)

// Store retains a small recent history of safely invertible rendered rows.
// Its zero value is usable but retains no rows; construct it with New.
type Store struct {
	mu    sync.RWMutex
	limit int
	rows  []row // oldest first
}

type row struct {
	logical         []rune
	visual          []rune
	visualToLogical []int
}

// New returns a Store retaining at most limit recent distinct rows. Very large
// limits are capped so a hostile or unusually long terminal stream cannot turn
// clipboard restoration into unbounded memory retention.
func New(limit int) *Store {
	if limit < 0 {
		limit = 0
	}
	if limit > maxRows {
		limit = maxRows
	}
	return &Store{limit: limit}
}

// Add records one logical/rendered row when its map is a safe one-to-one
// inversion. Rows containing the renderer's FEFF ligature fillers are skipped:
// those fillers are deliberately not rendered as text and therefore cannot be
// matched safely against clipboard contents.
func (s *Store) Add(logical []rune, visual []rune, visualToLogical []int) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.limit == 0 {
		return
	}
	for i := range s.rows {
		if sameRowInput(s.rows[i], logical, visual, visualToLogical) {
			duplicate := s.rows[i]
			copy(s.rows[i:], s.rows[i+1:])
			s.rows[len(s.rows)-1] = duplicate
			return
		}
	}
	// Check for an existing row first. The renderer calls Add for every row on
	// every frame, so the usual duplicate path avoids map validation and copies.
	if !validRow(logical, visual, visualToLogical) {
		return
	}
	r := row{
		logical:         append([]rune(nil), logical...),
		visual:          append([]rune(nil), visual...),
		visualToLogical: append([]int(nil), visualToLogical...),
	}
	if len(s.rows) == s.limit {
		copy(s.rows, s.rows[1:])
		s.rows[len(s.rows)-1] = r
		return
	}
	s.rows = append(s.rows, r)
}

// Restore restores each nonblank clipboard line only when every exact visual
// substring match has the same contiguous logical source interval. It preserves
// the original line endings, blank lines, trailing newline, and selected
// padding. If a selection is already known to occur in logical text, it is
// treated as ambiguous rather than risking corruption from a visual collision.
func (s *Store) Restore(text string) (string, bool) {
	if s == nil || text == "" {
		return text, false
	}
	s.mu.RLock()
	rows := append([]row(nil), s.rows...)
	s.mu.RUnlock()
	if len(rows) == 0 {
		return text, false
	}

	var out strings.Builder
	out.Grow(len(text))
	for rest := text; ; {
		line, ending, tail := splitLine(rest)
		if !blank(line) {
			restored, ok := restorePaddedLine(line, rows)
			if !ok {
				return text, false
			}
			// An unchanged LTR/number line is safe inside a mixed multiline
			// selection. A changed candidate that is also known logical text is
			// a collision, so leave the whole clipboard untouched.
			if restored != line && (knownLogical(line, rows) || knownLogical(trimExternalSpaces(line), rows)) {
				return text, false
			}
			out.WriteString(restored)
		} else {
			out.WriteString(line)
		}
		out.WriteString(ending)
		if tail == "" {
			break
		}
		rest = tail
	}
	result := out.String()
	if result == text {
		return text, false
	}
	return result, true
}

func validRow(logical, visual []rune, m []int) bool {
	if len(logical) == 0 || len(logical) != len(visual) || len(visual) != len(m) || len(logical) > maxRowRunes {
		return false
	}
	seen := make([]bool, len(logical))
	for i, logicalIndex := range m {
		if visual[i] == filler || logicalIndex < 0 || logicalIndex >= len(logical) || seen[logicalIndex] {
			return false
		}
		seen[logicalIndex] = true
	}
	return true
}

func sameRowInput(r row, logical, visual []rune, m []int) bool {
	return sameRunes(r.logical, logical) && sameRunes(r.visual, visual) && sameInts(r.visualToLogical, m)
}

func sameRunes(a, b []rune) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sameInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func splitLine(text string) (line, ending, tail string) {
	i := strings.IndexByte(text, '\n')
	if i < 0 {
		return text, "", ""
	}
	line, ending = text[:i], "\n"
	if strings.HasSuffix(line, "\r") {
		line = line[:len(line)-1]
		ending = "\r\n"
	}
	return line, ending, text[i+1:]
}

func blank(line string) bool { return strings.TrimSpace(line) == "" }

func knownLogical(selected string, rows []row) bool {
	needle := []rune(selected)
	for _, r := range rows {
		if containsRunes(r.logical, needle) {
			return true
		}
	}
	return false
}

// restoreLine distinguishes no rendered occurrence from an unsafe occurrence;
// callers must not turn the latter into a padded fallback match.
func restoreLine(selected []rune, rows []row) (string, bool, bool) {
	var candidate string
	found := false
	for _, r := range rows {
		for start := 0; start+len(selected) <= len(r.visual); start++ {
			if !sameRunes(r.visual[start:start+len(selected)], selected) {
				continue
			}
			logical, ok := mappedInterval(r, start, len(selected))
			if !ok {
				// The bytes do match a rendered row, but this particular selection
				// cannot describe one logical interval. Do not let another row make
				// that unsafe occurrence appear valid.
				return "", true, false
			}
			if !found {
				candidate, found = logical, true
			} else if candidate != logical {
				return "", true, false
			}
		}
	}
	return candidate, found, found
}

// restorePaddedLine first prefers an exact rendered substring. A terminal
// selection may include grid padding outside the row's used cells, though, so
// if exact matching fails it restores a core with only external ASCII spaces
// removed and puts those spaces back verbatim.
func restorePaddedLine(line string, rows []row) (string, bool) {
	exact, exactFound, exactOK := restoreLine([]rune(line), rows)
	if exactFound && !exactOK {
		return "", false
	}
	core := trimExternalSpaces(line)
	if core == line {
		return exact, exactFound && exactOK
	}
	if core == "" {
		return "", false
	}
	paddingStart := len(line) - len(strings.TrimLeft(line, " "))
	paddingEnd := len(line) - len(strings.TrimRight(line, " "))
	inner, innerFound, innerOK := restoreLine([]rune(core), rows)
	if innerFound && !innerOK {
		return "", false
	}
	if innerFound {
		padded := line[:paddingStart] + inner + line[len(line)-paddingEnd:]
		if exactFound && exact != padded {
			return "", false
		}
		return padded, true
	}
	return exact, exactFound && exactOK
}

func trimExternalSpaces(line string) string {
	start, end := 0, len(line)
	for start < end && line[start] == ' ' {
		start++
	}
	for end > start && line[end-1] == ' ' {
		end--
	}
	return line[start:end]
}

func mappedInterval(r row, start, length int) (string, bool) {
	indices := append([]int(nil), r.visualToLogical[start:start+length]...)
	sort.Ints(indices)
	for i := 1; i < len(indices); i++ {
		if indices[i] != indices[0]+i {
			return "", false
		}
	}
	return string(r.logical[indices[0] : indices[len(indices)-1]+1]), true
}

func containsRunes(haystack, needle []rune) bool {
	if len(needle) == 0 || len(needle) > len(haystack) {
		return false
	}
	for start := 0; start+len(needle) <= len(haystack); start++ {
		if sameRunes(haystack[start:start+len(needle)], needle) {
			return true
		}
	}
	return false
}
