package launch

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// promptReferences are the exact phrases that name a command without telling the agent to run
// it. The context decides, not the command name: the same `kander update` outside this phrase,
// as in "Then run kander update", is an instruction, and an instruction goes through the
// argument validation however short it is. Each phrase is removed before the command lines are
// read, and a phrase that no prompt contains any more fails the test, so the list cannot
// outlive the text it excuses.
//
// The orchestrate prompt names `kander subscribe` and `kander review` while it explains how a
// session is woken, which tells the agent nothing to run.
var promptReferences = []string{"controlled `kander update` commands", "a background `kander subscribe`", "such as `kander review`"}

// promptActions finds the actions the result prompt lists after its command line.
var promptActions = regexp.MustCompile(`--action ([a-z]+)`)

// resultActions are the actions the result prompt must name, each as a command line of its own.
var resultActions = []string{"inspect", "apply", "decide"}

// promptCommandLines are the command lines a rendered prompt tells an agent to run. The three
// actions of `issue result` stand in the sentence after the line, so they are added to it; a
// prompt that lost one of them has lost a command line, which fails the test.
func promptCommandLines(t *testing.T, name, text string, referenced map[string]bool) []string {
	t.Helper()
	for _, reference := range promptReferences {
		if strings.Contains(text, reference) {
			referenced[reference] = true
			text = strings.ReplaceAll(text, reference, "")
		}
	}
	var lines []string
	for _, match := range promptCommand.FindAllStringSubmatch(text, -1) {
		line := match[1]
		switch {
		case promptProse[line]:
		case strings.HasPrefix(line, "issue result "):
			var actions []string
			for _, action := range promptActions.FindAllStringSubmatch(text, -1) {
				actions = append(actions, action[1])
				lines = append(lines, line+" --action "+action[1])
			}
			if !slices.Equal(actions, resultActions) {
				t.Errorf("%s: the result prompt names the actions %v, want %v", name, actions, resultActions)
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
		input := board.DispatchInput{ID: "dispatch-1", TaskID: task, Kind: kind}
		if kind == "wrap-up" {
			// The wrap-up prompt names the full completion command, so its command line is checked too.
			input.Evidence.WrapUp = &board.DispatchWrapUpBinding{Git: board.DispatchIntegration{SourceCommit: strings.Repeat("a", 40)}}
		}
		text, err = DispatchInstruction(goldenPaths, board.Dispatch{
			Input:         input,
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
	seen, referenced := map[string]bool{}, map[string]bool{}
	for name, prompt := range renderedPrompts(t) {
		for _, line := range promptCommandLines(t, name, prompt, referenced) {
			words := strings.Fields(line)
			root := cli.NewRoot()
			target, _, err := root.Find(words)
			if err != nil || target == root {
				t.Errorf("%s: kander %s names no command", name, line)
				continue
			}
			t.Run(name+"/"+line, func(t *testing.T) {
				commandtest.Stub(root, func(*cobra.Command, []string) {})
				code, _, stderr := commandtest.Capture(func() int { return command.Execute(root, words) })
				if code != 0 {
					t.Errorf("kander %s: exit=%d %s", line, code, stderr)
					return
				}
				seen[target.CommandPath()] = true
			})
		}
	}
	for _, reference := range promptReferences {
		if !referenced[reference] {
			t.Errorf("no prompt contains the reference %q any more", reference)
		}
	}
	for _, name := range []string{"kander config", "kander show", "kander move", "kander issue result", "kander issue import"} {
		if !seen[name] {
			t.Errorf("no prompt names %q any more", name)
		}
	}
}

// Every prompt template that names a command is rendered by a production renderer above, so its
// command lines are tested. resume.md names commands but no renderer uses it, so it is listed
// as unrendered: a template added without a renderer in this test fails here.
func TestEveryPromptTemplateThatNamesACommandIsRendered(t *testing.T) {
	rendered := map[string]bool{
		"dispatch.md": true, "refine.md": true, "reporting.md": true, "result.md": true, "rule-loading.md": true,
		"self-move-review.md": true, "start.md": true, "takeover.md": true, "triage.md": true, "orchestrate.md": true,
	}
	unrendered := map[string]bool{"resume.md": true}
	files, err := filepath.Glob(filepath.Join("..", "..", "rules", "templates", "prompt", "*.md"))
	if err != nil || len(files) == 0 {
		t.Fatalf("prompt templates: %v %v", files, err)
	}
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(file)
		if !strings.Contains(string(data), "{{.Command}}") && !strings.Contains(string(data), "kander ") {
			continue
		}
		if !rendered[name] && !unrendered[name] {
			t.Errorf("%s names a command but no test renders it", name)
		}
	}
}
