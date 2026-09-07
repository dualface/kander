package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/window"
)

func resumeDispatchFixture(t *testing.T) (string, string, board.Dispatch) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake agent")
	}
	root, _, _ := setupBoard(t)
	task, path := makeTodo(t, root, "dispatch-resume")
	startThenReview(t, root, "claude", task, path)
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	text, err := window.RenderWindowMetadata(s.Text, "foreground")
	if err != nil {
		t.Fatal(err)
	}
	if err = board.WriteManagedDocument(root, s.Entry, text); err != nil {
		t.Fatal(err)
	}
	d, err := board.PrepareDispatch(root, board.DispatchInput{ID: "resume-one", TaskID: task, Kind: "sync", Message: "同步本轮交付", Base: strings.Repeat("a", 40)})
	if err != nil {
		t.Fatal(err)
	}
	return root, task, d
}

func TestDurableResumeRequiresStoppedAndReconcilesReceipt(t *testing.T) {
	root, task, d := resumeDispatchFixture(t)
	before, _ := os.ReadFile(filepath.Join(root, "tmux.log.command"))
	_, _, err := capture(t, func() error {
		return commandResume(root, nil, "", task, d.Input.Message, "", true, 61, DispatchOptions{ID: d.Input.ID})
	})
	if err == nil {
		t.Fatal("unknown foreground started a second executor")
	}
	after, _ := os.ReadFile(filepath.Join(root, "tmux.log.command"))
	if string(after) != string(before) {
		t.Fatal("unproven recovery launched")
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	out, _, err := capture(t, func() error {
		return commandResume(root, nil, "", task, d.Input.Message, "", true, 61, DispatchOptions{ID: d.Input.ID})
	})
	if err != nil || !strings.Contains(out, `"state":"accepted"`) {
		t.Fatalf("durable reconciliation: %s %v", out, err)
	}
}

func TestDurableResumeExplicitTakeoverFencesOldEpoch(t *testing.T) {
	root, task, d := resumeDispatchFixture(t)
	agent := "claude"
	out, _, err := capture(t, func() error {
		return commandResume(root, &agent, "tmux", task+".md", d.Input.Message, "", true, 61, DispatchOptions{ID: d.Input.ID})
	})
	if err != nil {
		t.Fatal(err)
	}
	current, err := board.ReadDispatch(root, task, d.Input.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Authorization.Epoch != d.Authorization.Epoch+1 || current.State != board.DispatchUnknown || current.Accepted != nil {
		t.Fatalf("%s %+v", out, current)
	}
	if !current.Input.ConfirmBy.Equal(d.Input.ConfirmBy) {
		t.Fatal("takeover reset confirmation deadline")
	}
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization}); err == nil {
		t.Fatal("old executor accepted after takeover")
	}
	command, err := os.ReadFile(filepath.Join(root, "tmux.log.command"))
	if err != nil {
		t.Fatal(err)
	}
	promptPath := taskFileFromCommand(t, string(command))
	prompt, err := os.ReadFile(promptPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "--execution-epoch 2") || !strings.Contains(string(prompt), "replayed=false") {
		t.Fatalf("missing fenced prompt: %s", prompt)
	}
}

func TestDurableRecoveryAcceptsQuickCompletionBeforeLiveness(t *testing.T) {
	root, task, d := resumeDispatchFixture(t)
	s, _ := board.ReadSnapshot(root, task)
	if _, err := board.MoveWithOptions(s.Entry, root, "working", board.MoveOptions{Authorization: d.Authorization}); err != nil {
		t.Fatal(err)
	}
	s, _ = board.ReadSnapshot(root, task)
	if _, err := board.MoveWithOptions(s.Entry, root, "review", board.MoveOptions{Authorization: d.Authorization, DeliveryCommit: strings.Repeat("b", 40)}); err != nil {
		t.Fatal(err)
	}
	// A process that finished this dispatch can already have exited. Its durable
	// completion must win over the recovery pane's done/exited classification.
	exitCode := 7
	err := validateResumedDispatch(root, s.Entry, s.Text, LaunchPlan{Launcher: "foreground"}, LaunchOutcome{Poll: func() *int { return &exitCode }}, AgentSession{}, time.Minute.Seconds())
	if err != nil {
		t.Fatal(err)
	}
}

func TestDispatchPromptTranslationsAndFlagParsing(t *testing.T) {
	for _, lang := range []string{"en", "cn", "ja"} {
		config.ApplyLanguageArgument([]string{"kander", "--lang", lang})
		t.Setenv(config.EnvLang, lang)
		t.Setenv(config.EnvLangCLI, "1")
		d := board.Dispatch{Input: board.DispatchInput{ID: "prompt-one", TaskID: "20260908-prompt-task", Kind: "fix"}, Authorization: board.ExecutionAuthorization{DispatchID: "prompt-one", Epoch: 4}}
		prompt := DispatchInstruction(config.InstallPaths{Mode: config.ModeGlobal}, d)
		for _, part := range []string{"--dispatch-id prompt-one", "--execution-epoch 4", "--expect-revision", "--delivery-commit", "--disposition", "replayed=false"} {
			if !strings.Contains(prompt, part) {
				t.Fatalf("%s missing %s: %s", lang, part, prompt)
			}
		}
	}
	rest, o, err := ParseDispatchOptions([]string{"task", "--dispatch-id=id-one", "--kind", "sync", "--base", strings.Repeat("a", 40)})
	if err != nil || len(rest) != 1 || o.ID != "id-one" || o.Kind != "sync" {
		t.Fatalf("%+v %v", o, err)
	}
	if _, _, err = ParseDispatchOptions([]string{"--kind", "fix", "--kind=sync"}); err == nil {
		t.Fatal("duplicate accepted")
	}
}

func TestDurableLaunchErrorRetainsUnknownExecutorAndPayload(t *testing.T) {
	for _, failure := range []string{"KANBAN_TMUX_RESPAWN_FAIL", "KANBAN_TMUX_PANE_SETOPT_FAIL"} {
		t.Run(failure, func(t *testing.T) {
			root, task, d := resumeDispatchFixture(t)
			t.Setenv(failure, "1")
			agent := "claude"
			_, _, err := capture(t, func() error {
				return commandResume(root, &agent, "tmux", task, d.Input.Message, "", true, 61, DispatchOptions{ID: d.Input.ID})
			})
			if err == nil {
				t.Fatal("transport error disappeared without receipt")
			}
			current, err := board.ReadDispatch(root, task, d.Input.ID)
			if err != nil {
				t.Fatal(err)
			}
			if current.State != board.DispatchUnknown || current.Accepted != nil {
				t.Fatal("launch error fabricated business result")
			}
			s, err := board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasPrefix(board.MetadataFrom(s.Text, "WINDOW"), "tmux:") {
				t.Fatal("uncertain executor address rolled back")
			}
			if _, err = os.Stat(filepath.Join(root, "tmux.log.kill")); !os.IsNotExist(err) {
				t.Fatal("uncertain executor was killed")
			}
			command, err := os.ReadFile(filepath.Join(root, "tmux.log.command"))
			if err != nil {
				t.Fatal(err)
			}
			path := taskFileFromCommand(t, string(command))
			if _, err = os.Stat(path); err != nil {
				t.Fatal("potentially delivered task file removed")
			}
		})
	}
}
