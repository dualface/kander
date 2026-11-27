package launch

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/process"
	"github.com/dualface/kander/v2/internal/terminal"
)

func TestSessionName(t *testing.T) {
	for label, want := range map[string]string{
		"kb-v2-worker-投递协作请求时确认执行端已提交，不再把输入框草稿记为已送达":            "kb-v2-worker-投递协作请求时确认执行端已提交-不再把输入框草稿记为已送达",
		" --kb-中文。＠@ /\"'a\u0301\u202eb\t\n\u007fc ٣②　x.y_z-- ": "kb-中文-a\u0301bc-٣-x.y_z",
		"chat-20261127-013000": "chat-20261127-013000",
		"/\u202e\x00 -@":       "",
	} {
		if got := sessionName(label); got != want {
			t.Errorf("sessionName(%q) = %q, want %q", label, got, want)
		}
	}
}

type renameTestBackend struct {
	terminal.Backend
	name          string
	programStatus bool
	agentIdentity bool
	screens       []string
	reads         int
	facts         terminal.PaneFacts
	command       string
	stopped       bool
	delivered     []string
	deliveryReads []int
	sendErr       error
}

func (b *renameTestBackend) Name() string                        { return b.name }
func (b *renameTestBackend) Executable() string                  { return os.Args[0] }
func (b *renameTestBackend) AutoDetect(func(string) string) bool { return false }
func (b *renameTestBackend) ParseAddress(string) (terminal.Address, bool) {
	return terminal.Address{}, false
}
func (b *renameTestBackend) Capabilities() terminal.Capabilities {
	return terminal.Capabilities{Container: true, ProgramStatus: b.programStatus, AgentIdentity: b.agentIdentity}
}
func (b *renameTestBackend) CreateContainer(terminal.Conn, terminal.Target, string, string) (terminal.Address, error) {
	return terminal.Address{Container: "window", Pane: "pane"}, nil
}
func (b *renameTestBackend) CloseContainer(context.Context, terminal.Conn, terminal.Address) error {
	b.stopped = true
	return nil
}
func (b *renameTestBackend) WaitReady(terminal.Conn, string, int) error { return nil }
func (b *renameTestBackend) RunCommand(_ terminal.Conn, _, _, command string, _ bool) error {
	b.command = command
	return nil
}
func (b *renameTestBackend) ReadOutput(context.Context, terminal.Conn, string) (string, error) {
	b.reads++
	return b.screens[min(b.reads, len(b.screens))-1], nil
}
func (b *renameTestBackend) PaneFacts(context.Context, terminal.Conn, string) (terminal.PaneFacts, error) {
	return b.facts, nil
}
func (b *renameTestBackend) DeliverText(_ context.Context, _ terminal.Conn, _ string, text string) error {
	b.delivered = append(b.delivered, text)
	b.deliveryReads = append(b.deliveryReads, b.reads)
	if strings.HasPrefix(text, "/rename ") {
		return b.sendErr
	}
	return nil
}

// Terminals of TestRenameLaunchDelivery: the default reports program status,
// identityTerminal reports a recognized agent, and plainTerminal reports
// neither.
const (
	identityTerminal = "identity"
	plainTerminal    = "plain"
)

