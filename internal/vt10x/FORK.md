# Vendored fork of github.com/hinshun/vt10x

Source: `github.com/hinshun/vt10x@v0.0.0-20220301184237-5011da428d02` (MIT, see
`LICENSE`). Upstream is unmaintained and exposes no way to observe lines that
scroll off the top of the screen, which rtlwrap needs in order to keep the real
terminal's scrollback intact while it repaints the normal screen.

Local changes:

- `State.scrollOut` field and `State.SetScrollCallback` (reporting the glyph
  rows that leave the screen), added to the `Terminal` interface in `vt.go`.
- `State.scrollUpTop`, a wrapper around `scrollUp` that reports the number of
  lines leaving the screen. Called from the three sites that scroll from the
  region's top edge (`newline`, `IND`, `CSI S`). Line deletion (`DL`) and
  scrolls inside a region below row 0 still call `scrollUp` directly, so they
  do not report — a real terminal does not put those lines in scrollback either.
- `csiEscape.foreign`: CSI sequences with a `<`, `=` or `>` parameter prefix
  or an intermediate byte are ignored instead of being run as the plain command
  with the same final byte, and `CSI ? s` / `CSI ? u` / `CSI ? m` are ignored
  too. Upstream ran Claude Code's kitty keyboard sequences (`CSI > 5 u`,
  `CSI < u`, `CSI ? u`) as DECRC, moving the cursor to a stale saved row.

Nothing else is modified.
