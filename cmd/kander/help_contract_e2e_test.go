package main

import (
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/rules"
)

// visibleLeaves lists the command paths that have a contract: every leaf but the internal worker.
func visibleLeaves(t *testing.T) []leafCommand {
	t.Helper()
	var leaves []leafCommand
	for _, leaf := range commandLeaves(cli.NewRoot()) {
		if !leaf.Hidden {
			leaves = append(leaves, leaf)
		}
	}
	return leaves
}

// contractOf is the top-level command that owns the contract of a command path.
func contractOf(path string) string { return strings.Fields(path)[0] }

// Every command path answers every form of help request with the contract help of its
// top-level command, in the fixed text format, on stdout with exit 0.
func TestEveryCommandHelpFollowsTheTextFormat(t *testing.T) {
	for _, leaf := range visibleLeaves(t) {
		contract, err := rules.EmbeddedContract(contractOf(leaf.path))
		if err != nil {
			t.Fatalf("%s: %v", leaf.path, err)
		}
		path := strings.Fields(leaf.path)
		var reference string
		for name, args := range map[string][]string{
			"--help": append(slices.Clone(path), "--help"),
			"-h":     append(slices.Clone(path), "-h"),
			"help":   append([]string{"help"}, path...),
		} {
			t.Run(leaf.path+"/"+name, func(t *testing.T) {
				result := runKander(t, args...)
				if result.stderr != "" && !strings.Contains(result.stderr, "kander doctor") {
					t.Fatalf("unexpected stderr: %q", result.stderr)
				}
				if result.code != 0 {
					t.Fatalf("exit=%d stderr=%s", result.code, result.stderr)
				}
				if reference == "" {
					reference = result.stdout
				}
				if result.stdout != reference {
					t.Fatalf("the forms of help differ:\n%s\n---\n%s", result.stdout, reference)
				}
				checkTextFormat(t, result.stdout, contract)
			})
		}
	}
}

// checkTextFormat checks the sections of a command help, in order, and the fixed line width of
// the wrapped ones.
func checkTextFormat(t *testing.T, text string, contract rules.Contract) {
	t.Helper()
	if strings.Contains(text, "\x1b") {
		t.Fatalf("help holds an escape sequence:\n%q", text)
	}
	last := 0
	for _, marker := range []string{contract.Summary + "\n\nUsage:\n", "\nDescription:\n", "\nArguments:\n", "\nExamples:\n", "\nGlobal flags:\n", "\nContract: "} {
		idx := strings.Index(text, marker)
		if idx < 0 || idx < last {
			t.Fatalf("section %q missing or out of order:\n%s", marker, text)
		}
		last = idx
	}
	for _, usage := range contract.Usage {
		if !strings.Contains(text, "\n  "+usage+"\n") {
			t.Fatalf("usage line missing: %s", usage)
		}
	}
	for _, example := range contract.Examples {
		if !strings.Contains(text, "  # "+example.Description+"\n  "+example.Command+"\n") {
			t.Fatalf("example missing: %+v", example)
		}
	}
	if len(contract.Arguments) == 0 && !strings.Contains(text, "\nArguments:\n  None.\n") {
		t.Fatalf("a command without arguments says None.:\n%s", text)
	}
	for _, argument := range contract.Arguments {
		if !strings.Contains(text, "\n  "+strings.Split(strings.Join(argument.Names, ", "), " ")[0]) {
			t.Fatalf("argument %v missing", argument.Names)
		}
	}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, " ") {
			section = line
		}
		// Only Description and Arguments are wrapped; syntax, examples, and flags keep their lines.
		if (section == "Description:" || section == "Arguments:") && len(line) > 80 {
			t.Fatalf("line of %s is longer than 80 columns: %q", section, line)
		}
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	if len(lines) < 3 || !strings.HasPrefix(lines[len(lines)-2], "Contract: ") || !strings.HasSuffix(lines[len(lines)-2], "/snippets/commands/"+filepath.Base(contract.File)) {
		t.Fatalf("contract path line:\n%s", text)
	}
	root := strings.TrimSuffix(strings.TrimPrefix(lines[len(lines)-2], "Contract: "), "/"+contract.File)
	if lines[len(lines)-1] != config.CommandReadSentence(root) {
		t.Fatalf("the last line is not the read sentence:\n%s", text)
	}
}

// A subcommand shows the full contract help of its top-level command.
func TestSubcommandHelpIsTheCommandHelp(t *testing.T) {
	want := runKander(t, "server", "--help").stdout
	if got := runKander(t, "server", "start", "--help"); got.code != 0 || got.stdout != want || !strings.Contains(want, "kander server stop") {
		t.Fatalf("exit=%d\n%s", got.code, got.stdout)
	}
}

