//go:build unix

package review

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewInstructionEndsAtFileProtocol(t *testing.T) {
	line, err := reviewInstruction("", "PMQA", "/tmp/prompt.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(line, "follow it exactly.") {
		t.Fatalf("line=%q", line)
	}
}

func TestStdinReviewLanguageLivesInContract(t *testing.T) {
	h := newCodexHarness(t)
	t.Setenv("HOME", h.home)
	instructionLog := filepath.Join(h.root, "instruction.log")
	t.Setenv("FAKE_CODEX_INSTRUCTION", instructionLog)
	if code, _, stderr := h.defaultReview(); code != 0 {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
	language, err := reportLanguageFromConfig()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(instructionLog)
	if err != nil {
		t.Fatal(err)
	}
	if instruction := strings.TrimRight(string(data), "\n"); !strings.HasSuffix(instruction, "follow it exactly.") || strings.Contains(instruction, "\n") {
		t.Fatalf("instruction=%q", instruction)
	}
	if prompt := readFile(t, h.stdinLog); !strings.Contains(prompt, `Hard requirement for every conversation related to this review: communicate with the user in "`+language+`".`) {
		t.Fatalf("review contract lacks language: %s", prompt)
	}
}
