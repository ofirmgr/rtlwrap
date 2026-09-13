//go:build darwin && cgo

package inputlang

/*
#cgo LDFLAGS: -framework Carbon
#include <stdlib.h>

char *rtl_current_input_language(void);
int rtl_input_language_main_thread(void);
void rtl_input_language_process_events(void);
*/
import "C"

import (
	"errors"
	"sync/atomic"
	"time"
	"unsafe"
)

var languageSnapshot atomic.Value

// Current reads the snapshot maintained by Run on the macOS main thread.
func Current() string {
	if language := languageSnapshot.Load(); language != nil {
		return language.(string)
	}
	return ""
}

func refresh() {
	language := C.rtl_current_input_language()
	if language == nil {
		languageSnapshot.Store("")
		return
	}
	defer C.free(unsafe.Pointer(language))
	languageSnapshot.Store(normalize(C.GoString(language)))
}

// Run keeps macOS input-source notifications flowing while work runs in a
// goroutine. The caller must be locked to the process's original main thread.
// TIS reads stay on that thread; Current is safe for polling from other threads.
func Run(work func() error) error {
	if C.rtl_input_language_main_thread() == 0 {
		return errors.New("input language event loop requires the macOS main thread")
	}
	refresh()
	done := make(chan error, 1)
	go func() { done <- work() }()
	for {
		select {
		case err := <-done:
			return err
		default:
		}
		started := time.Now()
		C.rtl_input_language_process_events()
		refresh()
		// A headless session may have no run-loop sources. Avoid spinning.
		if remaining := 50*time.Millisecond - time.Since(started); remaining > 0 {
			time.Sleep(remaining)
		}
	}
}
