package rules

import (
	"regexp"
	"strings"
	"testing"
)

// contractSections is the fixed section order of every command contract.
var contractSections = []string{"## Purpose and users", "## Syntax", "## Arguments", "## Preconditions", "## Effects", "## Output", "## Rejections"}

// contractCommands lists the commands that have a contract file. Whether the command tree has
// exactly these commands is checked where the whole tree is linked (cmd/kander).
func contractCommands(t *testing.T) []string {
	t.Helper()
	var names []string
	for _, name := range Names() {
		if !strings.HasPrefix(name, "snippets/commands/") || name == "snippets/commands/README.md" {
			continue
		}
		names = append(names, strings.TrimSuffix(strings.TrimPrefix(name, "snippets/commands/"), ".md"))
	}
	if len(names) < 20 {
		t.Fatalf("suspiciously few commands: %v", names)
	}
	return names
}

func syntaxBlock(t *testing.T, text string) []string {
	t.Helper()
	idx := strings.Index(text, "\n## Syntax\n")
	if idx < 0 {
		t.Fatal("no Syntax section")
	}
	rest := text[idx+len("\n## Syntax\n"):]
	open := strings.Index(rest, "```text\n")
	if open < 0 {
		t.Fatal("no ```text block under Syntax")
	}
	rest = rest[open+len("```text\n"):]
	closeIdx := strings.Index(rest, "\n```")
	if closeIdx < 0 {
		t.Fatal("unterminated Syntax block")
	}
	return strings.Split(rest[:closeIdx], "\n")
}

func tableBlock(t *testing.T, text, where string) string {
	t.Helper()
	block := regexp.MustCompile("(?m)^```text\n(?:kander .*\n)+```").FindString(text)
	if block == "" {
		t.Fatalf("no command table block in %s", where)
	}
	return block
}

func TestEveryContractIsWellFormed(t *testing.T) {
	readme, err := File("snippets/commands/README.md")
	if err != nil {
		t.Fatal(err)
	}
	commands := contractCommands(t)
	for _, cmd := range commands {
		name := "snippets/commands/" + cmd + ".md"
		data, err := File(name)
		if err != nil {
			t.Fatalf("%s has no contract: %v", cmd, err)
		}
		text := string(data)
		if !strings.HasPrefix(text, "# kander "+cmd+"\n") {
			t.Fatalf("%s: first line must be the command heading", name)
		}
		last := -1
		for _, section := range contractSections {
			idx := strings.Index(text, "\n"+section+"\n")
			if idx < 0 || idx < last {
				t.Fatalf("%s: section %q missing or out of order", name, section)
			}
			last = idx
		}
		if !strings.Contains(string(readme), "`"+name+"`") {
			t.Fatalf("README does not list %s", name)
		}
	}
}

func TestCommandTableIsSingleSource(t *testing.T) {
	readme, _ := File("snippets/commands/README.md")
	kanban, _ := File("KANDER-KANBAN-RULES.md")
	table := tableBlock(t, string(readme), "README")
	if table != tableBlock(t, string(kanban), "KANDER-KANBAN-RULES.md") {
		t.Fatal("the command table in README and in KANDER-KANBAN-RULES.md differ")
	}
	lines := strings.Split(table, "\n")
	lines = lines[1 : len(lines)-1]
	inTable := map[string]bool{}
	for _, line := range lines {
		inTable[line] = true
		cmd := strings.Fields(line)[1]
		data, err := File("snippets/commands/" + cmd + ".md")
		if err != nil {
			t.Fatalf("table names %s without a contract", cmd)
		}
		found := false
		for _, syntax := range syntaxBlock(t, string(data)) {
			if syntax == line {
				found = true
			}
		}
		if !found {
			t.Fatalf("table line is not in the %s contract Syntax block:\n%s", cmd, line)
		}
	}
	for _, cmd := range contractCommands(t) {
		data, _ := File("snippets/commands/" + cmd + ".md")
		for _, syntax := range syntaxBlock(t, string(data)) {
			if !inTable[syntax] {
				t.Fatalf("%s Syntax line is missing from the table:\n%s", cmd, syntax)
			}
		}
	}
}

func TestCommandIndexWordLimits(t *testing.T) {
	readme, _ := File("snippets/commands/README.md")
	if n := len(strings.Fields(string(readme))); n > 800 {
		t.Fatalf("README has %d words, limit 800", n)
	}
	kanban, _ := File("KANDER-KANBAN-RULES.md")
	text := string(kanban)
	start := strings.Index(text, "\n## Command Contract\n")
	end := strings.Index(text[start+1:], "\n## ")
	if start < 0 || end < 0 {
		t.Fatal("Command Contract section not found")
	}
	if n := len(strings.Fields(text[start : start+1+end])); n > 600 {
		t.Fatalf("Command Contract has %d words, limit 600", n)
	}
	for _, title := range []string{"**Durable Dispatch**", "**Launchers**", "**Reading the event stream**", "**Subscription Events**", "**Card Pins**", "**Check and Liveness Classification**"} {
		if strings.Contains(text, title) {
			t.Fatalf("KANDER-KANBAN-RULES.md still carries %s", title)
		}
	}
}

// Every contract tells in its Output section what the command prints with the global --json.
func TestEveryContractDescribesItsJSONOutput(t *testing.T) {
	for _, cmd := range contractCommands(t) {
		data, err := File("snippets/commands/" + cmd + ".md")
		if err != nil {
			t.Fatal(err)
		}
		text := string(data)
		start := strings.Index(text, "\n## Output\n")
		end := strings.Index(text, "\n## Rejections\n")
		if start < 0 || end < start {
			t.Fatalf("%s: no Output section", cmd)
		}
		output := text[start:end]
		if !strings.Contains(output, "--json") && !strings.Contains(output, "JSON") {
			t.Errorf("%s: the Output section does not describe the JSON output", cmd)
		}
	}
}

// The global flags are named in the README once, and no Syntax line repeats --json.
func TestGlobalJSONFlagIsNotInASyntaxLine(t *testing.T) {
	readme, _ := File("snippets/commands/README.md")
	if !strings.Contains(string(readme), "--json") {
		t.Fatal("the README does not name the global --json")
	}
	for _, cmd := range contractCommands(t) {
		data, _ := File("snippets/commands/" + cmd + ".md")
		for _, line := range syntaxBlock(t, string(data)) {
			if strings.Contains(line, "--json") {
				t.Errorf("%s: a Syntax line names the global --json: %s", cmd, line)
			}
		}
	}
}
