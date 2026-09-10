package issue

import (
	"strings"
	"testing"
)

func TestSanitizeStripsTerminalControlSequences(t *testing.T) {
	input := "\x1b[31mremote\x1b[0m failed\r\nsecond line\ttabbed"
	got := Sanitize(input)
	if strings.ContainsAny(got, "\x1b\r\n\t") {
		t.Fatalf("control characters survived sanitizing: %q", got)
	}
	if got != "remote failed second line tabbed" {
		t.Fatalf("got %q", got)
	}
}

func TestSanitizeRedactsTokens(t *testing.T) {
	tests := []string{
		"authentication failed for gho_abcdefghijklmnopqrstuvwxyz012345",
		"github_pat_11ABCDEFG0abcdefghijklmnopqrstuvwxyz012345",
		"Authorization: Bearer abcdefghijklmnopqrstuvwxyz",
		"Token: gho_abcdefghijklmnopqrstuvwxyz012345",
	}
	for _, input := range tests {
		got := Sanitize(input)
		if !strings.Contains(got, "[REDACTED]") {
			t.Fatalf("token not redacted: %q -> %q", input, got)
		}
		if strings.Contains(got, "gho_abcdefghijklmnopqrstuvwxyz012345") || strings.Contains(got, "abcdefghijklmnopqrstuvwxyz") {
			t.Fatalf("token survived: %q -> %q", input, got)
		}
	}
}

func TestSanitizeBoundsLength(t *testing.T) {
	got := Sanitize(strings.Repeat("x", 1000))
	if len(got) != sanitizeMaxLength+len("...") {
		t.Fatalf("length=%d want %d", len(got), sanitizeMaxLength+len("..."))
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("missing truncation marker: %q", got)
	}
}
