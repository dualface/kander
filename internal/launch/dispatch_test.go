package launch

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func TestDispatchPromptTranslations(t *testing.T) {
	for _, lang := range []string{"en", "cn", "ja"} {
		config.ApplyLanguageArgument([]string{"kander", "--lang", lang})
		t.Setenv(config.EnvLang, lang)
		t.Setenv(config.EnvLangCLI, "1")
		d := board.Dispatch{Input: board.DispatchInput{ID: "prompt-one", TaskID: "20260908-prompt-task", Kind: "fix"}, Authorization: board.ExecutionAuthorization{DispatchID: "prompt-one", Epoch: 4}}
		prompt, err := DispatchInstruction(config.InstallPaths{Mode: config.ModeGlobal}, d)
		if err != nil {
			t.Fatal(err)
		}
		for _, part := range []string{"--dispatch-id prompt-one", "--execution-epoch 4", "--expect-revision", "--delivery-commit", "--disposition", "replayed=false"} {
			if !strings.Contains(prompt, part) {
				t.Fatalf("%s missing %s: %s", lang, part, prompt)
			}
		}
	}
}

// A wrap-up completion binds the source_commit of its evidence and takes no disposition, so the
// prompt names that exact command and never offers the disposition flag or the integration file
// as one.
func TestDispatchPromptWrapUpNamesSourceCommit(t *testing.T) {
	source := strings.Repeat("d", 40)
	d := board.Dispatch{
		Input: board.DispatchInput{ID: "wrap-one", TaskID: "20260908-prompt-task", Kind: "wrap-up", Evidence: board.DispatchEvidence{WrapUp: &board.DispatchWrapUpBinding{
			Artifact: board.ArtifactReference{TaskID: "20260908-prompt-task", Path: "dispatches/wrap-one/integration.json"},
			Git:      board.DispatchIntegration{SourceCommit: source},
		}}},
		Authorization: board.ExecutionAuthorization{DispatchID: "wrap-one", Epoch: 3},
	}
	prompt, err := DispatchInstruction(config.InstallPaths{Mode: config.ModeGlobal}, d)
	if err != nil {
		t.Fatal(err)
	}
	command := "kander move 20260908-prompt-task done --result completed --dispatch-id wrap-one --execution-epoch 3 --delivery-commit " + source + ";"
	for _, part := range []string{command, "INTEGRATION: dispatches/wrap-one/integration.json (read-only integration evidence, not a disposition argument)"} {
		if !strings.Contains(prompt, part) {
			t.Fatalf("prompt lacks %q: %s", part, prompt)
		}
	}
	if strings.Contains(prompt, "--disposition") || strings.Contains(prompt, "<final full SHA>") {
		t.Fatalf("wrap-up prompt offers a disposition or a placeholder commit: %s", prompt)
	}
}
