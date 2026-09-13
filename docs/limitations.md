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

On the grid renderer a row whose resolved paragraph direction is RTL is
right-aligned (printed flush to the right edge), matching where a bidi-aware
renderer puts an RTL paragraph. A row that already reaches the right edge — a
full-width TUI box, for instance — has no room to shift and stays put, so text
inside such a box is still left-aligned within it. The non-TTY fallback does
not align at all.

During grid updates, cursor hiding, painting, final positioning, and restoration
are emitted in the same write. There is no quiet-period timer: continuous status
updates must not keep the cursor hidden between repaints. Application-requested
cursor hiding is preserved; shutdown restores the cursor immediately.

Synchronized-output markers (`CSI ?2026 h/l`) are forwarded to the host terminal.
This preserves redraw boundaries used by interactive applications such as Codex,
so a supporting terminal displays the completed frame rather than intermediate
RTL layouts from separate PTY reads. The host terminal must support synchronized
output for this protection to apply.

**Remaining gaps:**

- **Right alignment counts runes, not cells**, so a right-aligned row holding
  CJK or emoji ends one column short per wide rune (same missing width math as
  the cursor gap below).
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
  cursor shape, synchronized output) are forwarded to the terminal verbatim. Anything outside that
  list that a real terminal would act on — and rtlwrap's virtual terminal does
  not implement — is still dropped.
- **Symptom in the non-TTY fallback:** a partial RTL line with no trailing
  newline is held until the next read or EOF (so a word split across two PTY
  reads still joins as one line).

## Mid-line color inside an RTL run

- A color change **inside** a single Persian word splits shaping at the escape
  sequence, so that word may shape per-segment in the non-TTY fallback. On the
  grid renderer the row is reshaped from cells, so mid-word color is fine.

## Copy and paste

Ordinary terminal selection copies the visual character order emitted by
rtlwrap, not the original text. Pasting that into an RTL-aware application can
therefore display Hebrew backwards. Input pasted into rtlwrap is unchanged.

On macOS, clipboard restoration is enabled by default and restores matching selections
using original rendered-row text and its visual-to-logical mapping. It requires
a local cgo-enabled build and terminal stdin/stdout. It does not receive the
terminal's selection or Copy event: it polls clipboard changes while the
hosting terminal application is in the foreground.

The terminal must be identifiable in rtlwrap's process ancestry. Supported host
identifiers cover Terminal.app, iTerm2, Warp, Ghostty, Kitty, and Alacritty.
Detached multiplexers and other embedded terminals may not retain a supported
ancestor; in that case rtlwrap reports that restoration is unavailable and
still starts the child. `--no-restore-copy` disables the watcher. The explicit
`--restore-copy` flag remains available when restoration must be required;
that flag fails before starting the child if restoration cannot start.

- Only recently retained rows can be restored, including rows that entered
  scrollback. The cache holds at most 512 unique rows; older output expires.
- Partial selections must map to a contiguous original character range. Unknown
  text, conflicting matches, and selections that could already be original text
  are left unchanged. Multiline selections are matched row by row; they do not
  reconstruct logical paragraphs across terminal wrapping or rectangular
  selections.
- Rows with unsupported shaping mappings, including absorbed Arabic ligature
  fillers, are skipped. Existing wide-character and grapheme rendering limits
  also apply to copying.
- Foreground application checks do not identify the terminal tab or the source
  of a clipboard write. Identical text copied in another tab can match cached
  output. Run only one restoration-enabled wrapper per terminal application to
  avoid competing clipboard watchers.
- Polling and replacement are asynchronous. An immediate paste can beat
  restoration. Copying after the wrapped program exits is not corrected.
- Only a single item containing plain-text representations is eligible.
  Rich text, images, file lists, and other clipboard types are skipped.
  Clipboard access denied by macOS prevents restoration.

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
- **Warp punctuation exception:** Warp does not reorder RTL text, but its text
  renderer still mirrors paired punctuation from the already-visual row.
  rtlwrap pre-compensates using the bidi levels of that visual row. A blanket
  disabling of FriBidi mirroring breaks some brackets in LTR-base mixed lines.
- There is **no reliable auto-detection** of a terminal's bidi support, so
  rtlwrap cannot disable itself. Pick the wrapper based on the terminal.