// The request help is generated from the embedded contract: it lists the note kind and never
// offers wrap-up as a message kind.
func TestRequestHelpListsTheNoteKindButNotWrapUp(t *testing.T) {
	got := runKander(t, "request", "--help")
	if got.code != 0 || !strings.Contains(got.stdout, "kander request <task-id> fix|sync|review|stop|note <message...>") {
		t.Fatalf("request help omitted the note kind: exit=%d\n%s", got.code, got.stdout)
	}
	if strings.Contains(got.stdout, "wrap-up <message") {
		t.Fatalf("request help offers wrap-up as a message kind:\n%s", got.stdout)
	}
}

// help does not follow --lang.
func TestHelpIgnoresLangSetting(t *testing.T) {
	for _, args := range [][]string{{"move", "--help"}, {"help", "move"}, {"--help"}, {"help"}, {"help", "--json", "move"}, {"help", "--json"}} {
		cn := runCLI(t, append([]string{"--lang", "cn"}, args...)...)
		en := runCLI(t, append([]string{"--lang", "en"}, args...)...)
		ja := runCLI(t, append([]string{"--lang", "ja"}, args...)...)
		if cn.code != 0 || cn.stdout != en.stdout || en.stdout != ja.stdout || cn.stderr != en.stderr {
			t.Fatalf("%v differs between languages\ncn: %q\nen: %q", args, cn.stdout, en.stdout)
		}
	}
}

// The top-level help names every visible command with its summary; the worker is not named.
func TestTopLevelHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"help"}, {"-h"}} {
		result := runKander(t, args...)
		if result.code != 0 || !strings.HasPrefix(result.stdout, "Kander: kanban orchestration for coding agents.\n") || strings.Contains(result.stdout, "worker") || strings.Contains(result.stdout, "\x1b") {
			t.Fatalf("%v: exit=%d stdout=%q", args, result.code, result.stdout)
		}
		for _, name := range rules.CommandOrder() {
			contract, err := rules.EmbeddedContract(name)
			if err != nil {
				t.Fatal(err)
			}
			label := name
			if name == "list" {
				label = "list, ls"
			}
			if !strings.Contains(result.stdout, "  "+label+" ") {
				t.Errorf("%v: the command %q is not listed", args, label)
			}
			if !strings.Contains(strings.Join(strings.Fields(result.stdout), " "), contract.Summary) {
				t.Errorf("%v: the summary of %s is missing", args, name)
			}
		}
		for _, want := range []string{"--lang {cn,en,ja}", "--json", `kander help --json [<command>]`} {
			if !strings.Contains(result.stdout, want) {
				t.Errorf("%v: missing %q", args, want)
			}
		}
	}
	if result := runKander(t, "help", "nosuchcommand"); result.code != 2 {
		t.Fatalf("unknown help topic: exit=%d stderr=%q", result.code, result.stderr)
	}
}

// The commands of the tree are the commands of the order the help lists, and the other way around.
func TestHelpListsTheCommandsOfTheTree(t *testing.T) {
	var tree []string
	for _, c := range cli.NewRoot().Commands() {
		if !c.Hidden && c.Name() != "help" {
			tree = append(tree, c.Name())
		}
	}
	if order := rules.CommandOrder(); !slices.Equal(slices.Sorted(slices.Values(order)), slices.Sorted(slices.Values(tree))) {
		t.Fatalf("tree %v\norder %v", slices.Sorted(slices.Values(tree)), slices.Sorted(slices.Values(order)))
	}
}

// decodeObject decodes the one JSON object a command printed.
func decodeObject(t *testing.T, stdout string) map[string]any {
	t.Helper()
	var object map[string]any
	if err := json.Unmarshal([]byte(stdout), &object); err != nil || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("not one JSON object: %v\n%q", err, stdout)
	}
	return object
}

// typeCheck reports a mismatch between value and the JSON type want ("string", "array", ...).
func typeCheck(t *testing.T, where string, value any, want string) {
	t.Helper()
	ok := false
	switch want {
	case "string":
		_, ok = value.(string)
	case "number":
		_, ok = value.(float64)
	case "array":
		_, ok = value.([]any)
	}
	if !ok {
		t.Errorf("%s: want a JSON %s, got %#v", where, want, value)
	}
}

// checkObject checks that object has exactly the fields of schema, with their types.
func checkObject(t *testing.T, where string, object map[string]any, schema map[string]string) {
	t.Helper()
	if got, want := slices.Sorted(maps.Keys(object)), slices.Sorted(maps.Keys(schema)); !slices.Equal(got, want) {
		t.Errorf("%s: fields %v, want %v", where, got, want)
		return
	}
	for name, kind := range schema {
		typeCheck(t, where+"."+name, object[name], kind)
	}
}

