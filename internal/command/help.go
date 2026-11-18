package command

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/rules"
)

// helpSchemaVersion is the schema_version of the help JSON objects (docs/command-help.md).
const helpSchemaVersion = 1

// helpWidth is the fixed line width of the wrapped help text; it does not follow the terminal,
// so the output is the same everywhere.
const helpWidth = 80

// Help is always English and never colored: nothing below follows --lang.
const (
	helpTagline = "Kander: kanban orchestration for coding agents."
	rootUsage   = "Usage:\n  kander [--lang {cn,en,ja}] [--json] <command> [args...]\n  kander                      Open the terminal board"
	rootHint    = `Run "kander <command> --help" for details, or "kander help --json [<command>]" for JSON.`
)

type flagHelp struct {
	Name string `json:"name"`
	Text string `json:"text"`
}

var globalFlags = []flagHelp{
	{"--lang {cn,en,ja}", "Interface language for command output (help is always English)"},
	{"--json", "Print structured JSON output"},
	{"-h, --help", "Show help for the command"},
}

type subcommandHelp struct {
	Name  string   `json:"name"`
	Usage []string `json:"usage"`
}

// commandHelp is the help of one top-level command: the object of `kander <command> --help
// --json`, and the source of the text help.
type commandHelp struct {
	SchemaVersion int              `json:"schema_version"`
	Command       string           `json:"command"`
	Aliases       []string         `json:"aliases"`
	Summary       string           `json:"summary"`
	Description   string           `json:"description"`
	Usage         []string         `json:"usage"`
	Arguments     []rules.Argument `json:"arguments"`
	Examples      []rules.Example  `json:"examples"`
	Subcommands   []subcommandHelp `json:"subcommands"`
	GlobalFlags   []flagHelp       `json:"global_flags"`
	Contract      string           `json:"contract"`
	ReadSentence  string           `json:"read_sentence"`
}

type indexEntry struct {
	Name     string   `json:"name"`
	Aliases  []string `json:"aliases"`
	Summary  string   `json:"summary"`
	Contract string   `json:"contract"`
}

// indexHelp is the object of `kander help --json`: every command with its summary.
type indexHelp struct {
	SchemaVersion int          `json:"schema_version"`
	Commands      []indexEntry `json:"commands"`
	GlobalFlags   []flagHelp   `json:"global_flags"`
}

// topLevel is the command below the root that owns the contract c is documented in:
// `server start` is documented by `server`.
func topLevel(c *cobra.Command) *cobra.Command {
	for c.HasParent() && c.Parent().HasParent() {
		c = c.Parent()
	}
	return c
}

// buildCommandHelp reads the embedded contract of the top-level command c.
func buildCommandHelp(c *cobra.Command) (commandHelp, error) {
	contract, err := rules.EmbeddedContract(c.Name())
	if err != nil {
		return commandHelp{}, err
	}
	root, _ := config.ContractRoot()
	help := commandHelp{
		SchemaVersion: helpSchemaVersion,
		Command:       c.Name(),
		Aliases:       nonNil(c.Aliases),
		Summary:       contract.Summary,
		Description:   contract.Description,
		Usage:         nonNil(contract.Usage),
		Arguments:     nonNil(contract.Arguments),
		Examples:      nonNil(contract.Examples),
		Subcommands:   subcommands(c.Name(), contract.Usage),
		GlobalFlags:   globalFlags,
		Contract:      root + "/" + contract.File,
		ReadSentence:  config.CommandReadSentence(root),
	}
	return help, nil
}

func nonNil[T any](items []T) []T {
	if items == nil {
		return []T{}
	}
	return items
}

// subcommands groups the usage lines of command by the subcommand they name, in the order of
// the contract.
func subcommands(command string, usage []string) []subcommandHelp {
	subs := []subcommandHelp{}
	for _, line := range usage {
		for _, path := range rules.SyntaxPaths(line) {
			words := strings.Fields(path)
			if len(words) < 2 || words[0] != command {
				continue
			}
			found := false
			for i := range subs {
				if subs[i].Name == words[1] {
					subs[i].Usage = append(subs[i].Usage, line)
					found = true
				}
			}
			if !found {
				subs = append(subs, subcommandHelp{Name: words[1], Usage: []string{line}})
			}
		}
	}
	return subs
}

// notifyContract reports through the shared notice channel (stderr, or `notices` with --json)
// when the installed contract the Contract line names is missing or differs from the copy the
// binary carries. The text is fixed English like the rest of help.
func notifyContract(file string) {
	_, dir := config.ContractRoot()
	if dir == "" {
		return
	}
	installed, source, err := rules.Installed(dir, file)
	if err != nil {
		return
	}
	if source == rules.SourceEmbedded {
		Notify("contract_missing", fmt.Sprintf("The installed contract %s is missing; run `kander doctor` to install it.", file))
		return
	}
	if embedded, err := rules.File(file); err == nil && !bytes.Equal(installed, embedded) {
		Notify("contract_outdated", fmt.Sprintf("The installed contract %s differs from the copy in this binary; run `kander doctor` to update it.", file))
	}
}

