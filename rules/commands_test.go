package rules

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// contractSections is the fixed section order of every command contract.
var contractSections = []string{"## Purpose and users", "## Syntax", "## Arguments", "## Preconditions", "## Effects", "## Output", "## Rejections"}

// registeredCommands reads the frozen command registry from the CLI package source, so the
// contract set follows the binary and not a copy of the list.
func registeredCommands(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "internal", "cli", "cli.go"))
	if err != nil {
		t.Fatal(err)
	}
	block := regexp.MustCompile(`(?s)var commandNames = \[\]string\{(.*?)\n\}`).FindStringSubmatch(string(data))
	if block == nil {
		t.Fatal("commandNames not found in internal/cli/cli.go")
	}
	var names []string
	for _, m := range regexp.MustCompile(`"([a-z-]+)"`).FindAllStringSubmatch(block[1], -1) {
		names = append(names, m[1])
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

func TestEveryCommandHasAContract(t *testing.T) {
	readme, err := File("snippets/commands/README.md")
	if err != nil {
		t.Fatal(err)
	}
	commands := registeredCommands(t)
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
	known := map[string]bool{}
	for _, cmd := range commands {
		known[cmd] = true
	}
	for _, name := range Names() {
		if !strings.HasPrefix(name, "snippets/commands/") || name == "snippets/commands/README.md" {
			continue
		}
		if cmd := strings.TrimSuffix(strings.TrimPrefix(name, "snippets/commands/"), ".md"); !known[cmd] {
			t.Fatalf("%s documents a command the CLI does not register", name)
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
	for _, cmd := range registeredCommands(t) {
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

// parserFunctions names, per command, the Go functions that parse its arguments. The flags a
// contract documents must all be parsed by one of them, and every "--flag" literal in them
// must be documented by a command that lists the function.
var parserFunctions = map[string][]string{
	"config":      {"internal/menu/commands.go:parseConfigArgs"},
	"doctor":      {"internal/menu/commands.go:Doctor"},
	"version":     {"internal/cli/cli.go:runVersion"},
	"install":     {"internal/install/cmd.go:Run"},
	"terminal":    {"internal/terminalcheck/command.go:run"},
	"init":        {"internal/board/cmd.go:RunInit"},
	"list":        {"internal/board/cmd.go:RunList"},
	"show":        {"internal/board/cmd.go:RunShow"},
	"new":         {"internal/board/cmd.go:RunNew"},
	"move":        {"internal/board/cmd.go:RunMove"},
	"pick":        {"internal/board/cmd.go:RunPick"},
	"update":      {"internal/board/update.go:RunUpdate"},
	"dispatch":    {"internal/board/dispatch_cmd.go:RunDispatch"},
	"guard-write": {"internal/board/guard.go:RunGuardWrite"},
	"check":       {"internal/check/args.go:parseOptions", "internal/liveness/check.go:RunCheck"},
	"subscribe":   {"internal/liveness/subscribe.go:parseSubscribeArgs"},
	"start":       {"internal/launch/cmd.go:RunStart", "internal/launch/cmd.go:parseAgentLauncher"},
	"resume":      {"internal/launch/cmd.go:RunResume", "internal/launch/dispatch.go:ParseDispatchOptions"},
	"notify":      {"internal/notify/cmd.go:RunNotify", "internal/launch/dispatch.go:ParseDispatchOptions"},
	"orchestrate": {"internal/launch/orchestrate.go:RunOrchestrate", "internal/launch/cmd.go:parseAgentLauncher"},
	"coordinator": {"internal/launch/coordinator.go:RunCoordinator"},
	"dismiss":     {"internal/takeover/cmd.go:RunDismiss"},
	"review":      {"internal/review/archive.go:parseArchiveOptions", "internal/review/disposition.go:runDispositionCommand"},
	"issue":       {"internal/issue/command.go:runRepo", "internal/issue/command_list.go:runList", "internal/issue/command_show.go:runShow", "internal/issue/command_import.go:runImport", "internal/issue/command_triage.go:runTriage", "internal/issue/command_result.go:runResult"},
}

var flagLiteral = regexp.MustCompile(`^--[a-z][a-z-]*=?$`)
var flagInSyntax = regexp.MustCompile(`--[a-z][a-z-]*`)

// functionFlagLiterals returns every "--flag" string literal inside the named function.
func functionFlagLiterals(t *testing.T, ref string) map[string]bool {
	t.Helper()
	file, fn, _ := strings.Cut(ref, ":")
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, filepath.Join("..", filepath.FromSlash(file)), nil, 0)
	if err != nil {
		t.Fatalf("%s: %v", ref, err)
	}
	out := map[string]bool{}
	found := false
	for _, decl := range parsed.Decls {
		f, ok := decl.(*ast.FuncDecl)
		if !ok || f.Name.Name != fn {
			continue
		}
		found = true
		ast.Inspect(f.Body, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			value := strings.Trim(lit.Value, "\"`")
			if flagLiteral.MatchString(value) && value != "--help" {
				out[strings.TrimSuffix(value, "=")] = true
			}
			return true
		})
	}
	if !found {
		t.Fatalf("%s: function not found", ref)
	}
	return out
}

func TestContractFlagsMatchParsers(t *testing.T) {
	commands := registeredCommands(t)
	if len(parserFunctions) != len(commands) {
		t.Fatalf("parserFunctions covers %d commands, registry has %d", len(parserFunctions), len(commands))
	}
	documented := map[string]map[string]bool{}
	for _, cmd := range commands {
		data, err := File("snippets/commands/" + cmd + ".md")
		if err != nil {
			t.Fatal(err)
		}
		flags := map[string]bool{}
		for _, line := range syntaxBlock(t, string(data)) {
			for _, flag := range flagInSyntax.FindAllString(line, -1) {
				flags[flag] = true
			}
		}
		documented[cmd] = flags
	}
	literals := map[string]map[string]bool{}
	users := map[string][]string{}
	for cmd, refs := range parserFunctions {
		for _, ref := range refs {
			if literals[ref] == nil {
				literals[ref] = functionFlagLiterals(t, ref)
			}
			users[ref] = append(users[ref], cmd)
		}
	}
	var problems []string
	for cmd, flags := range documented {
		for flag := range flags {
			parsed := false
			for _, ref := range parserFunctions[cmd] {
				if literals[ref][flag] {
					parsed = true
				}
			}
			if !parsed {
				problems = append(problems, cmd+": documented flag "+flag+" is parsed by none of its functions")
			}
		}
	}
	for ref, flags := range literals {
		for flag := range flags {
			known := false
			for _, cmd := range users[ref] {
				if documented[cmd][flag] {
					known = true
				}
			}
			if !known {
				problems = append(problems, ref+": parses "+flag+", which no contract of its commands documents")
			}
		}
	}
	sort.Strings(problems)
	if len(problems) != 0 {
		t.Fatalf("contract and parser flags differ:\n%s", strings.Join(problems, "\n"))
	}
}
