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

Nothing else is modified.
