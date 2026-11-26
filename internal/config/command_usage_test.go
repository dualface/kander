package config

import (
	"strings"
	"testing"

	"github.com/dualface/kander/v2/rules"
)

func TestReadSentenceCopiesMatch(t *testing.T) {
	want := CommandReadSentence("<rules root>")
	for _, name := range []string{"snippets/commands/README.md", "KANDER-KANBAN-RULES.md", "KANDER-AGENTS.md", "KANDER-BASE-RULES.md"} {
		data, err := rules.File(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Fatalf("%s does not carry the conditional read sentence verbatim", name)
		}
	}
}
