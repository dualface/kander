package rules

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNamesAreMarkdownOnly(t *testing.T) {
	names := Names()
	if len(names) < 14 {
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
	if !seen["KANDER-AGENTS.md"] || !seen["KANDER-BASE-RULES.md"] || !seen["KANDER-ISSUE-RULES.md"] {
		t.Fatalf("missing entry files: %v", names)
	}
	if !seen["templates/README.md"] || !seen["snippets/README.md"] {
		t.Fatalf("missing templates/snippets entries: %v", names)
	}
	for _, name := range names {
		if _, err := File(name); err != nil {
			t.Fatalf("enumerated %s is not readable: %v", name, err)
		}
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

func TestEntryDeclaresIssueRulesLoadTiming(t *testing.T) {
	data, err := File("KANDER-AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "KANDER-ISSUE-RULES.md") {
		t.Fatal("the rules entry must name the issue rules file and its load timing")
	}
}

func TestEntryNamesLoadPath(t *testing.T) {
	data, err := File("KANDER-AGENTS.md")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"KANDER-BASE-RULES.md", "KANDER-LOADING-RULES.md"} {
		if !strings.Contains(string(data), name) {
			t.Fatalf("the rules entry must send every session to %s", name)
		}
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
	for _, name := range []string{"templates/../KANDER-BASE-RULES.md", "/KANDER-BASE-RULES.md", `templates\README.md`, "./KANDER-BASE-RULES.md", "templates//README.md"} {
		if _, err := File(name); err == nil {
			t.Fatalf("%q must be rejected", name)
		}
	}
}

func TestHashMatchesEmbeddedBytes(t *testing.T) {
	data, err := File("templates/README.md")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	digest, err := Hash("templates/README.md")
	if err != nil || digest != hex.EncodeToString(sum[:]) {
		t.Fatalf("digest=%s err=%v", digest, err)
	}
}

func TestInstalledPrefersInstalledCopy(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "README.md"), []byte("local copy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	data, source, err := Installed(dir, "templates/README.md")
	if err != nil || source != SourceInstalled || string(data) != "local copy\n" {
		t.Fatalf("data=%q source=%s err=%v", data, source, err)
	}
}

func TestInstalledFallsBackToEmbeddedWithoutWriting(t *testing.T) {
	dir := t.TempDir()
	want, err := File("snippets/README.md")
	if err != nil {
		t.Fatal(err)
	}
	data, source, err := Installed(dir, "snippets/README.md")
	if err != nil || source != SourceEmbedded || string(data) != string(want) {
		t.Fatalf("source=%s err=%v equal=%v", source, err, string(data) == string(want))
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("fallback must not write into the rules root: %v %v", entries, err)
	}
	if _, _, err := Installed(dir, "../x.md"); err == nil {
		t.Fatal("invalid name must fail")
	}
}

func TestRender(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "templates"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "templates", "probe.md"), []byte("Task {{.TaskID}} says {{.Title}}"), 0o600); err != nil {
		t.Fatal(err)
	}
	out, source, err := Render(dir, "templates/probe.md", map[string]string{"TaskID": "t-1", "Title": "hello {{.TaskID}}"})
	if err != nil || source != SourceInstalled {
		t.Fatalf("out=%q source=%s err=%v", out, source, err)
	}
	if out != "Task t-1 says hello {{.TaskID}}" {
		t.Fatalf("data must not be parsed as template text: %q", out)
	}
	if _, _, err := Render(dir, "templates/probe.md", map[string]string{"TaskID": "t-1"}); err == nil {
		t.Fatal("missing key must fail the whole render")
	}
	if _, _, err := Render(dir, "snippets/README.md", nil); err == nil {
		t.Fatal("a snippet is not a template")
	}
	if _, source, err := Render(dir, "templates/README.md", map[string]string{}); err != nil || source != SourceEmbedded {
		t.Fatalf("embedded template must render: source=%s err=%v", source, err)
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