func TestRenameLaunchDelivery(t *testing.T) {
	resetLang(t)
	const label = "kb-中文 @/Name_3.0"
	const rename = "/rename kb-中文-Name_3.0"
	const acknowledgement = "renamed kb-中文-Name_3.0"
	const prompt = "task instruction"
	const suggestion = "❯\u00a0Try \"fix lint errors\""
	const history = "❯ " + rename + "\n" + acknowledgement + "\nready"
	const beforeFixedScroll = "first\nsecond\nthird\nretained heading\nretained body\n" + history
	const beforeFixedScrollWithDrop = acknowledgement + "\nsecond\nthird\nretained heading\nretained body\n" + history
	const afterFixedScroll = "retained heading\nretained body\n" + history + "\n" + history
	for _, tc := range []struct {
		name          string
		command       string
		mode          string
		afterArgv     bool
		terminal      string
		paneAgent     string
		dialect       string
		busy          bool
		noComplete    bool
		completeMatch string
		screens       []string
		sendErr       error
		wantSends     []string
		wantCode      string
		wantWarning   string
		firstReadAt   int
	}{
		{name: "no-rename-argv", mode: "argv"},
		{name: "no-rename-pane", mode: "pane", screens: []string{"ready"}, wantSends: []string{prompt}},
		{name: "ready", command: "/rename {name}", screens: []string{"ready", "ready\n❯ " + rename + "\n" + acknowledgement}, wantSends: []string{rename, prompt}},
		{name: "composer-suggestion-is-not-a-draft", command: "/rename {name}", screens: []string{suggestion, suggestion + "\n" + acknowledgement}, wantSends: []string{rename, prompt}},
		{name: "working-status-timeout", command: "/rename {name}", busy: true, screens: []string{suggestion}, wantCode: "launch.pane_ready_timeout"},
		{name: "recognized-agent", command: "/rename {name}", terminal: identityTerminal, screens: []string{suggestion, suggestion + "\n" + acknowledgement}, wantSends: []string{rename, prompt}},
		{name: "recognized-dialect", command: "/rename {name}", terminal: identityTerminal, paneAgent: "builtin", dialect: "builtin", screens: []string{suggestion, suggestion + "\n" + acknowledgement}, wantSends: []string{rename, prompt}},
		{name: "recognized-other-agent-timeout", command: "/rename {name}", terminal: identityTerminal, paneAgent: "other", screens: []string{suggestion}, wantCode: "launch.pane_ready_timeout"},
		{name: "recognized-agent-working-timeout", command: "/rename {name}", terminal: identityTerminal, busy: true, screens: []string{suggestion}, wantCode: "launch.pane_ready_timeout"},
		{name: "no-agent-status-keeps-argv", command: "/rename {name}", terminal: plainTerminal, screens: []string{suggestion}, wantWarning: "launch.rename_no_status"},
		{name: "no-agent-status-pane-prompt", command: "/rename {name}", mode: "pane", terminal: plainTerminal, screens: []string{"ready"}, wantSends: []string{prompt}, wantWarning: "launch.rename_no_status"},
		{name: "completion-rule-with-leading-line-break", command: "/rename {name}", completeMatch: `regex:(?:^|\r?\n)renamed {name}(?:\r?\n|$)`, screens: []string{"ready", "❯ " + rename + "\r\n" + acknowledgement + "\r\nready"}, wantSends: []string{rename, prompt}},
		{name: "unchanged-help-is-not-a-draft", command: "/rename {name}", noComplete: true, screens: []string{"Help: /rename <title>\nready", "Help: /rename <title>\nready\nstatus updated"}, wantSends: []string{rename, prompt}},
		{name: "same-name-new-acknowledgement", command: "/rename {name}", screens: []string{"❯ " + rename + "\n" + acknowledgement + "\nready", "❯ " + rename + "\n" + acknowledgement + "\n❯ " + rename + "\n" + acknowledgement + "\nready"}, wantSends: []string{rename, prompt}},
		{name: "same-name-acknowledgement-replaces-scrolled-one", command: "/rename {name}", screens: []string{acknowledgement + "\nready", "ready\n❯ " + rename + "\n" + acknowledgement + "\nready"}, wantSends: []string{rename, prompt}},
		{name: "new-acknowledgement-reuses-row-of-retained-history", command: "/rename {name}", screens: []string{beforeFixedScroll, afterFixedScroll}, wantSends: []string{rename, prompt}},
		{name: "reused-acknowledgement-row-does-not-hide-independent-draft", command: "/rename {name}", screens: []string{beforeFixedScroll, afterFixedScroll + " ❯ " + rename}, wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "reused-row-with-another-acknowledgement-scrolled-out", command: "/rename {name}", screens: []string{beforeFixedScrollWithDrop, afterFixedScroll}, wantSends: []string{rename, prompt}},
		{name: "reused-row-with-scrolled-acknowledgement-still-blocks-draft", command: "/rename {name}", screens: []string{beforeFixedScrollWithDrop, afterFixedScroll + " ❯ " + rename}, wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "scrolled-acknowledgement-does-not-hide-another-draft", command: "/rename {name}", screens: []string{acknowledgement + "\nready", "ready\n❯ " + rename + "\n" + acknowledgement + "\nready\n❯ " + rename}, wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "same-name-stale-acknowledgement-with-draft", command: "/rename {name}", screens: []string{acknowledgement + "\nready", acknowledgement + "\nready\n❯ " + rename}, wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "scrolled-echo-is-not-proof-of-a-clean-composer", command: "/rename {name}", screens: []string{"❯ " + rename + "\n" + acknowledgement + "\nready", acknowledgement + "\nready\n❯ " + rename}, wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "fresh-acknowledgement-with-another-draft", command: "/rename {name}", screens: []string{"ready", "❯ " + rename + "\n" + acknowledgement + "\nready\n❯ " + rename}, wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "dialog-clears", command: "/rename {name}", screens: []string{"ready\ntrust", "ready\ntrust", "ready", "ready\nrenamed kb-中文-Name_3.0"}, wantSends: []string{rename, prompt}, firstReadAt: 3},
		{name: "dialog-timeout-while-idle", command: "/rename {name}", screens: []string{"ready\ntrust"}, wantCode: "launch.pane_ready_blocked"},
		{name: "send-failure", command: "/rename {name}", screens: []string{"ready"}, sendErr: errors.New("send failed"), wantSends: []string{rename, prompt}, wantWarning: "launch.rename_unconfirmed"},
		{name: "enter-failure-leaves-a-draft", command: "/rename {name}", screens: []string{"ready", "ready\n/rename kb-中文"}, sendErr: errors.New("Enter failed after typing"), wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "enter-failure-leaves-draft-above-unchanged-acknowledgement", command: "/rename {name}", screens: []string{"ready\n❯ \n" + acknowledgement, "ready\n❯ " + rename + "\n" + acknowledgement}, sendErr: errors.New("Enter failed after typing"), wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "transport-success-does-not-refresh-stationary-acknowledgement", command: "/rename {name}", screens: []string{"ready\n❯ \n" + acknowledgement, "ready\n❯ " + rename + "\n" + acknowledgement}, wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "repaint-is-not-proof-an-acknowledgement-scrolled", command: "/rename {name}", screens: []string{"ready\n❯ \n" + acknowledgement, "ready\nrepaint\n❯ " + rename + "\n" + acknowledgement}, wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "failed-delivery-does-not-discount-visible-command", command: "/rename {name}", screens: []string{"ready", "❯ " + rename + "\n" + acknowledgement + "\nready"}, sendErr: errors.New("Enter failed after typing"), wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "completion-timeout", command: "/rename {name}", screens: []string{"ready"}, wantSends: []string{rename, prompt}, wantWarning: "launch.rename_unconfirmed"},
		{name: "completion-timeout-leaves-a-draft", command: "/rename {name}", screens: []string{"ready", "ready\n" + rename}, wantSends: []string{rename}, wantCode: "launch.pane_ready_timeout", wantWarning: "launch.rename_unconfirmed"},
		{name: "after-argv", command: "/rename {name}", afterArgv: true, screens: []string{"ready", "ready\nrenamed kb-中文-Name_3.0"}, wantSends: []string{rename}},
		{name: "after-argv-dialog", command: "/rename {name}", afterArgv: true, screens: []string{"ready\ntrust", "ready"}, wantWarning: "launch.rename_not_ready"},
		{name: "after-argv-working-status-is-not-ready", command: "/rename {name}", afterArgv: true, busy: true, screens: []string{"ready"}, wantWarning: "launch.rename_not_ready"},
		{name: "after-argv-no-agent-status", command: "/rename {name}", afterArgv: true, terminal: plainTerminal, screens: []string{"ready"}, wantWarning: "launch.rename_no_status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			freezeClock(t)
			dir := t.TempDir()
			paneAgent := tc.paneAgent
			if paneAgent == "" {
				paneAgent = "sample"
			}
			b := &renameTestBackend{
				name: "rename-" + terminal.ProjectKey(dir), screens: tc.screens,
				programStatus: tc.terminal == "", agentIdentity: tc.terminal != plainTerminal, sendErr: tc.sendErr,
				facts: terminal.PaneFacts{Agent: paneAgent, AgentApp: "sample", AgentStatus: "idle"},
			}
			if tc.busy {
				b.facts.AgentStatus = "working"
			}
			terminal.Register(b)
			complete := "renamed {name}"
			if tc.completeMatch != "" {
				complete = tc.completeMatch
			}
			if tc.noComplete {
				complete = ""
			}
			agent := "sample"
			if tc.dialect != "" {
				agent = "wrapped"
			}
			var warnings []string
			plan := LaunchPlan{
				Launcher: b.Name(), agent: agent, agentDialect: tc.dialect, programStatusApp: "sample",
				PromptDelivery: config.PromptDelivery{Mode: tc.mode, Ready: &config.PromptReady{Match: "ready", TimeoutMS: 500}},
				rename: sessionRename{command: tc.command, RenameDelivery: config.RenameDelivery{
					Blocked:  []config.PromptBlocked{{Match: "trust", Reason: "confirm trust"}},
					Complete: complete, AfterArgv: tc.afterArgv,
				}},
				warning: func(message string) { warnings = append(warnings, message) },
			}
			argv := attachPrompt(&plan, []string{"agent"}, prompt)
			wantArgvPrompt := tc.mode != "pane" && (tc.command == "" || tc.afterArgv || tc.terminal == plainTerminal)
			if slices.Contains(argv, prompt) != wantArgvPrompt {
				t.Fatalf("argv = %q, want task prompt in argv: %t", argv, wantArgvPrompt)
			}
			locationAfter := -1
			location := func(LaunchOutcome) error { locationAfter = len(b.delivered); return nil }
			_, err := launchAgent(plan, filepath.Join(dir, "context"), label, process.ProcessInvocation{Argv: argv}, location, nil, nil, true)
			if tc.wantCode != "" {
				var failure *LaunchFailure
				detail := b.screens[len(b.screens)-1]
				var want error = launchError(tc.wantCode, detail)
				if tc.wantCode == "launch.pane_ready_blocked" {
					want = launchError(tc.wantCode, "confirm trust", detail)
				}
				if !errors.As(err, &failure) || failure.Err.Error() != want.Error() || failure.DeliveryUnknown || !b.stopped || locationAfter != -1 {
					t.Fatalf("expected clean rollback with no WINDOW write, got %v, closed=%t, window slot=%d", err, b.stopped, locationAfter)
				}
			} else if err != nil {
				t.Fatal(err)
			} else {
				wantLocationAfter := 0
				if !wantArgvPrompt {
					wantLocationAfter = len(tc.wantSends)
				}
				if locationAfter != wantLocationAfter || b.stopped {
					t.Fatalf("window slot=%d, want %d; closed=%t", locationAfter, wantLocationAfter, b.stopped)
				}
			}
			if !slices.Equal(b.delivered, tc.wantSends) {
				t.Fatalf("pane input=%q, want %q", b.delivered, tc.wantSends)
			}
			if tc.firstReadAt != 0 && b.deliveryReads[0] != tc.firstReadAt {
				t.Fatalf("rename sent on poll %d, want %d", b.deliveryReads[0], tc.firstReadAt)
			}
			var wantWarnings []string
			if tc.wantWarning != "" {
				wantWarnings = []string{config.Text(tc.wantWarning)}
			}
			if !slices.Equal(warnings, wantWarnings) {
				t.Errorf("warnings=%q, want %q", warnings, wantWarnings)
			}
		})
	}
}

