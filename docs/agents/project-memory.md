# Project memory

Durable repository knowledge only. Re-check current code, config, and dirty state before treating
anything here as current behavior.

Capture newly learned durable repository facts here during the same task, or in a focused document
under `docs/agents/` linked from `AGENTS.md`. External services need explicit coverage: purpose,
integration points, auth/config key names (never values), identifiers/endpoints, required
permissions, operational constraints and failure modes, plus verification source/date. Keep transient
live state in logs or reports and never store secrets, credentials, or private content.

See also `docs/limitations.md` for rendering/copy/paste behavioral boundaries — do not duplicate that
document here.

## Architecture / file locations

- Keyboard-language detection lives in `internal/inputlang/` (`native_darwin.m`/`native_darwin.go`
  on macOS, cgo, links `-framework Carbon`). It reads `TISCopyCurrentKeyboardInputSource()` on a
  Core Foundation run loop (`CFRunLoopRunInMode`) rather than polling from a background thread, so
  input-source-change notifications are actually processed. Verified against source, per Codex
  memory 2026-09-13.
- `Engine.render()` in `internal/termstate/engine.go` (line ~176) owns the terminal caret and the
  keyboard-language badge row; it is the place to change badge glyphs/position without touching
  caret mapping or language detection. Verified against source, per Codex memory 2026-09-14.
- Warp-specific bidi-mirroring compensation lives in `internal/shape/shape.go`, gated on
  `os.Getenv("TERM_PROGRAM") == "WarpTerminal"`: it clears FriBidi's `ShapeMirroring` flag before
  calling `fribidi.Shape(...)` so Warp's own punctuation-mirroring renderer isn't double-mirrored.
  Verified against source (shape.go:89, 143), per Codex memory 2026-09-10.

## Operational constraints

- Mouse-reporting modes pass through `internal/wrap/dispatch.go`, while
  `internal/wrap/wrap.go` forwards stdin directly to the child PTY. Mouse coordinates
  are not mapped back from rendered RTL cells to logical cells. This can affect
  application-managed mouse interaction; native terminal selection is a separate
  host-terminal behavior. Verified from source on 2026-09-17; no live selection
  reproduction was established by this source check.

- Keyboard arrow navigation: `internal/wrap/input.go` streams stdin via `forwardInput()`.
  On macOS with cgo, when `inputlang.Current() == "he"`, Left and Right arrow keys
  (standard CSI, SS3, modified CSI, and word navigation `\x1bb`/`\x1bf`) are automatically
  swapped to preserve visual arrow navigation in agent CLIs and shells. Can be disabled
  via `--no-swap-arrows` / `RTLWRAP_NO_SWAP_ARROWS=1` or forced via `--swap-arrows` /
  `RTLWRAP_SWAP_ARROWS=1`. Verified from source and tests on 2026-09-18.


- In a macOS sandbox where the default Go build cache is blocked (`operation not permitted`), use
  `GOCACHE=/private/tmp/rtlwrap-go-cache go test -race ./...` instead of the plain test command.
  Per Codex memory 2026-09-02/2026-09-13 (not present in README/AGENTS.md).
- An already-running wrapped process is not updated by rebuilding the binary; restart the wrapper
  (e.g. `rtl codex`) before treating a new build as live behavior. Per Codex memory 2026-09-10/09-13.
