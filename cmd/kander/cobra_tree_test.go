package main

import (
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/dualface/kander/v2/internal/cli"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/servicectl"
	"github.com/dualface/kander/v2/rules"
)

const sampleTask = "20261118-sample-task"

// groups are the commands that only group subcommands.
var groups = map[string]bool{"coordinator": true, "decision": true, "dispatch": true, "issue": true, "server": true, "terminal": true}

// leafCommand is a command that runs something, with its command path.
type leafCommand struct {
	*cobra.Command
	path string
}

// commandLeaves lists every command of the tree that performs an operation: the leaves, and the
// commands that take arguments of their own besides subcommands (check, request, review).
func commandLeaves(root *cobra.Command) []leafCommand {
	var leaves []leafCommand
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, child := range c.Commands() {
			if child.Name() == "help" {
				continue
			}
			// A command that takes its command line verbatim (the Windows job bootstrap of review)
			// has no flags to parse.
			if child.Runnable() && !groups[child.Name()] && !child.DisableFlagParsing {
				leaves = append(leaves, leafCommand{child, strings.TrimPrefix(child.CommandPath(), "kander ")})
			}
			walk(child)
		}
	}
	walk(root)
	sort.Slice(leaves, func(i, j int) bool { return leaves[i].path < leaves[j].path })
	return leaves
}

// samplePositionals are valid positional arguments for each command path.
var samplePositionals = map[string][]string{
	"assign":                       {sampleTask, "Example User"},
	"check":                        {},
	"check delivery":               {},
	"check overlap":                {},
	"config":                       {},
	"confirm":                      {sampleTask},
	"coordinator claim":            {"/abs/claim.json"},
	"coordinator integrate":        {"/abs/integration.json"},
	"coordinator integration-show": {"refs/heads/main"},
	"coordinator recover":          {"/abs/request.json"},
	"coordinator reconcile":        {"/abs/observations.json"},
	"coordinator release":          {"/abs/release.json"},
	"coordinator remove-member":    {"/abs/request.json"},
	"coordinator show":             {"20261118-sample-group"},
	"decision list":                {sampleTask},
	"decision mode":                {sampleTask, "leader"},
	"decision record":              {sampleTask},
	"dismiss":                      {sampleTask},
	"dispatch authorize-wrap-up":   {"/abs/request.json"},
	"dispatch cancel":              {sampleTask, "d1", "3", "reason"},
	"dispatch fail":                {sampleTask, "d1", "3", "reason"},
	"dispatch prepare":             {"/abs/intent.json"},
	"dispatch settle":              {sampleTask, "d1", "3"},
	"dispatch show":                {sampleTask, "d1"},
	"doctor":                       {},
	"guard-write":                  {"main.go"},
	"gc":                           {},
	"init":                         {},
	"install":                      {},
	"issue import":                 {"42"},
	"issue list":                   {},
	"issue repo":                   {},
	"issue result":                 {"42"},
	"issue show":                   {"42"},
	"issue triage":                 {"42"},
	"list":                         {},
	"move":                         {sampleTask, "todo"},
	"new":                          {"chore", "slug", "A", "title"},
	"notify":                       {sampleTask},
	"orchestrate":                  {sampleTask},
	"pick":                         {sampleTask},
	"request":                      {sampleTask, "fix", "please"},
	"request answer":               {sampleTask, "request-1", "done", "noted"},
	"request list":                 {sampleTask},
	"request show":                 {sampleTask, "request-1"},
	"resume":                       {sampleTask},
	"review":                       {"/abs/repo", "base", "commit", "PMQA", "goal"},
	"review abandon":               {"/abs/repo", "/abs/abandon.json"},
	"review advance":               {"/abs/repo", "/abs/advance.json"},
	"review aggregate":             {"/abs/repo", "batch-1"},
	"review assign":                {"/abs/repo", "/abs/assign.json"},
	"review close":                 {"/abs/repo", "/abs/close.json"},
	"review confirm":               {"/abs/repo", "publication-1", sampleTask},
	"review disposition":           {"/abs/repo", "/abs/record.json", "3"},
	"review extend-plan":           {"/abs/repo", "/abs/extension.json"},
	"review interpret":             {"/abs/repo", "/abs/interpretation.json"},
	"review map-legacy":            {"/abs/repo", "/abs/map.json"},
	"review plan":                  {"/abs/repo", "/abs/plan.json"},
	"review progress":              {"/abs/repo", sampleTask},
	"review publication":           {"/abs/repo", "publication-1"},
	"server gc":                    {},
	"server import":                {},
	"server maintain":              {},
	"server projects":              {},
	"server restart":               {},
	"server start":                 {},
	"server status":                {},
	"server stop":                  {},
	"show":                         {sampleTask},
	"start":                        {},
	"subscribe":                    {"20261118-sample-group", sampleTask},
	"terminal list":                {},
	"terminal test":                {"herdr"},
	"uninstall":                    {},
	"update":                       {sampleTask},
	"version":                      {},
	"worker":                       {},
}