func checkItems(t *testing.T, where string, value any, schema map[string]string) {
	t.Helper()
	items, _ := value.([]any)
	for i, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			t.Errorf("%s[%d]: not an object", where, i)
			continue
		}
		checkObject(t, where, object, schema)
	}
}

// For every command path, `<path> --help --json` and `help --json <path>` are the same object, in
// the schema of docs/command-help.md.
func TestHelpJSONOfEveryCommandFollowsTheSchema(t *testing.T) {
	for _, leaf := range visibleLeaves(t) {
		t.Run(leaf.path, func(t *testing.T) {
			path := strings.Fields(leaf.path)
			flagForm := runKander(t, append(slices.Clone(path), "--help", "--json")...)
			commandForm := runKander(t, append([]string{"help", "--json"}, path...)...)
			if flagForm.code != 0 || commandForm.code != 0 || flagForm.stdout != commandForm.stdout {
				t.Fatalf("exit=%d/%d\n%s\n%s", flagForm.code, commandForm.code, flagForm.stdout, commandForm.stdout)
			}
			object := decodeObject(t, flagForm.stdout)
			withoutNotices(t, object)
			checkObject(t, "help", object, map[string]string{
				"schema_version": "number", "command": "string", "aliases": "array", "summary": "string", "description": "string",
				"usage": "array", "arguments": "array", "examples": "array", "subcommands": "array", "global_flags": "array",
				"contract": "string", "read_sentence": "string",
			})
			checkItems(t, "arguments", object["arguments"], map[string]string{"names": "array", "text": "string"})
			checkItems(t, "examples", object["examples"], map[string]string{"description": "string", "command": "string"})
			checkItems(t, "subcommands", object["subcommands"], map[string]string{"name": "string", "usage": "array"})
			checkItems(t, "global_flags", object["global_flags"], map[string]string{"name": "string", "text": "string"})
			contract, _ := rules.EmbeddedContract(contractOf(leaf.path))
			if object["schema_version"] != float64(1) || object["command"] != contractOf(leaf.path) || object["summary"] != contract.Summary || len(object["usage"].([]any)) != len(contract.Usage) {
				t.Errorf("content: %v", object)
			}
			if (len(contract.Arguments) == 0) != (len(object["arguments"].([]any)) == 0) || len(object["examples"].([]any)) != len(contract.Examples) {
				t.Errorf("arguments or examples differ from the contract: %v", object)
			}
		})
	}
}

// The subcommands of a command with subcommands carry their usage lines.
func TestHelpJSONListsSubcommands(t *testing.T) {
	object := decodeObject(t, runKander(t, "help", "--json", "server").stdout)
	var names []string
	for _, item := range object["subcommands"].([]any) {
		sub := item.(map[string]any)
		names = append(names, sub["name"].(string))
		if len(sub["usage"].([]any)) == 0 {
			t.Errorf("subcommand %v has no usage", sub["name"])
		}
	}
	if want := []string{"start", "stop", "restart", "status", "import", "maintain", "projects"}; !slices.Equal(names, want) {
		t.Fatalf("subcommands %v, want %v", names, want)
	}
	if plain := decodeObject(t, runKander(t, "help", "--json", "version").stdout); len(plain["subcommands"].([]any)) != 0 {
		t.Fatalf("version has subcommands: %v", plain["subcommands"])
	}
}

// `help --json` and `--help --json` are the same index of every command.
func TestHelpJSONIndex(t *testing.T) {
	first := runKander(t, "help", "--json")
	second := runKander(t, "--help", "--json")
	if first.code != 0 || first.stdout != second.stdout {
		t.Fatalf("exit=%d\n%s\n%s", first.code, first.stdout, second.stdout)
	}
	object := decodeObject(t, first.stdout)
	withoutNotices(t, object)
	checkObject(t, "index", object, map[string]string{"schema_version": "number", "commands": "array", "global_flags": "array"})
	checkItems(t, "commands", object["commands"], map[string]string{"name": "string", "aliases": "array", "summary": "string", "contract": "string"})
	var names []string
	for _, item := range object["commands"].([]any) {
		names = append(names, item.(map[string]any)["name"].(string))
	}
	if !slices.Equal(names, rules.CommandOrder()) {
		t.Fatalf("commands %v, want %v", names, rules.CommandOrder())
	}
}

