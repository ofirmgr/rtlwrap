# rtlwrap

Correct RTL (Persian, Arabic, Hebrew) text in any terminal application.

No patches. No plugins. No changes to the application or terminal.

Many terminal applications display Persian, Arabic, and Hebrew backwards or with broken letter joining because most terminals do not implement Unicode bidirectional rendering.

rtlwrap fixes this by sitting between the application and your terminal. It works without modifying either one.

Just run:

rtlwrap <command>

and the output is rendered correctly.

## Demo

![rtlwrap before and after](pics/TestPic.png)

**Left:** Claude Code running through `rtlwrap` — Persian is displayed in the correct right-to-left order with proper Arabic letter joining.

**Right:** The same output without `rtlwrap` — text appears in logical order, making Persian display backwards and unjoined.

## Why

Many terminal programs emit RTL text in logical order and assume the terminal
will reorder it. Terminals that do no bidi processing then display it backwards
and unjoined. rtlwrap does that reordering and shaping in transit, so those
programs read correctly in a plain terminal.

## Platform support

- Linux: supported.
- macOS: supported.
- Windows: not tested yet. It relies on a Unix PTY, so it is unlikely to work
  as-is on Windows.

## Install

### With Go

```sh
go install github.com/Har2yQn78/rtlwrap/cmd/rtlwrap@latest
```

This puts the `rtlwrap` binary in `$(go env GOPATH)/bin` (add it to your `PATH`).

### Linux package (.deb / .rpm)

Download the package for your distro from the
[Releases](https://github.com/Har2yQn78/rtlwrap/releases) page. It installs the
binary to `/usr/bin/rtlwrap`.

```sh
# Debian / Ubuntu
sudo dpkg -i rtlwrap_*_linux_amd64.deb
# Fedora / RHEL
sudo rpm -i rtlwrap_*_linux_amd64.rpm
```

### Prebuilt binary

Or download the `tar.gz` for your OS and architecture from the
[Releases](https://github.com/Har2yQn78/rtlwrap/releases) page, extract it, and
move the `rtlwrap` binary somewhere on your `PATH`.

### From source

```sh
git clone https://github.com/Har2yQn78/rtlwrap.git
cd rtlwrap
go build -o rtlwrap ./cmd/rtlwrap
```

## Usage

Put `rtlwrap` in front of any command you want RTL-corrected:

```sh
rtlwrap <program> [args...]
```

Examples:

```sh
rtlwrap claude
rtlwrap codex
rtlwrap antigravity
```

Or, if you built the binary locally and did not put it on your `PATH`:

```sh
./rtlwrap claude
./rtlwrap codex
./rtlwrap antigravity
```

Everything the program prints is reshaped; everything you type is sent through
untouched.

### Copy original Hebrew text on macOS

Terminal selection normally copies rtlwrap's visual character order, which
appears reversed when pasted into an application with native RTL support.
Local macOS builds with cgo enabled restore original text automatically:

```sh
CGO_ENABLED=1 go build -o rtlwrap ./cmd/rtlwrap
./rtlwrap <program> [args...]
```

Select and copy normally. While the hosting terminal is in the foreground,
rtlwrap watches for new plain-text clipboard contents and matches selections
against its recent rendered rows. It restores the corresponding original
character order, preserving English and numbers instead of reversing them.
The existing clipboard is ignored at startup; unknown or ambiguous selections
are left unchanged. Watching stops when the wrapped program exits.

This feature reads and replaces matching system clipboard text. Its history is
bounded and kept only in memory. It requires macOS clipboard access; use
`--no-restore-copy` to disable it. If restoration cannot start, rtlwrap reports
the reason and still runs the program. Detection is asynchronous, so an immediate paste can happen before
restoration. See [copy limitations](docs/limitations.md#copy-and-paste).
The current release configuration disables cgo, so those prebuilt binaries do
not include this feature.

### Input-language label on macOS

Local builds with cgo enabled show a compact `he` (Hebrew) or `en` (English)
on the terminal row above the blinking text caret. The label follows the
visually reordered caret and checks a refreshed keyboard-language snapshot every
150 ms, including while the child is idle. Restart the wrapped program after
rebuilding `rtlwrap` to use it.

The macOS command keeps its original main thread running a Core Foundation event
loop, which delivers keyboard-source changes. `internal/inputlang` reads Carbon
on that thread and publishes a snapshot for the terminal renderer. Replacing
this with background Carbon polling alone can leave the startup language cached.
When validating this integration, switch English/Hebrew/English while one wrapped
process stays running and idle; checking only its startup label misses stale
input-source state.

The label uses baseline lowercase characters in two terminal cells to keep it
close to the caret below. Terminal output cannot set a floating label's font size
or position text between rows. It temporarily covers those cells and
restores the underlying row when it moves or disappears. It hides for other
languages, hidden cursors, the top screen row, and pre-existing shell rows whose
contents rtlwrap cannot restore. It is removed before scrolling, screen changes,
and exit, and is excluded from the original-text copy history. A terminal's own
selection can still include a currently visible label. Builds without cgo and
non-macOS builds do not show it.

After a terminal resize, a reflowed label can remain until the child repaints.
rtlwrap discards its old label coordinates to avoid restoring text into the
wrong row of the resized terminal.

## How it works

rtlwrap runs the program on a pseudo-terminal and feeds its output into a
virtual terminal, then re-emits each screen row with its RTL runs reshaped and
colors carried onto the reordered cells. Because the reshaping works from the
screen as it actually stands, it covers both:

- Scrolling / static output (for example `rtlwrap cat file.fa`,
  `rtlwrap git log`). Rows that scroll off the top go into your terminal's real
  scrollback, already shaped.
- Programs that repaint in place — a prompt being typed into, a status line, a
  spinner, and full-screen programs on the alternate screen. Text is reshaped
  per row, so RTL you type appears in the right order as you type it, not only
  after the program redraws the whole line.

Piping the output somewhere that is not a terminal (`rtlwrap cmd | tee log`)
falls back to shaping line by line as it streams.

Escape sequences are handled, not guessed at: those that move the cursor or
paint cells drive the virtual screen, and the ones a virtual screen cannot
reproduce — window title, clipboard, hyperlinks, mouse reporting, bracketed
paste, cursor shape — are passed to your terminal verbatim.

## Terminal compatibility

rtlwrap is for terminals that do no bidi of their own. Use it with terminals
like Ghostty, Warp, Kitty, foot, xterm, GNOME Terminal, Konsole, or VS Code's
terminal.

Do not use it with a terminal that already does its own bidi and Arabic shaping
(for example Zed's embedded terminal). There, run the program directly, without
rtlwrap. Wrapping a bidi-aware terminal reorders the text twice and scrambles
it. See [docs/limitations.md](docs/limitations.md) for the full list of known
limitations, including the monospace cell gap between joined letters.

## Limitations

rtlwrap has real, documented limits: the monospace cell gap between joined
letters, terminals that do their own bidi (do not wrap those), redraw behavior
in some programs, and Unicode edge cases. Please read
[docs/limitations.md](docs/limitations.md) before filing an issue, so you know
what is a bug and what is a known boundary.

## Contributing

If rtlwrap helped you, please leave a star. It is the simplest way to show the
project is useful and worth maintaining.

Contributions are very welcome. Bug reports, terminal compatibility notes,
better shaping coverage, and Windows support are all wanted. Open an issue to
discuss a change, or send a pull request. Please run `go vet ./...` and
`go test ./...` before submitting.

## License

MIT. See [LICENSE](LICENSE).
