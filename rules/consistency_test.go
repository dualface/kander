package rules

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Cross references inside the published files take two forms: a rule file name followed by
// one or more quoted section names, and a relative path below templates/ or snippets/. Both
// must resolve, or an agent sent to a section or a snippet finds nothing there. A referable
// name is a Markdown heading, a bold title line, or the label of a `- Label: ...` bullet.
var (
	sectionReference = regexp.MustCompile("`(KANDER-[A-Z-]+\\.md)`((?:,? ?(?:and |or )?\"[^\"]+\")+)")
	quotedName       = regexp.MustCompile(`"([^"]+)"`)
	pathReference    = regexp.MustCompile(`(?:snippets|templates)/[A-Za-z0-9_./-]+\.md`)
	headingLine      = regexp.MustCompile(`(?m)^(?:#{2,6}\s+(.+?)\s*|\*\*(.+?)\*\*\s*|- ([A-Z][^:\n]{2,60}):.*)$`)
)

// headings returns every referable name of one embedded file.
func headings(t *testing.T, name string) map[string]bool {
	t.Helper()
	data, err := File(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	out := map[string]bool{}
	for _, match := range headingLine.FindAllStringSubmatch(string(data), -1) {
		title := match[1]
		if title == "" {
			title = match[2]
		}
		if title == "" {
			title = match[3]
		}
		out[strings.TrimSpace(title)] = true
	}
	return out
}

func brokenReferences(t *testing.T, files map[string]string) []string {
	t.Helper()
	exists := map[string]bool{}
	for _, name := range Names() {
		exists[name] = true
	}
	headingCache := map[string]map[string]bool{}
	var broken []string
	for name, text := range files {
		for _, match := range sectionReference.FindAllStringSubmatch(text, -1) {
			target := match[1]
			if !exists[target] {
				broken = append(broken, fmt.Sprintf("%s: references missing file %s", name, target))
				continue
			}
			if headingCache[target] == nil {
				headingCache[target] = headings(t, target)
			}
			for _, quoted := range quotedName.FindAllStringSubmatch(match[2], -1) {
				if !headingCache[target][quoted[1]] {
					broken = append(broken, fmt.Sprintf("%s: references %s %q, no such section", name, target, quoted[1]))
				}
			}
		}
		for _, ref := range pathReference.FindAllString(text, -1) {
			if !exists[ref] {
				broken = append(broken, fmt.Sprintf("%s: references missing path %s", name, ref))
			}
		}
	}
	sort.Strings(broken)
	return broken
}

func embeddedFiles(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, name := range Names() {
		data, err := File(name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(data)
	}
	return files
}

func TestCrossReferencesResolve(t *testing.T) {
	if broken := brokenReferences(t, embeddedFiles(t)); len(broken) != 0 {
		t.Fatalf("broken references:\n%s", strings.Join(broken, "\n"))
	}
}

func TestCrossReferenceCheckDetectsBreakage(t *testing.T) {
	files := embeddedFiles(t)
	files["probe.md"] = "See `KANDER-BASE-RULES.md` \"No Such Section\" and snippets/none.md."
	broken := brokenReferences(t, files)
	if len(broken) != 2 {
		t.Fatalf("expected two broken references, got %v", broken)
	}
	if !strings.Contains(broken[0], `"No Such Section"`) || !strings.Contains(broken[1], "snippets/none.md") {
		t.Fatalf("unexpected diagnostics: %v", broken)
	}
}
