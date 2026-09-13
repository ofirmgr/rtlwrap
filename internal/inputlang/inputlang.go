// Package inputlang reports the active macOS keyboard input language.
package inputlang

import "strings"

// normalize maps an input-source language identifier to the labels understood
// by rtlwrap. Other languages are intentionally not reported.
func normalize(language string) string {
	language = strings.ToLower(strings.TrimSpace(language))
	if language == "" {
		return ""
	}
	language = strings.ReplaceAll(language, "_", "-")
	primary, _, _ := strings.Cut(language, "-")
	switch primary {
	case "he", "iw":
		return "he"
	case "en":
		return "en"
	default:
		return ""
	}
}
