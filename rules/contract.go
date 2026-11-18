package rules

import (
	"fmt"
	"regexp"
	"strings"
)

// Argument is one top-level entry of the Arguments section of a contract.
type Argument struct {
	// Names are the backticked names as written, for example "--owner <agent>".
	Names []string `json:"names"`
	Text  string   `json:"text"`
}

// Example is one pair of lines of the Examples section: a "# description" line and the
// command line that follows it.
type Example struct {
	Description string `json:"description"`
	Command     string `json:"command"`
}

// Contract is the structured content of a command contract, the single source of the help.
type Contract struct {
	// File is the contract path relative to the rules root.
	File        string
	Summary     string
	Description string
	Usage       []string
	Arguments   []Argument
	Examples    []Example
}

// requestSection is the section of notify.md that describes `kander request`.
const requestSection = "## Request command"

// commandsDir is the directory of the contract files below the rules root.
const commandsDir = SnippetsDir + "/commands/"

// ContractFile returns the contract file path of a top-level command. `request` is documented
// in the contract of `notify`.
func ContractFile(command string) string {
	if command == "request" {
		command = "notify"
	}
	return commandsDir + command + ".md"
}

// EmbeddedContract parses the embedded contract of a top-level command.
func EmbeddedContract(command string) (Contract, error) {
	data, err := File(ContractFile(command))
	if err != nil {
		return Contract{}, err
	}
	return ParseContract(command, string(data))
}

// ParseContract reads the help content of command out of its contract text. The contract of
// `notify` holds two commands: Usage, Arguments and Examples of each are the ones that belong
// to it, and the summary and description of `request` come from its own section.
func ParseContract(command, text string) (Contract, error) {
	sections := splitSections(text)
	contract := Contract{File: ContractFile(command)}
	purpose := sections["## Purpose and users"]
	if command == "request" {
		purpose = sections[requestSection]
	}
	contract.Description = strings.TrimSpace(strings.Join(purpose, "\n"))
	contract.Summary = FirstSentence(contract.Description)

	shared := contract.File == commandsDir+"notify.md"
	contract.Usage = ownLines(CommandSyntaxLines(text), command, shared)
	var err error
	if contract.Arguments, err = parseArguments(sections["## Arguments"]); err != nil {
		return Contract{}, fmt.Errorf("%s: %w", contract.File, err)
	}
	if shared {
		contract.Arguments = argumentsNamedIn(contract.Arguments, contract.Usage)
	}
	if contract.Examples, err = parseExamples(sections["## Examples"]); err != nil {
		return Contract{}, fmt.Errorf("%s: %w", contract.File, err)
	}
	if shared {
		var own []Example
		for _, example := range contract.Examples {
			if strings.HasPrefix(example.Command, "kander "+command+" ") || example.Command == "kander "+command {
				own = append(own, example)
			}
		}
		contract.Examples = own
	}
	return contract, nil
}

// ownLines keeps the lines of a command in a contract that holds two (shared): the lines that
// start with its name. Without sharing every line belongs to the command.
func ownLines(lines []string, command string, shared bool) []string {
	var own []string
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !shared || strings.HasPrefix(line, "kander "+command+" ") || line == "kander "+command {
			own = append(own, line)
		}
	}
	return own
}

// splitSections splits a contract at its second-level headings. The key is the heading
// line; the lines of a fenced block that look like headings are not headings.
func splitSections(text string) map[string][]string {
	sections := map[string][]string{}
	title, fenced := "", false
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, "```") {
			fenced = !fenced
		}
		if !fenced && strings.HasPrefix(line, "## ") {
			title = line
			sections[title] = nil
			continue
		}
		if title != "" {
			sections[title] = append(sections[title], line)
		}
	}
	return sections
}

// FirstSentence returns the first sentence of text: up to the first period outside inline
// code that is followed by a space or ends the text. A text without such a period is returned
// whole.
func FirstSentence(text string) string {
	text = strings.TrimSpace(text)
	inCode := false
	for i, r := range text {
		switch {
		case r == '`':
			inCode = !inCode
		case r == '.' && !inCode:
			if rest := text[i+1:]; rest == "" || rest[0] == ' ' || rest[0] == '\n' {
				return text[:i+1]
			}
		}
	}
	return text
}

var argumentEntry = regexp.MustCompile("^- (`[^`]+`(?:, `[^`]+`)*): (\\S.*)$")

var argumentName = regexp.MustCompile("`([^`]+)`")

