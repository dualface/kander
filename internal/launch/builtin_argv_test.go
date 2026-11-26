package launch

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/process"
)

func TestBuiltinAgentArgumentsMatchPreChangeOutput(t *testing.T) {
	cfg := config.DefaultConfig()
	models := cfg.Models.Kanban
	const sid = "session-id"
	for _, agent := range []string{"codex", "claude", "grok", "cursor", "pi", "devin", "opencode", "kimi", "droid", "agy"} {
		for _, kind := range []string{"large", "small"} {
			for _, resume := range []bool{false, true} {
				for _, hasSession := range []bool{true, false} {
					model := models[agent]
					scale := "small"
					if kind == "large" {
						scale = "large"
					}
					modelID := config.KanbanModelFor(model, scale)
					effort := model[scale+"_effort"]
					ref := ""
					if hasSession {
						ref = sid
					}
					var want []string
					switch agent {
					case "codex":
						var options []string
						if modelID != "" {
							options = append(options, "--model", modelID)
						}
						options = append(options, "--config", `model_reasoning_effort="`+effort+`"`, "--dangerously-bypass-approvals-and-sandbox")
						if resume {
							want = append(append([]string{"resume"}, options...), ref)
						} else {
							want = options
						}
					case "claude":
						flag := "--session-id"
						if resume {
							flag = "--resume"
						}
						if modelID != "" {
							want = append(want, "--model", modelID)
						}
						want = append(want, "--effort", effort, "--dangerously-skip-permissions", flag, ref)
					case "grok":
						flag := "--session-id"
						if resume {
							flag = "--resume"
						}
						if modelID != "" {
							want = append(want, "--model", modelID)
						}
						want = append(want, "--effort", effort, "--permission-mode", "bypassPermissions", flag, ref)
					case "cursor":
						if modelID != "" {
							want = append(want, "--model", modelID)
						}
						want = append(want, "--trust", "--force", "--resume", ref)
					case "pi":
						// pi became a built-in after the four original agents; this case
						// pins its initial baseline rather than pre-change output.
						flag := "--session-id"
						if resume {
							flag = "--session"
						}
						want = append(want, "--approve")
						if ref != "" {
							want = append(want, flag, ref)
						}
						if modelID != "" {
							want = append(want, "--model", modelID)
						}
						want = append(want, "--thinking", effort)
					case "devin":
						if modelID != "" {
							want = append(want, "--model", modelID)
						}
						want = append(want, "--permission-mode", "dangerous", "--respect-workspace-trust", "false")
						if resume && ref != "" {
							want = append(want, "--resume", ref)
						}
						want = append(want, "--")
					case "opencode":
						if modelID != "" {
							want = append(want, "--model", modelID)
						}
						if resume && ref != "" {
							want = append(want, "--session", ref)
						}
						want = append(want, "--auto", "--prompt")
					case "kimi":
						want = append(want, "--auto")
						if modelID != "" {
							want = append(want, "--model", modelID)
						}
						if resume && ref != "" {
							want = append(want, "--session", ref)
						}
					case "droid":
						want = append(want, "--auto", "high")
						if resume && ref != "" {
							want = append(want, "--resume", ref)
						}
					case "agy":
						if resume && ref != "" {
							want = append(want, "--conversation", ref)
						}
						if modelID != "" {
							want = append(want, "--model", modelID)
						}
						want = append(want, "--effort", effort, "--prompt-interactive")
					}
					name := agent + "/" + kind
					if resume {
						name += "/resume"
					} else {
						name += "/start"
					}
					if hasSession {
						name += "/session"
					} else {
						name += "/nosession"
					}
					t.Run(name, func(t *testing.T) {
						got, err := agentArguments(agent, model, kind, AgentSession{Agent: agent, Reference: ref}, resume, cfg)
						if err != nil {
							t.Fatal(err)
						}
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("%q != %q", got, want)
						}
					})
				}
			}
		}
	}
}