// sampleValues are valid values for each value flag; every name is shared by the commands that
// declare it.
var sampleValues = map[string]string{
	"action": "inspect", "advance-file": "advance.json", "agent": "claude", "assignee": "alice", "base": "abc123", "batch-id": "batch-1",
	"card": sampleTask, "commit": "HEAD", "contract-decision-file": "decision.md", "cursor": "opaque", "data": "/tmp/data", "decision": "ref-1",
	"delivery-commit": "abc123", "description-file": "description.md", "disposition": "reviews/a.json", "dispatch-id": "dispatch-1", "document": "plan.md",
	"duplicate-of": sampleTask, "evidence-file": "evidence.json", "execution-epoch": "2", "expect-revision": "3",
	"file": "input.md", "from": "/tmp/board", "group": "20261125-example-group", "head": "HEAD", "heartbeat": "5", "kind": "fix", "label": "bug",
	"language": "en", "launcher": "herdr", "limit": "5", "listen": "127.0.0.1:7878", "log-keep": "64MiB", "message": "hello",
	"message-file": "message.md", "older-than": "7d", "operation-id": "operation-1", "owner": "codex", "pane": "pane-1", "previous-run-id": "run-0",
	"project": "demo", "reason": "because", "refresh": "2", "remote": "https://example.com/r.git", "renew": "10s",
	"repo": "github.com/owner/repo", "requirements-file": "requirements.json", "result": "completed", "run-id": "run-1",
	"search": "query", "server": "http://127.0.0.1:7878", "source": "HEAD", "state": "open", "submitter": "bob", "task": sampleTask,
	"timeout": "5", "ttl": "60s", "type": "feature", "watch": sampleTask,
}

func sampleValue(path, flag string) (string, bool) {
	if path == "decision record" && flag == "kind" {
		return "leader", true
	}
	value, ok := sampleValues[flag]
	return value, ok
}

// valueFlags are the flags of c that take a value, in name order.
func valueFlags(c *cobra.Command) []*pflag.Flag {
	var flags []*pflag.Flag
	c.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name != "help" && f.NoOptDefVal == "" {
			flags = append(flags, f)
		}
	})
	return flags
}

// requiredArguments are the flags a complete command line of leaf needs, without the flag
// named skip: the checks that run before the command body refuse a line that lacks one.
func requiredArguments(leaf leafCommand, skip string) []string {
	var out []string
	flags := requiredFlags[leaf.path]
	for i := 0; i+1 < len(flags); i += 2 {
		if flags[i] != "--"+skip && flags[i] != "--"+replacedBy[skip] {
			out = append(out, flags[i], flags[i+1])
		}
	}
	if companion := companionFlags[skip]; len(companion) > 0 && leaf.Flags().Lookup(strings.TrimPrefix(companion[0], "--")) != nil {
		out = append(out, companion...)
	}
	return out
}

// replacedBy names the flag a required flag gives way to when the other form is the one tested:
// the text of a message comes from --message or from --message-file, never both.
var replacedBy = map[string]string{"message-file": "message"}

