# Limitations & honest scope

rtlwrap is a PTY wrapper that **enables correct RTL shaping and bidirectional
rendering for terminal applications that don't support it.** It is not a claim
that RTL renders perfectly everywhere. This document lists what it does not do,
so the project's promises stay defensible.

## Rendering — not fixable by rtlwrap

- **Letters have a small gap between them (not seamlessly cursive).**
  Arabic/Persian is a cursive, *proportional* script; a terminal is a
  *fixed-cell monospace grid*. rtlwrap emits the correct joined presentation
  forms (initial/medial/final/isolated), but each glyph still gets a whole
  cell of advance, so joined letters sit adjacent rather than flowing into one
  another. This is a font + terminal rendering limit, not something a
  byte-stream transformer can close. The grid renderer does **not** fix it
  either — it's a different layer.
  - *Improves with:* a terminal with good Arabic cell handling (Kitty, foot)
    and a font designed for it (Vazirmatn, Noto Naskh Arabic).
- **Some breaks between letters are correct.** ا د ذ ر ز و and friends never
  join to their left. A gap after them is proper Persian, not a bug.

## Redraw-heavy / interactive apps

rtlwrap feeds the child's output into a virtual terminal and re-emits each row
with its RTL runs reshaped and colors remapped onto the reordered cells. That
grid renderer drives both screens:

- **The normal screen** (`rtlwrap cat file.he`, `rtlwrap git log`, program logs,
  and interactive apps that repaint in place — Claude Code, Codex CLI, spinners,
  status lines). Rows that scroll off the top are pushed through the terminal's
  top row as they leave, so they land in the real scrollback already shaped.
- **The alternate screen** (vim, less, full-screen TUIs), reshaped against the
  live grid the same way.

The scrolling line-by-line renderer is now only the fallback for a non-TTY
stdout (`rtlwrap cmd | tee log`), where there is no grid to repaint.

**Remaining gaps:**

- **Cursor position in a reshaped RTL row** can sit one cell off per lam-alef
  ligature to its left (the zero-width filler is stripped from display but not
  yet subtracted from the cursor column).
- **Anchoring to the current line** relies on a cursor-position report
  (`ESC [ 6 n`) at startup. A terminal that does not answer within 250 ms leaves
  rtlwrap anchored at the top row, and output can then paint over lines the
  shell had already printed.
- **On resize**, rtlwrap takes the reflowed grid as already on screen instead of
  repainting it. Apps repaint themselves on SIGWINCH; one that does not can be
  left showing stale rows until its next redraw.
- **Escape sequences the grid cannot reproduce** (window title, OSC 52
  clipboard, hyperlinks, mouse reporting, focus reporting, bracketed paste,
  cursor shape) are forwarded to the terminal verbatim. Anything outside that
  list that a real terminal would act on — and rtlwrap's virtual terminal does
  not implement — is still dropped.
- **Symptom in the non-TTY fallback:** a partial RTL line with no trailing
  newline is held until the next read or EOF (so a word split across two PTY
  reads still joins as one line).

## Mid-line color inside an RTL run

- A color change **inside** a single Persian word splits shaping at the escape
  sequence, so that word may shape per-segment in the non-TTY fallback. On the
  grid renderer the row is reshaped from cells, so mid-word color is fine.

## Unicode edge cases

- **Emoji, ZWJ sequences, combining marks** are preserved (not dropped or
  reordered into the RTL run), but exhaustive correctness across every emoji
  ZWJ family and combining-mark stack is not guaranteed. Covered by tests for
  the common cases only.

## Terminals that do their own bidi — do NOT wrap them

rtlwrap emits text **already reordered** to visual order with joined
presentation forms. That is correct for a terminal that prints cells
left-to-right as-is. A terminal that runs its **own** Unicode bidi + Arabic
shaping will reorder rtlwrap's already-visual output a **second** time, and the
two reorderings scramble the result (letters and words out of order, joins
broken).

- **Known bidi-aware: Zed's embedded terminal** (it reuses the code editor's
  RTL-aware text pipeline). Some iTerm2 / Kitty configurations also attempt it.
  In these, run the program **directly** (e.g. plain `claude`) — the terminal
  already shapes RTL, so rtlwrap is not needed and actively hurts.
- **Known dumb (use rtlwrap here): Ghostty, Warp, foot, xterm, GNOME Terminal,
  Konsole, VS Code's terminal.** These do no bidi, so rtlwrap's visual output
  renders correctly.
- There is **no reliable auto-detection** of a terminal's bidi support, so
  rtlwrap cannot disable itself. Pick the wrapper based on the terminal.