func TestBuiltinStartWithRenameDisabledKeepsPromptOnArgv(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, bin := setupBoard(t)
	herdrLog := installHerdr(t, root, bin)
	var captured []string
	// onLaunch runs once the launch instruction is known, which is after the
	// pre-launch session snapshot, so it can stand in for an agent that writes
	// its session then.
	var onLaunch func(args []string)
	previous := newShellInvocation
	newShellInvocation = func(p process.AgentProgram, args []string, env map[string]string) (process.ProcessInvocation, error) {
		captured = append([]string{}, args...)
		if onLaunch != nil {
			onLaunch(captured)
		}
		return previous(p, args, env)
	}
	t.Cleanup(func() { newShellInvocation = previous })
	for _, launcher := range []string{"tmux", "herdr"} {
		for _, agent := range config.ExecutionAgents {
			definition := config.AgentFor(nil, agent)
			if definition.PromptDelivery != nil && definition.PromptDelivery.Mode == "pane" {
				continue
			}
			t.Run(launcher+"/"+agent, func(t *testing.T) {
				session := definition.Session
				discovered := session != nil && session.Mode == "discovered"
				if discovered {
					previousList := enumerateSessionsFn
					calls := 0
					enumerateSessionsFn = func(context.Context, *config.SessionDiscovery, *process.AgentProgram, string, string) ([]string, error) {
						calls++
						if calls == 1 {
							return nil, nil
						}
						return []string{agent + "-session"}, nil
					}
					defer func() { enumerateSessionsFn = previousList }()
				}
				captured = nil
				agyConversation := ""
				if definition.Session != nil && definition.Session.Mode == "hook:agy-transcript" {
					// The fake agy writes the conversation the launch created.
					agyConversation = "agy-" + launcher
					onLaunch = func(args []string) {
						writeAgyTranscript(t, agyBrainDir(t), agyConversation, agyTranscriptLine(args[len(args)-1]))
					}
					defer func() { onLaunch = nil }()
				}
				id, _ := makeTodo(t, root, "argv-"+launcher+"-"+agent)
				_, _, err := capture(t, func() error { return commandStart(root, agent, launcher, id) })
				if err != nil {
					t.Fatal(err)
				}
				if len(captured) == 0 || !strings.Contains(captured[len(captured)-1], "UTF-8") {
					t.Fatalf("prompt not last argv element: %q", captured)
				}
				if agent == "agy" && (len(captured) < 2 || captured[len(captured)-2] != "--prompt-interactive") {
					t.Fatalf("the instruction must follow --prompt-interactive: %q", captured)
				}
				if _, err := os.Stat(filepath.Join(root, "tmux.log.send-keys")); err == nil {
					t.Fatal("argv path sent keys")
				}
				if _, err := os.Stat(herdrLog + ".prompt"); !os.IsNotExist(err) {
					t.Fatalf("argv path used agent prompt: %v", err)
				}
				if discovered {
					snapshot, err := board.ReadSnapshot(root, id)
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(snapshot.Text, "- SESSION: "+agent+" "+agent+"-session\n") {
						t.Fatalf("card session was not persisted: %s", snapshot.Text)
					}
				}
				if agyConversation != "" {
					snapshot, err := board.ReadSnapshot(root, id)
					if err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(snapshot.Text, "- SESSION: agy "+agyConversation+"\n") {
						t.Fatalf("the discovered conversation was not written to the card: %s", snapshot.Text)
					}
				}
			})
		}
	}
}

func TestPiStartUsesAGeneratedUUIDSession(t *testing.T) {
	cfg := config.DefaultConfig()
	session, err := newAgentSession("pi", nil, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if session.Agent != "pi" || !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`).MatchString(session.Reference) {
		t.Fatalf("pi session %+v is not a generated UUID", session)
	}
	got, err := agentArguments("pi", cfg.Models.Kanban["pi"], "small", session, false, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got[:3], []string{"--approve", "--session-id", session.Reference}) {
		t.Fatalf("start argv %q does not carry the generated session", got)
	}
}
