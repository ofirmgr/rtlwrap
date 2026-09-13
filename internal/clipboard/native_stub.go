//go:build !darwin || !cgo

package clipboard

import "errors"

func newNativeSource() (*unsupportedSource, error) {
	return nil, errors.New("clipboard: macOS AppKit backend requires darwin with cgo enabled")
}

type unsupportedSource struct{}

func (*unsupportedSource) snapshot() (clipboardSample, error) {
	return clipboardSample{}, errors.New("clipboard: unsupported")
}
func (*unsupportedSource) foreground() (bool, error) {
	return false, errors.New("clipboard: unsupported")
}
func (*unsupportedSource) textIfCurrent(int64) (clipboardSample, bool, error) {
	return clipboardSample{}, false, errors.New("clipboard: unsupported")
}
func (*unsupportedSource) replaceIfCurrent(int64, string) (int64, bool, error) {
	return 0, false, errors.New("clipboard: unsupported")
}
func (*unsupportedSource) close() {}
