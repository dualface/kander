package launch

import (
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func TestDispatchPromptTranslationsAndFlagParsing(t *testing.T) {
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
	rest, o, err := ParseDispatchOptions([]string{"task", "--dispatch-id=id-one", "--kind", "sync", "--base", strings.Repeat("a", 40)})
	if err != nil || len(rest) != 1 || o.ID != "id-one" || o.Kind != "sync" {
		t.Fatalf("%+v %v", o, err)
	}
	if _, _, err = ParseDispatchOptions([]string{"--kind", "fix", "--kind=sync"}); err == nil {
		t.Fatal("duplicate accepted")
	}
}