// companionFlags are the flags that a flag under test needs next to it.
var companionFlags = map[string][]string{
	"cursor":          {"--limit", "5"},
	"dispatch-id":     {"--execution-epoch", "2"},
	"execution-epoch": {"--dispatch-id", "d1"},
}

func sampleArguments(t *testing.T, leaf leafCommand) []string {
	t.Helper()
	positionals, ok := samplePositionals[leaf.path]
	if !ok {
		t.Fatalf("no sample arguments for %q", leaf.path)
	}
	positionals = slices.Clone(positionals)
	if strings.HasPrefix(leaf.path, "coordinator ") {
		// The coordinator commands refuse a path that is not absolute on this platform.
		for i, word := range positionals {
			positionals[i] = platformAbs(t, word)
		}
	}
	return positionals
}

// platformAbs turns a word that starts with a slash, such as /abs/claim.json, into an absolute path
// of this platform through filepath.FromSlash and filepath.Abs, so the result is also cleaned.
// The coordinator commands check paths with filepath.IsAbs, which on Windows does not accept a
// single-slash path that has no volume name, so a literal /abs/x.json would be refused as a usage
// error before the branch under test. A word that does not start with a slash is returned as it
// is. On POSIX a clean path keeps its text, which holds for every sample used here.
func platformAbs(t *testing.T, word string) string {
	t.Helper()
	if !strings.HasPrefix(word, "/") {
		return word
	}
	abs, err := filepath.Abs(filepath.FromSlash(word))
	if err != nil {
		t.Fatal(err)
	}
	return abs
}

// invocation is what a stubbed command saw.
type invocation struct {
	path  string
	args  []string
	flags map[string]string
}

// runStubbed runs args against a fresh tree whose commands only record themselves: the parsing,
// the argument validation and the checks before the command body are the real ones, and the
// body and the project gate are not.
func runStubbed(t *testing.T, args ...string) (code int, got *invocation, stderr string) {
	t.Helper()
	root := cli.NewRoot()
	commandtest.Stub(root, func(c *cobra.Command, args []string) {
		got = &invocation{path: c.CommandPath(), args: slices.Clone(args), flags: map[string]string{}}
		c.Flags().Visit(func(f *pflag.Flag) { got.flags[f.Name] = f.Value.String() })
	})
	code, _, stderr = commandtest.Capture(func() int { return command.Execute(root, args) })
	return code, got, stderr
}

func TestCommandTreeHasEveryCommandPath(t *testing.T) {
	want := slices.Sorted(maps.Keys(samplePositionals))
	var got []string
	for _, leaf := range commandLeaves(cli.NewRoot()) {
		got = append(got, leaf.path)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("command paths differ\n got: %q\nwant: %q", got, want)
	}
	root := cli.NewRoot()
	if cmd, _, err := root.Find([]string{"ls"}); err != nil || cmd.Name() != "list" {
		t.Fatalf("alias ls: %v %v", cmd, err)
	}
	if cmd, _, err := root.Find([]string{"worker"}); err != nil || !cmd.Hidden {
		t.Fatalf("worker must be a hidden command: %v %v", cmd, err)
	}
}

// --name value and --name=value mean the same.
func TestValueFlagsAcceptSpaceAndEqualsForms(t *testing.T) {
	for _, leaf := range commandLeaves(cli.NewRoot()) {
		for _, f := range valueFlags(leaf.Command) {
			value, ok := sampleValue(leaf.path, f.Name)
			if !ok {
				t.Fatalf("%s: no sample value for --%s", leaf.path, f.Name)
			}
			t.Run(leaf.path+"/"+f.Name, func(t *testing.T) {
				base := append(append(strings.Fields(leaf.path), sampleArguments(t, leaf)...), requiredArguments(leaf, f.Name)...)
				spaced, spacedGot, spacedErr := runStubbed(t, append(slices.Clone(base), "--"+f.Name, value)...)
				joined, joinedGot, joinedErr := runStubbed(t, append(slices.Clone(base), "--"+f.Name+"="+value)...)
				if spaced != 0 || joined != 0 {
					t.Fatalf("exit %d / %d\n%s\n%s", spaced, joined, spacedErr, joinedErr)
				}
				if !maps.Equal(spacedGot.flags, joinedGot.flags) || !slices.Equal(spacedGot.args, joinedGot.args) || spacedGot.flags[f.Name] == "" {
					t.Fatalf("spaced %+v differs from joined %+v", spacedGot, joinedGot)
				}
			})
		}
	}
}

