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
  Badge colors: `he` uses SGR `0;34` (blue), `en` uses SGR `0;31` (red), set in the label switch
  in `render()`; tests in `internal/termstate/language_test.go` and `internal/wrap/language_test.go`
  match the exact `heBadge`/`enBadge` byte sequences. Verified by `go test ./...`.
- Warp-specific bidi-mirroring compensation lives in `internal/shape/shape.go`, gated on
  `os.Getenv("TERM_PROGRAM") == "WarpTerminal"`: it clears FriBidi's `ShapeMirroring` flag before
  calling `fribidi.Shape(...)` so Warp's own punctuation-mirroring renderer isn't double-mirrored.
  Verified against source (shape.go:89, 143), per Codex memory 2026-09-10.
- `internal/vt10x` is a vendored fork; every local change is listed in `internal/vt10x/FORK.md`.
  Its CSI parser ignores sequences with `<`/`=`/`>` prefixes or intermediate bytes, because Claude
  Code's kitty-keyboard sequences (`CSI > 5 u`, `CSI < u`, `CSI ? u`) were otherwise run as DECRC
  and moved the virtual caret to a stale row. Verified by replaying captured Claude output, 2026-09-23.

## Operational constraints

- RTL base direction is the default for rows with strong RTL characters;
  English-only rows retain automatic direction. `--auto-direction` opts out;
  `--rtl` restores the default, with the last direction flag winning.
  Wiring: `!wrap.Options.AutoDirection` ->
  dispatcher -> normal/alternate engines and non-TTY pipe ->
  `shape.ShapeRunesLayoutWithRTL`. The option must be set before output starts.
  Physical rows remain independent; no paragraph reconstruction across soft wraps.
  Verified by mixed English-prefix/Hebrew regression tests across all three
  renderers on 2026-10-11.

- Mouse-reporting modes pass through `internal/wrap/dispatch.go`, while
  `internal/wrap/wrap.go` forwards stdin directly to the child PTY. Mouse coordinates
  are not mapped back from rendered RTL cells to logical cells. This can affect
  application-managed mouse interaction; native terminal selection is a separate
  host-terminal behavior. Verified from source on 2026-09-17; no live selection
  reproduction was established by this source check.

- Keyboard arrow navigation: `internal/wrap/input.go` streams stdin via `forwardInput()`
  through `makeTranslator`. Plain `\x1b[D`/`\x1b[C`/`\x1bOD`/`\x1bOC` become N logical
  arrows via `termstate.Caret.Step` (`internal/termstate/caret.go`): the move to
  the visually nearest boundary on the caret row. `Engine.Caret()` is an atomic
  snapshot set in `render()` (nil when the cursor is hidden); `dispatcher.caret()`
  picks the active grid. An unchanged snapshot pointer means no render since the
  last keys, so the translator continues from its predicted X (key repeat).
  Modified arrows and `\x1bb`/`\x1bf` swap when `Caret.Direction() < 0`; nil caret
  falls back to `inputlang.Current() == "he"`. Swapping alone (by layout or by row
  direction) cannot fix mixed Hebrew/English lines: a logical step across a run
  boundary jumps visually. Caret maps must not carry an identity tail past the
  used text, or the end of RTL text reads as LTR. In `internal/shape/shape.go`,
  `carets[li] = x` directly maps each character's logical column to its visual
  position, and the final boundary sits at `carets[last] ± 1` by embedding level,
  so run boundaries never skip cells. Claude Code 2.1.303 shows the real cursor
  at the logical insertion point. At visual boundaries (where `Step(dx)` has nowhere
  further to step), physical arrows are suppressed to avoid passing logical moves
  that jump across the line in mixed text. `--swap-arrows` / `RTLWRAP_SWAP_ARROWS=1` swaps
  unconditionally; `--no-swap-arrows` / `RTLWRAP_NO_SWAP_ARROWS=1` passes keys
  unchanged. Verified by `TestArrowNavigationVisualDirection` and by driving
  `rtlwrap claude` in a pty (monotonic contiguous caret across "❯ שלום hello עולם",
  cleanly stopping at both edges without jumping) on 2026-10-19.


- In a macOS sandbox where the default Go build cache is blocked (`operation not permitted`), use
  `GOCACHE=/private/tmp/rtlwrap-go-cache go test -race ./...` instead of the plain test command.
  Per Codex memory 2026-09-02/2026-09-13 (not present in README/AGENTS.md).
- An already-running wrapped process is not updated by rebuilding the binary; restart the wrapper
  (e.g. `rtl codex`) before treating a new build as live behavior. Per Codex memory 2026-09-10/09-13.
- On Ofir's Mac, `rtl` is `~/.local/bin/rtl -> <repo>/rtlwrap`, so source edits are not live until
  `CGO_ENABLED=1 go build -o rtlwrap ./cmd/rtlwrap` in the repo root. A stale binary (built
  2026-10-19) hid the badge-color change. Verified with `ls -la` and `strings` on 2026-11-10.
- The badge renders only when the row above the caret was painted by the wrapped program in this
  session (`e.owned[cur.Y-1]`); blank rows and pre-existing shell output stay unowned, so a test
  child must print text on the line above the caret. TIS works from a non-GUI agent shell
  (returned `en`), and a pty-driven run printing `first line\nabc` emitted `\x1b[0;31men\x1b[0m`.
  Running under `script` with no host TTY gives a 0x0 PTY and panics in `vt10x.(*State).setChar`
  (`index out of range [-1]`, `state.go:268`). Observed 2026-11-10; not fixed.
- `rtlwrap codex` (codex-cli 0.166.0) driven in a 100x30 Python pty with `TERM_PROGRAM=WarpTerminal`
  shows the badge on the row above the composer caret (`\x1b[0;31men`). Codex opens with an update
  prompt first; send Esc to reach the composer. Verified 2026-11-10; not a real Warp window.
- kiro-cli 3.1.0 never shows the hardware cursor: it sends only `\x1b[?25l` and draws a fake caret
  as a reverse-video cell (`› \x1b[7m \x1b[0m`). `kiro-cli --v2`, `--legacy-ui` and
  `chat --legacy-ui` behave the same. `Engine.drawnCaret()` in `internal/termstate/engine.go`
  places the badge above the only reverse-video cell on screen while the cursor is hidden (none if
  there are several). The drawn caret does not set the arrow-key `Caret` snapshot, so kiro-cli
  arrows still fall back to `inputlang.Current()`. Verified by `TestLanguageBadgeFollowsDrawnCaret`
  and a pty run of `rtlwrap kiro-cli` (badge above the caret, also after typing `שלום abc`) on
  2026-11-10.
