package launch

import (
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/command/commandtest"
	"github.com/dualface/kander/internal/issue"
)

// promptCommand finds the command lines in a rendered prompt: an occurrence of "kander
// <command>" up to the end of the sentence or clause.
var promptCommand = regexp.MustCompile("kander ([a-z][a-z-]*(?: [^ \\n`]*[^ \\n`.,;:)])*?)(?: (?:to|then|with|and|in|for) |[.,;:)`](?: |$)|\\n|$)")

// promptProse are the mentions of the command name in a prompt that are not command lines.
var promptProse = map[string]bool{"move prints it prefilled": true}

// promptActions finds the actions the result prompt lists after its command line.
var promptActions = regexp.MustCompile(`--action ([a-z]+)`)

// promptCommandLines are the command lines a rendered prompt tells an agent to run. The three
// actions of `issue result` stand in the sentence after the line, so they are added to it.
func promptCommandLines(text string) []string {
	var lines []string
	for _, match := range promptCommand.FindAllStringSubmatch(text, -1) {
		line := match[1]
		switch {
		case promptProse[line]:
		case strings.HasPrefix(line, "issue result "):
			for _, action := range promptActions.FindAllStringSubmatch(text, -1) {
				lines = append(lines, line+" --action "+action[1])
			}
		default:
			lines = append(lines, line)
		}
	}
	return lines
}

// renderedPrompts renders every prompt Kander writes for an agent with the production
// renderers, the way a launch does.
func renderedPrompts(t *testing.T) map[string]string {
	t.Helper()
	goldenConfig(t, true)
	const task = "20260101-golden-task"
	single := "- LANGUAGE: en\n- SIZE: small\n"
	member := "- LANGUAGE: en\n- SIZE: large\n- TASK_GROUP: 20260101-golden-group\n"
	prompts := map[string]string{}
	must := func(name, text string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		prompts[name] = text
	}
	text, err := startAgentPrompt(task, goldenPaths, "", single)
	must("start single", text, err)
	text, err = startAgentPrompt(task, goldenPaths, "20260101-golden-group", member)
	must("start member", text, err)
	text, err = takeoverAgentPrompt(task, "continue the work", goldenPaths, "codex", "working", single)
	must("takeover", text, err)
	text, err = SelfMoveInstruction(goldenPaths, task, "review")
	must("self move", text, err)
	for _, kind := range []string{"fix", "sync", "wrap-up"} {
		text, err = DispatchInstruction(goldenPaths, board.Dispatch{
			Input:         board.DispatchInput{ID: "dispatch-1", TaskID: task, Kind: kind},
			Authorization: board.ExecutionAuthorization{DispatchID: "dispatch-1", Epoch: 2},
		})
		must("dispatch "+kind, text, err)
	}
	text, err = orchestratorAgentPrompt([]OrchestrateTask{{TaskID: task, State: "todo", TaskGroup: "20260101-golden-group"}}, []string{member}, "plan notes", goldenPaths)
	must("orchestrate", text, err)
	repository := issue.Repository{Host: "github.com", Owner: "owner", Name: "repo"}
	card := &board.TaskSummary{TaskID: task, State: "backlog", Document: single}
	launch := issue.TriageLaunch{Repository: repository, Number: 42, CardID: task, JSONPath: "/evidence/issue.json", MarkdownPath: "/evidence/issue.md", CardSnapshot: card}
	for name, request := range map[string]issue.TriageLaunch{
		"issue result": {ResultSync: true},
		"issue refine": {RefineCard: true},
		"issue triage": {},
	} {
		request.Repository, request.Number, request.CardID, request.JSONPath, request.MarkdownPath, request.CardSnapshot = launch.Repository, launch.Number, launch.CardID, launch.JSONPath, launch.MarkdownPath, launch.CardSnapshot
		text, err = triageAgentPrompt(request, goldenPaths)
		must(name, text, err)
	}
	unbound := launch
	unbound.CardID = ""
	text, err = triageAgentPrompt(unbound, goldenPaths)
	must("issue triage unbound", text, err)
	return prompts
}

// Every command line a prompt of Kander tells an agent to run is accepted by the command that
// would run it: the prompts come from the production renderers, and the command line goes
// through the parsing, the argument validation and the checks before the command body of the
// real command tree, with only the body replaced.
func TestRenderedPromptCommandLinesAreAccepted(t *testing.T) {
	seen := map[string]bool{}
	for name, prompt := range renderedPrompts(t) {
		for _, line := range promptCommandLines(prompt) {
			words := strings.Fields(line)
			root := cli.NewRoot()
			target, rest, err := root.Find(words)
			if err != nil || target == root {
				t.Errorf("%s: kander %s names no command", name, line)
				continue
			}
			seen[target.CommandPath()] = true
			if len(rest) == 0 {
				// The prompt only names the command, as in "controlled `kander update` commands".
				continue
			}
			t.Run(name+"/"+line, func(t *testing.T) {
				commandtest.Stub(root, func(*cobra.Command, []string) {})
				if code, _, stderr := commandtest.Capture(func() int { return command.Execute(root, words) }); code != 0 {
					t.Errorf("kander %s: exit=%d %s", line, code, stderr)
				}
			})
		}
	}
	for _, name := range []string{"kander config", "kander show", "kander move", "kander issue result", "kander issue import", "kander update"} {
		if !seen[name] {
			t.Errorf("no prompt names %q any more", name)
		}
	}
}
