package wrap

import (
	"bytes"
	"strings"
	"testing"
)

func TestLanguageSwitchWhileIdleAndScreenCleanup(t *testing.T) {
	var out bytes.Buffer
	d := newInlineDispatcher(&out, 20, 4, 0)
	if err := d.setLanguage("he"); err != nil {
		t.Fatal(err)
	}
	_, _ = d.Write([]byte("header\r\nprompt"))
	if !strings.Contains(out.String(), "ʰᵉ") {
		t.Fatal("main label missing")
	}
	out.Reset()
	if err := d.setLanguage("en"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "ᵉⁿ") {
		t.Fatal("idle language switch not painted")
	}
	out.Reset()
	_, _ = d.Write([]byte("\x1b[?1049h\x1b[2;2H"))
	switchAt := strings.Index(out.String(), "\x1b[?1049h")
	if switchAt < 0 || !strings.Contains(out.String()[:switchAt], "header") {
		t.Fatal("main label not cleared before alt switch")
	}
	if !strings.Contains(out.String()[switchAt:], "ᵉⁿ") {
		t.Fatal("alt language not inherited")
	}
	out.Reset()
	_, _ = d.Write([]byte("\x1b[?1049l"))
	if !strings.Contains(out.String(), "ᵉⁿ") {
		t.Fatal("main label not restored")
	}
	out.Reset()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "header") || strings.ContainsAny(out.String(), "ʰᵉⁿ") {
		t.Fatal("exit did not remove label")
	}
}

func TestLanguageDoesNotChangePipeOutput(t *testing.T) {
	var out bytes.Buffer
	d := newDispatcher(&out, 20, 4)
	_ = d.setLanguage("he")
	_, _ = d.Write([]byte("hello\n"))
	_ = d.Close()
	if strings.ContainsAny(out.String(), "ʰᵉⁿ") {
		t.Fatal("language label in pipe output")
	}
}
