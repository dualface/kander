package launch

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/process"
)

func TestBuiltinAgentArgumentsMatchPreChangeOutput(t *testing.T) {
	cfg := config.DefaultConfig()
	models := cfg.Models.Kanban
	const sid = "session-id"
	for _, agent := range []string{"codex", "claude", "grok", "cursor"} {
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

func TestBuiltinStartKeepsPromptOnArgvAndDoesNotDeliverToPane(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, bin := setupBoard(t)
	herdrLog := installHerdr(t, root, bin)
	var captured []string
	previous := newShellInvocation
	newShellInvocation = func(p process.AgentProgram, args []string, env map[string]string) (process.ProcessInvocation, error) {
		captured = append([]string{}, args...)
		return previous(p, args, env)
	}
	t.Cleanup(func() { newShellInvocation = previous })
	for _, launcher := range []string{"tmux", "herdr"} {
		for _, agent := range config.ExecutionAgents {
			t.Run(launcher+"/"+agent, func(t *testing.T) {
				captured = nil
				id, _ := makeTodo(t, root, "argv-"+launcher+"-"+agent)
				_, _, err := capture(t, func() error { return commandStart(root, agent, launcher, id) })
				if err != nil {
					t.Fatal(err)
				}
				if len(captured) == 0 || !strings.Contains(captured[len(captured)-1], "UTF-8") {
					t.Fatalf("prompt not last argv element: %q", captured)
				}
				if _, err := os.Stat(filepath.Join(root, "tmux.log.send-keys")); err == nil {
					t.Fatal("argv path sent keys")
				}
				if _, err := os.Stat(herdrLog + ".prompt"); !os.IsNotExist(err) {
					t.Fatalf("argv path used agent prompt: %v", err)
				}
			})
		}
	}
}
