//go:build unix

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestReviewInstructionAppendsReminderOnlyForAReportLanguage(t *testing.T) {
	plain, err := reviewInstruction("", "PMQA", "/tmp/prompt.md", "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(plain, "follow it exactly.") {
		t.Fatalf("an empty report language changed the instruction: %q", plain)
	}
	line, err := reviewInstruction("", "PMQA", "/tmp/prompt.md", "ja")
	if err != nil {
		t.Fatal(err)
	}
	if line != plain+" "+config.LanguageReminder("ja") {
		t.Fatalf("line=%q", line)
	}
}

func TestStdinReviewInstructionCarriesTheReminderButThePromptFileDoesNot(t *testing.T) {
	h := newCodexHarness(t)
	instructionLog := filepath.Join(h.root, "instruction.log")
	t.Setenv("FAKE_CODEX_INSTRUCTION", instructionLog)
	if code, _, stderr := h.defaultReview(); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	language, err := reportLanguageFromConfig()
	if err != nil {
		t.Fatal(err)
	}
	reminder := config.LanguageReminder(language)
	if reminder == "" {
		t.Fatalf("no reminder for %q", language)
	}
	data, err := os.ReadFile(instructionLog)
	if err != nil {
		t.Fatal(err)
	}
	if instruction := strings.TrimRight(string(data), "\n"); !strings.HasSuffix(instruction, "follow it exactly. "+reminder) || strings.Contains(instruction, "\n") {
		t.Fatalf("stdin instruction lacks the %q reminder: %q", language, instruction)
	}
	if prompt := readFile(t, h.stdinLog); strings.Contains(prompt, reminder) {
		t.Fatalf("bootstrap or contract file carries the reminder:\n%s", prompt)
	}
}
