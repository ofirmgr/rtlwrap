package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/Har2yQn78/rtlwrap/internal/wrap"
)

// version is set at build time via -ldflags "-X main.version=..." (GoReleaser).
var version = "dev"

func main() {
	args := os.Args[1:]
	options := wrap.Options{}
	for len(args) > 0 {
		consumed := true
		switch args[0] {
		case "--restore-copy":
			options.RestoreCopy = true
		case "--no-restore-copy":
			options.DisableCopy = true
		case "--swap-arrows", "--swap-hebrew-arrows":
			options.SwapArrows = true
		case "--no-swap-arrows", "--no-swap-hebrew-arrows":
			options.DisableSwapArrows = true
		default:
			consumed = false
		}
		if !consumed {
			break
		}
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: rtlwrap [--no-restore-copy] [--no-swap-arrows] [--swap-arrows] [--] <command> [args...]")
		os.Exit(2)
	}
	if args[0] == "--version" || args[0] == "-v" {
		fmt.Println("rtlwrap", version)
		return
	}
	err := runWithInputEvents(func() error { return wrap.RunWithOptions(args, options) })
	if err == nil {
		return
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		os.Exit(ee.ExitCode()) // propagate the child's exit code
	}
	fmt.Fprintln(os.Stderr, "rtlwrap:", err)
	os.Exit(1)
}