func TestRenameDirectLaunchKeepsBaselineArgvAndWarns(t *testing.T) {
	resetLang(t)
	previous := startProcessFn
	t.Cleanup(func() { startProcessFn = previous })
	for _, launcher := range []string{"foreground", "console"} {
		t.Run(launcher, func(t *testing.T) {
			var warnings []string
			plan := LaunchPlan{Launcher: launcher, warning: func(s string) { warnings = append(warnings, s) }}
			want := attachPrompt(&plan, []string{"agent", "--interactive"}, "task instruction")
			plan.rename.command = "/rename {name}"
			argv := attachPrompt(&plan, []string{"agent", "--interactive"}, plan.Prompt)
			startProcessFn = func(got []string, _ map[string]string, _ string, _ bool) (*startedProc, error) {
				if !slices.Equal(got, want) {
					t.Fatalf("argv=%q, baseline=%q", got, want)
				}
				wait := make(chan int, 1)
				wait <- 0
				close(wait)
				return &startedProc{wait: wait}, nil
			}
			o, err := launchAgent(plan, filepath.Join(t.TempDir(), "context"), "kb-label", process.ProcessInvocation{Argv: argv}, nil, nil, nil)
			if err != nil {
				t.Fatal(err)
			}
			if code, err := o.Wait(); err != nil || code != 0 {
				t.Fatalf("exit=%d, %v", code, err)
			}
			if !slices.Equal(warnings, []string{config.Text("launch.rename_no_pane")}) {
				t.Errorf("warnings=%q", warnings)
			}
		})
	}
}
