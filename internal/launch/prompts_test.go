package launch

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

// TestStartPromptNeverCarriesCardBodies pins the handoff boundary: the card is
// the trusted place for the source-attachment requirement, while the task file
// keeps only the task ID and the fixed launch contract. Private issue text and
// attachment paths must not travel through the prompt, argv or environment.
func TestStartPromptNeverCarriesCardBodies(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	t.Setenv(config.EnvLangCLI, "")
	paths := config.InstallPaths{
		Mode:     config.ModeGlobal,
		RulesDir: filepath.Join(t.TempDir(), "rules"),
	}
	card := "# Secret issue title\n\n" +
		"- LANGUAGE: en\n- SIZE: small\n\n" +
		"## GOAL\n\nRead source/github-issue.md first. PRIVATE-ISSUE-BODY\n\n" +
		"## DISCUSSION\n\nATTACHMENT source/github-issue.json COMMENT-SECRET\n"
	prompt, err := startAgentPrompt("task-1", paths, "", card)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"Secret issue title", "PRIVATE-ISSUE-BODY", "ATTACHMENT", "COMMENT-SECRET"} {
		if strings.Contains(prompt, secret) {
			t.Fatalf("card body %q leaked into the task file: %s", secret, prompt)
		}
	}
	if !strings.Contains(prompt, "task-1") {
		t.Fatalf("task ID missing from the prompt: %s", prompt)
	}
}
