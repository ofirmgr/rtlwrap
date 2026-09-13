//go:build darwin && cgo

package main

import (
	"os"
	"runtime"

	"github.com/Har2yQn78/rtlwrap/internal/inputlang"
	"golang.org/x/term"
)

func init() {
	// Go runs package initialization on the original process thread. Keep main
	// there so macOS can deliver distributed keyboard-source notifications.
	runtime.LockOSThread()
}

func runWithInputEvents(work func() error) error {
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return work()
	}
	return inputlang.Run(work)
}