// A flag may follow the positional arguments.
func TestFlagsMayFollowPositionalArguments(t *testing.T) {
	for _, leaf := range commandLeaves(cli.NewRoot()) {
		positionals := sampleArguments(t, leaf)
		flags := valueFlags(leaf.Command)
		if len(positionals) == 0 || len(flags) == 0 {
			continue
		}
		t.Run(leaf.path, func(t *testing.T) {
			f := flags[0]
			path := strings.Fields(leaf.path)
			required := requiredArguments(leaf, f.Name)
			value, _ := sampleValue(leaf.path, f.Name)
			before := append(append(append(slices.Clone(path), required...), "--"+f.Name, value), positionals...)
			after := append(append(append(slices.Clone(path), positionals...), required...), "--"+f.Name, value)
			a, aGot, aErr := runStubbed(t, before...)
			b, bGot, bErr := runStubbed(t, after...)
			if a != 0 || b != 0 {
				t.Fatalf("exit %d / %d\n%s\n%s", a, b, aErr, bErr)
			}
			if !maps.Equal(aGot.flags, bGot.flags) || !slices.Equal(aGot.args, bGot.args) {
				t.Fatalf("flag before %+v differs from flag after %+v", aGot, bGot)
			}
		})
	}
}

// After --, a word that starts with a dash is a positional argument.
func TestWordsAfterDoubleDashArePositional(t *testing.T) {
	for _, leaf := range commandLeaves(cli.NewRoot()) {
		positionals := sampleArguments(t, leaf)
		if len(positionals) == 0 {
			continue
		}
		t.Run(leaf.path, func(t *testing.T) {
			dashed := slices.Clone(positionals)
			dashed[len(dashed)-1] = "-dash-word"
			code, got, stderr := runStubbed(t, append(append(append(strings.Fields(leaf.path), requiredArguments(leaf, "")...), "--"), dashed...)...)
			if code == 0 {
				if got == nil || !slices.Equal(got.args, dashed) {
					t.Fatalf("arguments=%+v", got)
				}
				return
			}
			// The command validated the value itself; the word was never read as a flag.
			if strings.Contains(stderr, "unknown option") || code != 2 {
				t.Fatalf("exit=%d stderr=%s", code, stderr)
			}
		})
	}
}

func TestUnknownFlagIsAnArgumentError(t *testing.T) {
	for _, leaf := range commandLeaves(cli.NewRoot()) {
		t.Run(leaf.path, func(t *testing.T) {
			args := append(append(append(strings.Fields(leaf.path), sampleArguments(t, leaf)...), requiredArguments(leaf, "")...), "--no-such-flag")
			code, _, stderr := runStubbed(t, args...)
			if code != 2 || !strings.Contains(stderr, "--no-such-flag") {
				t.Fatalf("exit=%d stderr=%s", code, stderr)
			}
		})
	}
}

// The error report names the command's contract Syntax lines and follows --lang.
func TestArgumentErrorsAreLocalizedAndShowTheSyntax(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		cn   string
		en   string
	}{
		{"unknown flag", []string{"show", "--no-such-flag"}, "未知选项 --no-such-flag", "unknown option --no-such-flag"},
		{"missing value", []string{"show", sampleTask, "--document"}, "选项 --document 需要一个值", "option --document requires a value"},
		{"argument count", []string{"show"}, "需要 1 个参数，实际给出 0 个", "expected 1 argument(s), got 0"},
	} {
		for language, want := range map[string]string{"cn": test.cn, "en": test.en} {
			t.Run(test.name+"/"+language, func(t *testing.T) {
				result := runCLI(t, append([]string{"--lang", language}, test.args...)...)
				code, stderr := result.code, result.stderr
				if code != 2 || !strings.Contains(stderr, want) || !strings.Contains(stderr, "kander show [--document <relative-path>] <task-id>") {
					t.Fatalf("exit=%d stderr=%s", code, stderr)
				}
			})
		}
	}
}

