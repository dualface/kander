package install

import (
	"io"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestParseCopyAnswer(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"y", true},
		{"Y", true},
		{" yes ", true},
		{"YES", true},
		{"n", false},
		{"N", false},
		{"no", false},
		{"", false},
		{"maybe", false},
		{"yep", false},
	} {
		if got := parseCopyAnswer(tc.in); got != tc.want {
			t.Fatalf("parseCopyAnswer(%q)=%v", tc.in, got)
		}
	}
}

func TestRunCopyYesNoLineInput(t *testing.T) {
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	previousIn, previousOut := copyPromptIn, copyPromptOut
	t.Cleanup(func() { copyPromptIn, copyPromptOut = previousIn, previousOut })
	check := pathCheck{Current: "/tmp/kander"}
	for _, tc := range []struct {
		in   string
		want bool
	}{
		{"y\n", true},
		{"yes\n", true},
		{"n\n", false},
		{"\n", false},
		{"", false},
		{"nope\n", false},
	} {
		var out strings.Builder
		copyPromptIn = strings.NewReader(tc.in)
		copyPromptOut = &out
		got, err := runCopyYesNo(check, "/home/user/.local/bin/kander")
		if err != nil || got != tc.want {
			t.Fatalf("in=%q got=%v err=%v", tc.in, got, err)
		}
		text := out.String()
		if !strings.Contains(text, config.Text("install.copy_question")) || !strings.Contains(text, config.Text("install.copy_yn")) {
			t.Fatalf("prompt missing question or y/n: %s", text)
		}
	}
}

func TestRunCopyYesNoEOFIsDecline(t *testing.T) {
	previousIn, previousOut := copyPromptIn, copyPromptOut
	t.Cleanup(func() { copyPromptIn, copyPromptOut = previousIn, previousOut })
	copyPromptIn = strings.NewReader("")
	copyPromptOut = io.Discard
	got, err := runCopyYesNo(pathCheck{Current: "cur"}, "dest")
	if err != nil || got {
		t.Fatalf("got=%v err=%v", got, err)
	}
}
