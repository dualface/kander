package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/rules"
)

func TestCommandUsageReadsInstalledContract(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "snippets", "commands"), 0o700); err != nil {
		t.Fatal(err)
	}
	contract := "# kander probe\n\n## Purpose and users\n\nx\n\n## Syntax\n\n```text\nkander probe --a\nkander probe sub --b\n```\n\n## Arguments\n"
	if err := os.WriteFile(filepath.Join(dir, "snippets", "commands", "probe.md"), []byte(contract), 0o600); err != nil {
		t.Fatal(err)
	}
	got := commandUsage(dir, "probe")
	want := "kander probe --a\nkander probe sub --b\nContract: " + dir + "/snippets/commands/probe.md\n" + CommandReadSentence(dir)
	if got != want {
		t.Fatalf("usage:\n%s\nwant:\n%s", got, want)
	}
}

func TestCommandUsageFallsBackToEmbeddedAndPlaceholder(t *testing.T) {
	got := commandUsage("", "move")
	data, err := rules.File("snippets/commands/move.md")
	if err != nil {
		t.Fatal(err)
	}
	syntax := strings.Join(contractSyntaxLines(string(data)), "\n")
	if !strings.HasPrefix(got, syntax+"\n") {
		t.Fatalf("usage does not start with the embedded syntax block:\n%s", got)
	}
	if !strings.Contains(got, "Contract: <rules root>/snippets/commands/move.md\n") {
		t.Fatalf("placeholder contract line missing:\n%s", got)
	}
	if !strings.HasSuffix(got, CommandReadSentence("<rules root>")) {
		t.Fatalf("read sentence missing:\n%s", got)
	}
	if len(strings.Split(got, "\n")) != 3 {
		t.Fatalf("expected one syntax line, one contract line, one sentence line:\n%s", got)
	}
}

func TestCommandUsageWithoutContract(t *testing.T) {
	got := commandUsage(t.TempDir(), "no-such-command")
	if !strings.HasPrefix(got, "kander no-such-command\n") {
		t.Fatalf("fallback usage:\n%s", got)
	}
}

func TestReadSentenceCopiesMatch(t *testing.T) {
	want := CommandReadSentence("<rules root>")
	for _, name := range []string{"snippets/commands/README.md", "KANDER-KANBAN-RULES.md"} {
		data, err := rules.File(name)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), want) {
			t.Fatalf("%s does not carry the conditional read sentence verbatim", name)
		}
	}
}