// A usage error the command contract names stays an argument error.
func TestContractUsageErrorsExitTwo(t *testing.T) {
	for _, args := range [][]string{
		{"show", sampleTask, "--document", "a.md", "--document", "b.md"},
		{"list", "--agent-status"},
		{"move", sampleTask, "done", "--dispatch-id", "d1"},
		{"update", sampleTask, "--document", "spec.md"},
		{"resume", "--adopt", sampleTask},
		{"resume", "--release", "--decision", "x", "--message", "m", sampleTask},
		{"coordinator", "recover", "relative.json"},
		{"subscribe", "20261118-sample-group"},
	} {
		if result := runKander(t, args...); result.code != 2 {
			t.Errorf("%v: exit=%d stderr=%s", args, result.code, result.stderr)
		}
	}
}

func TestRemovedAndRetiredForms(t *testing.T) {
	for _, test := range []struct {
		args []string
		code int
	}{
		{[]string{"init", "--maintenance"}, 2},
		{[]string{"list", "--mobile"}, 2},
		{[]string{"server", "start", "-data", "x"}, 2},
		{[]string{"completion"}, 2},
		{[]string{"completion", "bash"}, 2},
		{[]string{"worker", "extra"}, 2},
		{[]string{"worker", "--bogus"}, 2},
		{[]string{"--lang", "fr", "version"}, 2},
		{[]string{"nosuchcommand"}, 2},
		{[]string{"dispatch"}, 2},
		{[]string{"dispatch", "nosuch"}, 2},
	} {
		if result := runKander(t, test.args...); result.code != test.code {
			t.Errorf("%v: exit=%d stderr=%s", test.args, result.code, result.stderr)
		}
	}
	help := runCLI(t, "worker", "--help")
	if help.code != 0 || strings.Count(strings.TrimSpace(help.stdout), "\n") != 0 || !strings.Contains(help.stdout, "internally") {
		t.Fatalf("worker --help: exit=%d stdout=%q", help.code, help.stdout)
	}
}

var syntaxFlag = regexp.MustCompile(`--[a-z][a-z-]*`)

// contractPaths maps each command path a contract Syntax line names to the long flags the
// lines of that path name together.
func contractPaths(t *testing.T) map[string]map[string]bool {
	t.Helper()
	files, err := filepath.Glob(filepath.Join("..", "..", "rules", "snippets", "commands", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("contracts: %v %v", files, err)
	}
	paths := map[string]map[string]bool{}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if filepath.Base(file) == "README.md" {
			continue
		}
		for _, line := range rules.CommandSyntaxLines(string(data)) {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "kander ") {
				continue
			}
			for _, path := range rules.SyntaxPaths(line) {
				if paths[path] == nil {
					paths[path] = map[string]bool{}
				}
				for _, flag := range syntaxFlag.FindAllString(line, -1) {
					paths[path][flag] = true
				}
			}
		}
	}
	return paths
}

