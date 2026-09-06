package rules

import (
	"strings"
	"testing"
)

func TestNamesAreMarkdownOnly(t *testing.T) {
	names := Names()
	if len(names) != 10 {
		t.Fatalf("names=%v", names)
	}
	seen := map[string]bool{}
	for _, name := range names {
		if !strings.HasSuffix(name, ".md") {
			t.Fatalf("non-markdown enumerated: %s", name)
		}
		if seen[name] {
			t.Fatalf("duplicate %s", name)
		}
		seen[name] = true
	}
	if !seen["KANDER-AGENTS.md"] || !seen["KANDER-BASE-RULES.md"] {
		t.Fatalf("missing entry files: %v", names)
	}
}

func TestEntryDeclaresAgentLanguage(t *testing.T) {
	data, err := File("KANDER-AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "`agent_language`") {
		t.Fatal("the rules entry must tell agents to honor agent_language")
	}
}

func TestFileRejectsInvalidNames(t *testing.T) {
	if _, err := File("embed.go"); err == nil {
		t.Fatal("non-markdown must not be readable as a rule")
	}
	if _, err := File("../embed.go"); err == nil {
		t.Fatal("path escape must fail")
	}
	if _, err := File("missing.md"); err == nil {
		t.Fatal("missing file must fail")
	}
}

func TestHashStable(t *testing.T) {
	digest, err := Hash("KANDER-BASE-RULES.md")
	if err != nil || len(digest) != 64 {
		t.Fatalf("digest=%s err=%v", digest, err)
	}
	again, err := Hash("KANDER-BASE-RULES.md")
	if err != nil || again != digest {
		t.Fatalf("hash mismatch: %s vs %s", digest, again)
	}
}
