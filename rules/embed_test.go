package rules

import (
	"strings"
	"testing"
)

func TestNamesAreChineseMarkdownOnly(t *testing.T) {
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

func TestFileEnglishFallsBackToChinese(t *testing.T) {
	cn, actualCN, err := File(LangCN, "KANDER-AGENTS.md")
	if err != nil || actualCN != LangCN || len(cn) == 0 {
		t.Fatalf("cn: actual=%s err=%v", actualCN, err)
	}
	en, actualEN, err := File(LangEN, "KANDER-AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if actualEN != LangCN {
		t.Fatalf("expected cn fallback, got %s", actualEN)
	}
	if string(en) != string(cn) {
		t.Fatal("en fallback must return the chinese original")
	}
}

func TestFileChineseNeverFallsBack(t *testing.T) {
	_, actual, err := File("fr", "KANDER-AGENTS.md")
	if err != nil || actual != LangCN {
		t.Fatalf("unknown lang must read cn: actual=%s err=%v", actual, err)
	}
}

func TestFileRejectsPlaceholderAndInvalidNames(t *testing.T) {
	if _, _, err := File(LangEN, "README.txt"); err == nil {
		t.Fatal("placeholder must not be readable as a rule")
	}
	if _, _, err := File(LangCN, "../embed.go"); err == nil {
		t.Fatal("path escape must fail")
	}
	if _, _, err := File(LangCN, "missing.md"); err == nil {
		t.Fatal("missing cn file must fail without fallback")
	}
}

func TestHashStable(t *testing.T) {
	digest, actual, err := Hash(LangEN, "KANDER-BASE-RULES.md")
	if err != nil || actual != LangCN || len(digest) != 64 {
		t.Fatalf("digest=%s actual=%s err=%v", digest, actual, err)
	}
	again, _, err := Hash(LangCN, "KANDER-BASE-RULES.md")
	if err != nil || again != digest {
		t.Fatalf("hash mismatch: %s vs %s", digest, again)
	}
}
