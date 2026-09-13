//go:build !darwin || !cgo

package inputlang

// Current returns an empty label when macOS Carbon input-source APIs are
// unavailable.
func Current() string { return "" }