// parseArguments reads the entries of an Arguments section. The section is either the single
// line "None." or a list of entries "- `name`, `name`: text", whose text may continue on the
// indented lines below it.
func parseArguments(lines []string) ([]Argument, error) {
	var entries []Argument
	var body []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			body = append(body, line)
		}
	}
	if len(body) == 1 && body[0] == "None." {
		return nil, nil
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("the Arguments section is empty; write None. when the command has no arguments")
	}
	for _, line := range body {
		switch match := argumentEntry.FindStringSubmatch(line); {
		case match != nil:
			var names []string
			for _, name := range argumentName.FindAllStringSubmatch(match[1], -1) {
				names = append(names, name[1])
			}
			entries = append(entries, Argument{Names: names, Text: match[2]})
		case strings.HasPrefix(line, " ") && len(entries) > 0:
			entries[len(entries)-1].Text += " " + strings.TrimSpace(line)
		default:
			return nil, fmt.Errorf("the Arguments section holds a line that is not an argument entry: %q", line)
		}
	}
	return entries, nil
}

// argumentsNamedIn keeps the entries that have a name which a usage line names: the first word
// of the name, without brackets, angle brackets and trailing dots, is a word of the line.
func argumentsNamedIn(arguments []Argument, usage []string) []Argument {
	words := map[string]bool{}
	for _, line := range usage {
		for word := range strings.FieldsSeq(line) {
			words[trimWord(word)] = true
		}
	}
	var kept []Argument
	for _, argument := range arguments {
		for _, name := range argument.Names {
			if first := strings.Fields(name); len(first) > 0 && words[trimWord(first[0])] {
				kept = append(kept, argument)
				break
			}
		}
	}
	return kept
}

func trimWord(word string) string {
	return strings.TrimSuffix(strings.Trim(word, "[]()<>,"), "...")
}

// parseExamples reads the Examples section: in a fenced block, a "# description" line followed
// by a command line, with a blank line between the examples.
func parseExamples(lines []string) ([]Example, error) {
	var examples []Example
	fenced, description := false, ""
	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "```"):
			fenced = !fenced
		case !fenced:
		case strings.TrimSpace(line) == "":
			if description != "" {
				return nil, fmt.Errorf("the example %q has no command", description)
			}
		case strings.HasPrefix(line, "# "):
			if description != "" {
				return nil, fmt.Errorf("the example %q has no command", description)
			}
			description = strings.TrimPrefix(line, "# ")
		case description == "":
			return nil, fmt.Errorf("the command %q has no description line above it", line)
		default:
			examples = append(examples, Example{Description: description, Command: line})
			description = ""
		}
	}
	if description != "" {
		return nil, fmt.Errorf("the example %q has no command", description)
	}
	return examples, nil
}

// SyntaxPaths expands the command path a Syntax line names into the paths it stands for: the
// words after "kander" up to the first option, optional word or placeholder, with an
// alternation such as <plan|close> giving one path per alternative.
func SyntaxPaths(line string) []string {
	words := strings.Fields(strings.TrimPrefix(strings.TrimSpace(line), "kander "))
	prefixes := [][]string{{}}
	for _, word := range words {
		if strings.HasPrefix(word, "-") || strings.HasPrefix(word, "[") || strings.HasPrefix(word, "(") {
			break
		}
		choices := []string{word}
		switch {
		case strings.HasPrefix(word, "<") && strings.Contains(word, "|"):
			choices = strings.Split(strings.Trim(word, "<>"), "|")
		case strings.HasPrefix(word, "<"), word == strings.ToUpper(word) && strings.ToLower(word) != word:
			choices = nil
		case strings.Contains(word, "|"):
			choices = strings.Split(word, "|")
		}
		if choices == nil {
			break
		}
		var next [][]string
		for _, prefix := range prefixes {
			for _, choice := range choices {
				next = append(next, append(append([]string(nil), prefix...), choice))
			}
		}
		prefixes = next
	}
	paths := make([]string, 0, len(prefixes))
	for _, prefix := range prefixes {
		paths = append(paths, strings.Join(prefix, " "))
	}
	return paths
}

// CommandOrder returns the top-level commands in the order of the command table of the
// embedded README, with `request` after `notify`.
func CommandOrder() []string {
	data, err := File(commandsDir + "README.md")
	if err != nil {
		return nil
	}
	var order []string
	seen := map[string]bool{}
	add := func(name string) {
		if !seen[name] {
			seen[name] = true
			order = append(order, name)
		}
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if !strings.HasPrefix(line, "kander ") {
			continue
		}
		name := strings.Fields(line)[1]
		add(name)
		if name == "notify" {
			add("request")
		}
	}
	return order
}