// buildIndex lists the visible top-level commands in the order of the command table.
func buildIndex(root *cobra.Command) indexHelp {
	shown, _ := config.ContractRoot()
	index := indexHelp{SchemaVersion: helpSchemaVersion, Commands: []indexEntry{}, GlobalFlags: globalFlags}
	for _, name := range rules.CommandOrder() {
		var c *cobra.Command
		for _, child := range root.Commands() {
			if child.Name() == name && !child.Hidden {
				c = child
			}
		}
		if c == nil {
			continue
		}
		contract, err := rules.EmbeddedContract(name)
		if err != nil {
			continue
		}
		index.Commands = append(index.Commands, indexEntry{Name: name, Aliases: nonNil(c.Aliases), Summary: contract.Summary, Contract: shown + "/" + contract.File})
	}
	return index
}

// printHelp answers a help request for c: the text, or with --json the object, on stdout. A
// help request of a streaming command is not a stream: its notices join the object.
func printHelp(c *cobra.Command) {
	current.mu.Lock()
	current.streaming = false
	current.mu.Unlock()
	if !c.HasParent() || c.Name() == "help" {
		root := c
		for root.HasParent() {
			root = root.Parent()
		}
		index := buildIndex(root)
		if JSON() {
			Result(index)
			return
		}
		fmt.Fprint(c.OutOrStdout(), indexText(index))
		return
	}
	help, err := buildCommandHelp(topLevel(c))
	if err != nil {
		fmt.Fprintln(c.OutOrStdout(), c.UseLine())
		return
	}
	notifyContract(rules.ContractFile(topLevel(c).Name()))
	if JSON() {
		Result(help)
		return
	}
	fmt.Fprint(c.OutOrStdout(), commandText(help))
}

// usageText is the short usage printed after an argument error: the Syntax lines of the
// contract and its path.
func usageText(c *cobra.Command) string {
	if !c.HasParent() {
		return rootUsage + "\n" + rootHint
	}
	help, err := buildCommandHelp(topLevel(c))
	if err != nil {
		return c.UseLine()
	}
	return strings.Join(help.Usage, "\n") + "\nContract: " + help.Contract
}

// wrap breaks text into lines of at most width columns at spaces. The first line starts with
// first, the others with rest.
func wrap(text, first, rest string, width int) []string {
	var lines []string
	line := first
	empty := true
	for word := range strings.FieldsSeq(text) {
		if !empty && len(line)+1+len(word) > width {
			lines = append(lines, line)
			line, empty = rest, true
		}
		if !empty {
			line += " "
		}
		line += word
		empty = false
	}
	return append(lines, line)
}

func commandText(h commandHelp) string {
	var b strings.Builder
	line := func(format string, args ...any) { fmt.Fprintf(&b, format+"\n", args...) }
	line("%s\n", h.Summary)
	line("Usage:")
	for _, usage := range h.Usage {
		line("  %s", usage)
	}
	line("\nDescription:")
	for i, paragraph := range strings.Split(h.Description, "\n\n") {
		if i > 0 {
			line("")
		}
		for _, text := range wrap(paragraph, "  ", "  ", helpWidth) {
			line("%s", text)
		}
	}
	line("\nArguments:")
	if len(h.Arguments) == 0 {
		line("  None.")
	}
	for _, argument := range h.Arguments {
		for _, text := range wrap(strings.Join(argument.Names, ", "), "  ", "    ", helpWidth) {
			line("%s", text)
		}
		for _, text := range wrap(argument.Text, "      ", "      ", helpWidth) {
			line("%s", text)
		}
	}
	line("\nExamples:")
	for i, example := range h.Examples {
		if i > 0 {
			line("")
		}
		line("  # %s", example.Description)
		line("  %s", example.Command)
	}
	line("\nGlobal flags:")
	for _, text := range flagLines(h.GlobalFlags) {
		line("%s", text)
	}
	line("\nContract: %s", h.Contract)
	line("%s", h.ReadSentence)
	return b.String()
}

func flagLines(flags []flagHelp) []string {
	width := 0
	for _, f := range flags {
		width = max(width, len(f.Name))
	}
	lines := make([]string, 0, len(flags))
	for _, f := range flags {
		lines = append(lines, fmt.Sprintf("  %-*s   %s", width, f.Name, f.Text))
	}
	return lines
}

func indexText(index indexHelp) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s\n\nCommands:\n", helpTagline, rootUsage)
	labels := make([]string, len(index.Commands))
	width := 0
	for i, entry := range index.Commands {
		labels[i] = strings.Join(append([]string{entry.Name}, entry.Aliases...), ", ")
		width = max(width, len(labels[i]))
	}
	for i, entry := range index.Commands {
		indent := strings.Repeat(" ", 2+width+2)
		for _, text := range wrap(entry.Summary, fmt.Sprintf("  %-*s  ", width, labels[i]), indent, helpWidth) {
			fmt.Fprintln(&b, text)
		}
	}
	fmt.Fprintf(&b, "\nGlobal flags:\n%s\n\n%s\n", strings.Join(flagLines(index.GlobalFlags), "\n"), rootHint)
	return b.String()
}
