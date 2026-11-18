package rules

import (
	"regexp"
	"slices"
	"strings"
	"testing"
)

// contractSections is the fixed section order of every command contract.
var contractSections = []string{"## Purpose and users", "## Syntax", "## Arguments", "## Examples", "## Preconditions", "## Effects", "## Output", "## Rejections"}

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

// parsedContracts parses every contract as the command it documents; the contract of notify
// documents request as well.
func parsedContracts(t *testing.T) map[string]Contract {
	t.Helper()
	parsed := map[string]Contract{}
	for _, cmd := range contractCommands(t) {
		names := []string{cmd}
		if cmd == "notify" {
			names = append(names, "request")
		}
		for _, name := range names {
			contract, err := EmbeddedContract(name)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			parsed[name] = contract
		}
	}
	return parsed
}

// The summary is the first sentence of Purpose and users: one sentence, at most 25 words.
func TestEverySummaryIsOneShortSentence(t *testing.T) {
	for name, contract := range parsedContracts(t) {
		if !strings.HasSuffix(contract.Summary, ".") {
			t.Errorf("%s: the summary does not end with a period: %q", name, contract.Summary)
		}
		if words := len(strings.Fields(contract.Summary)); words > 25 {
			t.Errorf("%s: the summary has %d words, limit 25: %q", name, words, contract.Summary)
		}
	}
}

func TestFirstSentence(t *testing.T) {
	for text, want := range map[string]string{
		"Prints `kander v1.2`. Used by all.":         "Prints `kander v1.2`.",
		"Prints it.":                                 "Prints it.",
		"No period at all":                           "No period at all",
		"Runs `a. b` and stops. Then more.\nSecond.": "Runs `a. b` and stops.",
	} {
		if got := FirstSentence(text); got != want {
			t.Errorf("FirstSentence(%q) = %q, want %q", text, got, want)
		}
	}
}

// Every argument entry has a name and a text; a command without arguments says None. and has
// no entries.
func TestEveryArgumentEntryParses(t *testing.T) {
	for name, contract := range parsedContracts(t) {
		for _, argument := range contract.Arguments {
			if len(argument.Names) == 0 || strings.TrimSpace(argument.Text) == "" {
				t.Errorf("%s: an argument entry has no name or no text: %+v", name, argument)
			}
		}
	}
	for _, name := range []string{"config", "doctor", "install", "version"} {
		if contract := parsedContracts(t)[name]; len(contract.Arguments) != 0 {
			t.Errorf("%s: want no arguments, got %+v", name, contract.Arguments)
		}
		data, _ := File("snippets/commands/" + name + ".md")
		if !strings.Contains(string(data), "\n## Arguments\n\nNone.\n\n## Examples\n") {
			t.Errorf("%s: the Arguments section must be the single line None.", name)
		}
	}
}

var syntaxLongFlag = regexp.MustCompile(`--[a-z][a-z-]*`)

// Every long flag a Syntax line names is the name of an argument entry, except the global flags.
func TestEverySyntaxFlagHasAnArgumentEntry(t *testing.T) {
	for _, cmd := range contractCommands(t) {
		data, _ := File("snippets/commands/" + cmd + ".md")
		var documented strings.Builder
		contract, err := ParseContract(cmd, string(data))
		if err != nil {
			t.Fatal(err)
		}
		for _, argument := range contract.Arguments {
			documented.WriteString(strings.Join(argument.Names, " ") + " ")
		}
		for _, line := range CommandSyntaxLines(string(data)) {
			for _, flag := range syntaxLongFlag.FindAllString(line, -1) {
				if flag == "--lang" || flag == "--json" || flag == "--help" {
					continue
				}
				if !regexp.MustCompile(regexp.QuoteMeta(flag) + `\b`).MatchString(documented.String()) {
					t.Errorf("%s: %s is in a Syntax line and in no argument entry", cmd, flag)
				}
			}
		}
	}
}

// Every contract has Examples, each a "# description" and a command, and the command of an
// example starts with the command the contract documents.
func TestEveryContractHasExamples(t *testing.T) {
	for name, contract := range parsedContracts(t) {
		if len(contract.Examples) == 0 {
			t.Errorf("%s: no examples", name)
		}
		for _, example := range contract.Examples {
			if strings.TrimSpace(example.Description) == "" || !strings.HasPrefix(example.Command, "kander "+name) {
				t.Errorf("%s: example %+v does not start with `kander %s`", name, example, name)
			}
		}
	}
}

// request is described by its own section of the notify contract.
func TestRequestHasItsOwnSection(t *testing.T) {
	data, _ := File("snippets/commands/notify.md")
	text := string(data)
	idx := strings.Index(text, "\n## Request command\n")
	if idx < 0 || idx < strings.Index(text, "\n## Rejections\n") {
		t.Fatal("notify.md has no Request command section after the fixed sections")
	}
	request, notify := parsedContracts(t)["request"], parsedContracts(t)["notify"]
	if request.Summary == notify.Summary || !strings.HasPrefix(request.Summary, "Proposes") && !strings.Contains(request.Summary, "request") {
		t.Errorf("the request summary is not its own: %q", request.Summary)
	}
	for _, line := range request.Usage {
		if !strings.HasPrefix(line, "kander request ") {
			t.Errorf("request usage line %q", line)
		}
	}
	for _, line := range notify.Usage {
		if !strings.HasPrefix(line, "kander notify ") {
			t.Errorf("notify usage line %q", line)
		}
	}
}

func TestCommandOrderFollowsTheReadmeTable(t *testing.T) {
	order := CommandOrder()
	for _, want := range []string{"config", "notify", "request", "review", "issue"} {
		if !slices.Contains(order, want) {
			t.Errorf("CommandOrder lacks %s: %v", want, order)
		}
	}
	if slices.Index(order, "request") != slices.Index(order, "notify")+1 {
		t.Errorf("request does not follow notify: %v", order)
	}
	if len(order) != len(contractCommands(t))+1 {
		t.Errorf("CommandOrder %v does not list every contract and request", order)
	}
}

func TestSyntaxPaths(t *testing.T) {
	for line, want := range map[string][]string{
		"kander move <task-id> <backlog|todo> --owner x":          {"move"},
		"kander review <plan|close> --schema":                     {"review plan", "review close"},
		"kander server start [--data DIR]":                        {"server start"},
		"kander issue show NUMBER [--repo HOST/OWNER/REPO]":       {"issue show"},
		"kander request answer <task-id> accepted|done [note...]": {"request answer"},
	} {
		if got := SyntaxPaths(line); !slices.Equal(got, want) {
			t.Errorf("SyntaxPaths(%q) = %v, want %v", line, got, want)
		}
	}
}
