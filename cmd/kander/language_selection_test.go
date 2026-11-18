package main

import (
	"strings"
	"testing"
)

// --lang is read with the flag definitions of the command it is given to: the value of another
// flag that looks like it is not a language selection, in either spelling of that flag.
func TestLanguageSelectionFollowsTheFlagsOfTheCommand(t *testing.T) {
	spaced := runCLI(t, "--lang", "en", "notify", "--message", "--lang=ja")
	joined := runCLI(t, "--lang", "en", "notify", "--message=--lang=ja")
	if spaced.code != 2 || spaced.stderr != joined.stderr || strings.Contains(spaced.stderr, "引数") || !strings.Contains(spaced.stderr, "expected 1 argument(s), got 0") {
		t.Fatalf("spaced %d %q, joined %d %q", spaced.code, spaced.stderr, joined.code, joined.stderr)
	}
	for _, args := range [][]string{
		{"notify", "--lang=ja"},
		{"notify", "--lang", "ja"},
		{"--lang=ja", "notify"},
		{"notify", "--message", "text", "--lang", "ja"},
	} {
		if result := runCLI(t, args...); result.code != 2 || !strings.Contains(result.stderr, "引数") {
			t.Errorf("%v: exit=%d stderr=%s", args, result.code, result.stderr)
		}
	}
}