// withoutNotices checks the optional notices of a help object and removes them: they appear
// only when the installed contract is missing or differs, which depends on the machine.
func withoutNotices(t *testing.T, object map[string]any) {
	t.Helper()
	if notices, ok := object["notices"]; ok {
		checkItems(t, "notices", notices, map[string]string{"code": "string", "message": "string"})
		delete(object, "notices")
	}
}

// The notices of help are fixed English and name the one thing to do; an installed contract
// that matches the binary raises none.
func TestHelpNoticesAreFixedEnglish(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	paths, err := config.CurrentInstallPaths()
	if err != nil || paths.RulesDir == "" {
		t.Skipf("no rules root to compare: %v", err)
	}
	cn := runCLI(t, "--lang", "cn", "help", "--json", "move")
	en := runCLI(t, "--lang", "en", "help", "--json", "move")
	if cn.stdout != en.stdout {
		t.Fatalf("notices follow --lang:\n%s\n%s", cn.stdout, en.stdout)
	}
	notices, _ := decodeObject(t, en.stdout)["notices"].([]any)
	if len(notices) != 1 || notices[0].(map[string]any)["code"] != "contract_missing" || !strings.Contains(notices[0].(map[string]any)["message"].(string), "kander doctor") {
		t.Fatalf("a missing installed contract: %v", notices)
	}
	if text := runKander(t, "move", "--help"); !strings.Contains(text.stderr, "kander doctor") || strings.Contains(text.stdout, "kander doctor") {
		t.Fatalf("text help: stdout=%q stderr=%q", text.stdout, text.stderr)
	}
	// An installed copy that differs is reported, and one that matches is not.
	embedded, _ := rules.File("snippets/commands/move.md")
	installed := filepath.Join(paths.RulesDir, "snippets", "commands", "move.md")
	if err := os.MkdirAll(filepath.Dir(installed), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(installed, append(slices.Clone(embedded), "\nlocal edit\n"...), 0o600); err != nil {
		t.Fatal(err)
	}
	notices, _ = decodeObject(t, runKander(t, "help", "--json", "move").stdout)["notices"].([]any)
	if len(notices) != 1 || notices[0].(map[string]any)["code"] != "contract_outdated" {
		t.Fatalf("an outdated installed contract: %v", notices)
	}
	if err := os.WriteFile(installed, embedded, 0o600); err != nil {
		t.Fatal(err)
	}
	if object := decodeObject(t, runKander(t, "help", "--json", "move").stdout); object["notices"] != nil {
		t.Fatalf("a matching installed contract: %v", object["notices"])
	}
}

// splitCommandLine splits an example command line at spaces; a double-quoted part is one word.
func splitCommandLine(line string) []string {
	var words []string
	var word strings.Builder
	quoted, started := false, false
	for _, r := range line {
		switch {
		case r == '"':
			quoted, started = !quoted, true
		case r == ' ' && !quoted:
			if started {
				words = append(words, word.String())
				word.Reset()
				started = false
			}
		default:
			word.WriteRune(r)
			started = true
		}
	}
	if started {
		words = append(words, word.String())
	}
	return words
}

// Every example of every contract starts with the command path it belongs to, parses against the
// command tree without an argument error, and every command path has an example.
func TestEveryExampleParsesAndEveryPathHasOne(t *testing.T) {
	covered := map[string]bool{}
	root := cli.NewRoot()
	for _, command := range rules.CommandOrder() {
		contract, err := rules.EmbeddedContract(command)
		if err != nil {
			t.Fatal(err)
		}
		for _, example := range contract.Examples {
			words := splitCommandLine(example.Command)
			if len(words) < 2 || words[0] != "kander" {
				t.Errorf("%s: example %q does not start with kander", command, example.Command)
				continue
			}
			target, _, err := root.Find(words[1:])
			if err != nil || contractOf(strings.TrimPrefix(target.CommandPath(), "kander ")) != command {
				t.Errorf("%s: example %q does not belong to the command (%v)", command, example.Command, err)
				continue
			}
			path := strings.TrimPrefix(target.CommandPath(), "kander ")
			covered[path] = true
			if !strings.HasPrefix(example.Command, "kander "+path) {
				t.Errorf("%s: example %q does not start with `kander %s`", command, example.Command, path)
			}
			// The words of the example stay POSIX in the contract; the run needs the paths of this platform.
			args := slices.Clone(words[1:])
			for i, word := range args {
				args[i] = platformAbs(t, word)
			}
			if code, _, stderr := runStubbed(t, args...); code != 0 {
				t.Errorf("example %q: exit=%d %s", example.Command, code, stderr)
			}
		}
	}
	for _, leaf := range visibleLeaves(t) {
		if !covered[leaf.path] {
			t.Errorf("no example for `kander %s`", leaf.path)
		}
	}
}