// The flags a command registers are the flags its contract Syntax lines name. This replaces the
// scan of the hand-written parsers the contracts used to be checked against.
func TestRegisteredFlagsMatchContractSyntax(t *testing.T) {
	contract := contractPaths(t)
	leaves := map[string]leafCommand{}
	for _, leaf := range commandLeaves(cli.NewRoot()) {
		leaves[leaf.path] = leaf
	}
	for path, flags := range contract {
		// --json and --lang are global flags of the root; no Syntax line names them.
		if flags["--json"] || flags["--lang"] {
			t.Errorf("the contract Syntax of `kander %s` names a global flag", path)
		}
		leaf, ok := leaves[path]
		if !ok {
			t.Errorf("the contract names `kander %s`, which the command tree does not have", path)
			continue
		}
		registered := map[string]bool{}
		leaf.NonInheritedFlags().VisitAll(func(f *pflag.Flag) {
			if f.Name != "help" {
				registered["--"+f.Name] = true
			}
		})
		if global := leaf.InheritedFlags().Lookup("json"); global == nil {
			t.Errorf("kander %s does not inherit the global --json", path)
		}
		if !maps.Equal(registered, flags) {
			t.Errorf("kander %s: registered %v, contract %v", path, slices.Sorted(maps.Keys(registered)), slices.Sorted(maps.Keys(flags)))
		}
	}
	for path, leaf := range leaves {
		if _, ok := contract[path]; !ok && !leaf.Hidden {
			t.Errorf("no contract Syntax line names `kander %s`", path)
		}
	}
}

// The command lines Kander starts itself are the arguments the code builds, so they are taken
// from the functions that build them. The command lines of the prompts are tested next to the
// renderers that produce them, in internal/launch.
func TestGeneratedCommandLinesParse(t *testing.T) {
	dir := t.TempDir()
	for name, args := range map[string][]string{
		"server start":  servicectl.ServerStartArguments(dir, servicectl.StartOptions{Listen: "127.0.0.1:7878", TTL: 90e9, Renew: 30e9}),
		"worker":        remote.WorkerArguments(),
		"version probe": command.VersionArguments(),
	} {
		t.Run(name, func(t *testing.T) { assertParses(t, args) })
	}
}

// Free text that starts with a dash parses when it follows --, or is the value of a flag.
func TestFreeTextThatStartsWithADashParses(t *testing.T) {
	for _, line := range []string{
		"notify --message -leading --dispatch-id dispatch-1 --kind fix --base abc --evidence-file e.json " + sampleTask,
		"resume --agent claude --timeout 61 --message -leading --decision -x " + sampleTask,
		"orchestrate --message -leading " + sampleTask,
		"new chore slug -- -leading title",
		"request " + sampleTask + " fix -- -leading message",
		"request answer " + sampleTask + " request-1 done -- -leading note",
		"dispatch fail " + sampleTask + " d1 3 -- -leading-reason",
	} {
		assertParses(t, strings.Fields(line))
	}
}

func assertParses(t *testing.T, args []string) {
	t.Helper()
	if code, _, stderr := runStubbed(t, args...); code != 0 {
		t.Errorf("kander %s: exit=%d %s", strings.Join(args, " "), code, stderr)
	}
}

// Every top-level command has a contract, and every contract documents a command of the tree.
func TestEveryCommandHasAContractAndTheOtherWayAround(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "rules", "snippets", "commands", "*.md"))
	if err != nil {
		t.Fatal(err)
	}
	contracts := map[string]bool{}
	for _, file := range files {
		if name := strings.TrimSuffix(filepath.Base(file), ".md"); name != "README" {
			contracts[name] = true
		}
	}
	commands := map[string]bool{}
	for _, c := range cli.NewRoot().Commands() {
		// help is cobra's own, worker is internal, and request is documented in notify.md.
		if c.Hidden || c.Name() == "help" || c.Name() == "request" {
			continue
		}
		commands[c.Name()] = true
	}
	if !maps.Equal(commands, contracts) {
		t.Fatalf("commands %v\ncontracts %v", slices.Sorted(maps.Keys(commands)), slices.Sorted(maps.Keys(contracts)))
	}
}

// The internal job bootstrap of review receives a reviewer command line, which must reach it
// without being read as flags.
func TestJobBootstrapTakesItsCommandLineVerbatim(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the bootstrap runs a real job object on Windows")
	}
	// The bootstrap is a stub on POSIX that exits 125; an argument error would exit 2.
	if result := runKander(t, "review", "job-bootstrap", "event", "codex", "--model", "x", "--unknown", "--", "-y"); result.code != 125 {
		t.Fatalf("exit=%d stderr=%s", result.code, result.stderr)
	}
}
