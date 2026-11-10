package termstate

// Caret is a rendered caret: the cursor row's logical insertion boundaries
// mapped to visual columns (used text only) and the cursor's logical column.
// A nil *Caret means unknown (hidden cursor, no grid).
type Caret struct {
	Map []int
	X   int
}

// Direction reports the visual direction of a logical-forward move at the
// caret, or of the move that reached it at the end of the text: -1 left (RTL
// text), +1 right, 0 unknown.
func (c *Caret) Direction() int {
	if c == nil {
		return 0
	}
	delta := 0
	if c.X >= 0 && c.X+1 < len(c.Map) {
		delta = c.Map[c.X+1] - c.Map[c.X]
	}
	if delta == 0 && c.X > 0 && c.X < len(c.Map) {
		delta = c.Map[c.X] - c.Map[c.X-1]
	}
	switch {
	case delta < 0:
		return -1
	case delta > 0:
		return 1
	}
	return 0
}

// Step returns the logical move, negative for backward, that reaches the
// boundary visually nearest the caret on its left (dx < 0) or right (dx > 0).
// In mixed-direction text this can be several cells: a logical step across a
// run boundary jumps visually. ok is false outside the text or at that edge.
func (c *Caret) Step(dx int) (n int, ok bool) {
	if c == nil || c.X < 0 || c.X >= len(c.Map) {
		return 0, false
	}
	cv := c.Map[c.X]
	best, bestDist := -1, 0
	for j, v := range c.Map {
		d := (v - cv) * dx
		if j == c.X || d <= 0 {
			continue
		}
		if best < 0 || d < bestDist || d == bestDist && abs(j-c.X) < abs(best-c.X) {
			best, bestDist = j, d
		}
	}
	if best < 0 {
		return 0, false
	}
	return best - c.X, true
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
