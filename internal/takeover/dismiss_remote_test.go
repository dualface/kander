package takeover

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/terminal"
)

const (
	dismissGroup       = "20261118-dismiss-group"
	dismissCoordinator = "coordinator-exec"
	dismissClaim       = "finished-executor"
	dismissToken       = "removed-secret"
)

// remoteDismiss is one done card on an in-process test server, claimed by this
// host's client, whose terminal is the fake herdr of setupBoard.
type remoteDismiss struct {
	root, task, home, identity, forbidden string
	card                                  api.Task
	record                                board.ExecutionRecord
	service                               *remote.Session
	// onShow may change the served card; n counts the reads of it.
	onShow func(n int, card *api.Task)
	shows  int
}

func bindDismissProcess(t *testing.T, execution, coordinator string) {
	t.Helper()
	for name, value := range map[string]string{remote.EnvExecutionID: execution, remote.EnvCoordinatorExecutionID: coordinator} {
		t.Setenv(name, value)
		if value == "" {
			if err := os.Unsetenv(name); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func newRemoteDismiss(t *testing.T, slug string) *remoteDismiss {
	t.Helper()
	// A user terminal definition named herdr would replace the fake terminal.
	// Only the embedded definitions take part, whatever the share directories
	// of this machine or of the checkout hold.
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	saved := terminal.DefinitionDirs
	terminal.DefinitionDirs = func() []terminal.DefinitionDir { return nil }
	terminal.ReloadDefinitions()
	t.Cleanup(func() {
		terminal.DefinitionDirs = saved
		terminal.ReloadDefinitions()
	})
	root, _ := setupBoard(t)
	task, path := makeDone(t, root, slug, "herdr:w1:t8:w1:p8")
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := board.CurrentExecution(root, task)
	if err != nil {
		t.Fatal(err)
	}
	d := &remoteDismiss{root: root, task: task, record: record}
	cwd := t.TempDir()
	t.Chdir(cwd)
	d.forbidden = filepath.Join(cwd, "forbidden-board")
	t.Setenv(board.EnvBoardDir, d.forbidden)
	d.home = filepath.Join(t.TempDir(), "client")
	t.Setenv(remote.EnvClientHome, d.home)
	if d.identity, err = remote.ClientID(d.home); err != nil {
		t.Fatal(err)
	}
	project := api.Project{ID: "dismiss-project", Name: "dismiss-project"}
	d.card = api.Task{TaskID: task, Text: string(text), State: "done", Revision: 10, Claim: &api.ClaimView{ClientID: d.identity, ExecutionID: dismissClaim, Generation: 1, Status: api.ClaimReleased}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.VersionInfo{Protocol: api.ProtocolVersion, MinProtocol: api.MinProtocolVersion})
	})
	mux.HandleFunc("POST /api/v1/projects", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(project) })
	mux.HandleFunc("GET /api/v1/projects/{project}/tasks/{task}", func(w http.ResponseWriter, r *http.Request) {
		d.shows++
		served := d.card
		if d.onShow != nil {
			d.onShow(d.shows, &served)
		}
		_ = json.NewEncoder(w).Encode(served)
	})
	mux.HandleFunc("GET /api/v1/projects/{project}/tasks/{task}/execution/record", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(d.record)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Server.URL, cfg.Project.Name = srv.URL, project.Name
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	if d.service, err = remote.Open(t.Context(), cfg, cwd); err != nil {
		t.Fatal(err)
	}
	return d
}

// retain saves the member's credential and removes it, which leaves the
// retained execution record that dismiss reads after the claim is released.
func (d *remoteDismiss) retain(t *testing.T, change func(*remote.Credential)) {
	t.Helper()
	credential := remote.Credential{ServerURL: d.service.ServerURL, ClientID: d.identity, ProjectID: d.service.Info.ID, TaskID: d.task, ExecutionID: dismissClaim, Generation: 1, Token: dismissToken, Session: d.record.Session, Window: board.MetadataFrom(d.card.Text, board.FieldWindow)}
	if change != nil {
		change(&credential)
	}
	if err := d.service.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	if err := remote.RemoveCredential(d.home, credential); err != nil {
		t.Fatal(err)
	}
}

func (d *remoteDismiss) joinGroup(t *testing.T) {
	t.Helper()
	d.card.Text = strings.Replace(d.card.Text, "- TASK_GROUP:", "- TASK_GROUP: "+dismissGroup, 1)
	if board.TaskGroupFrom(d.card.Text) != dismissGroup {
		t.Fatal("the fixture card did not take the group")
	}
}

func (d *remoteDismiss) saveGroupCredential(t *testing.T, change func(*remote.Credential)) {
	t.Helper()
	credential := remote.Credential{ResourceKind: "group", ServerURL: d.service.ServerURL, ClientID: d.identity, ProjectID: d.service.Info.ID, TaskID: dismissGroup, ExecutionID: dismissCoordinator, Generation: 1, Token: dismissToken}
	if change != nil {
		change(&credential)
	}
	if err := d.service.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
}

func (d *remoteDismiss) run(t *testing.T) (code int, stdout, stderr string) {
	t.Helper()
	stdout, stderr, _ = capture(t, func() error {
		code = RunDismiss([]string{d.task})
		return nil
	})
	return code, stdout, stderr
}

func (d *remoteDismiss) receipts(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(d.home, "projects", "*", "launch-actions", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func (d *remoteDismiss) terminalLog(name string) bool {
	_, err := os.Stat(filepath.Join(d.root, "herdr.log."+name))
	return err == nil
}

// requireRefused checks one refusal: a failing exit, the common sentence, the
// condition text with its values, no token, no terminal operation and no
// dismiss receipt.
func (d *remoteDismiss) requireRefused(t *testing.T, want ...string) {
	t.Helper()
	code, _, stderr := d.run(t)
	if code == 0 {
		t.Fatal("dismiss was accepted")
	}
	if !strings.Contains(stderr, config.Text("remote.local_execution_required")) {
		t.Fatalf("refusal lacks the common sentence: %s", stderr)
	}
	for _, fragment := range want {
		if !strings.Contains(stderr, fragment) {
			t.Fatalf("refusal lacks %q: %s", fragment, stderr)
		}
	}
	if strings.Contains(stderr, dismissToken) {
		t.Fatalf("refusal leaks a token: %s", stderr)
	}
	if d.terminalLog("prompt") || d.terminalLog("close") {
		t.Fatal("a terminal operation ran before the refusal")
	}
	if files := d.receipts(t); len(files) != 0 {
		t.Fatalf("a refused dismiss wrote a receipt: %v", files)
	}
}

func TestRemoteDismissUsesRetainedLocalRoutingAfterSecretRemoval(t *testing.T) {
	d := newRemoteDismiss(t, "remote-dismiss")
	bindDismissProcess(t, "", "")
	d.retain(t, nil)
	if _, found, err := d.service.Credential(d.task); err != nil || found {
		t.Fatalf("released secret remained: %v", err)
	}
	if code, _, stderr := d.run(t); code != 0 {
		t.Fatalf("dismiss exit %d: %s", code, stderr)
	}
	if !d.terminalLog("close") {
		t.Fatal("owned container was not closed")
	}
	if code, out, _ := d.run(t); code != 0 || !strings.Contains(out, t_("remote.execution_dismissed", d.task)) {
		t.Fatalf("completed receipt replay exit %d: %s", code, out)
	}
	if _, err := os.Stat(d.forbidden); !os.IsNotExist(err) {
		t.Fatalf("remote dismiss read local board: %v", err)
	}
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "foreign"))
	if code, _, _ := d.run(t); code == 0 {
		t.Fatal("foreign client dismissed an execution")
	}
}

func t_(id string, args ...any) string { return config.Text(id, args...) }

func TestRemoteDismissLetsTheCoordinatorCleanUpAMemberOfItsGroup(t *testing.T) {
	d := newRemoteDismiss(t, "coordinator-dismiss")
	d.joinGroup(t)
	d.retain(t, nil)
	d.saveGroupCredential(t, nil)
	bindDismissProcess(t, dismissCoordinator, dismissCoordinator)
	code, out, stderr := d.run(t)
	if code != 0 || !strings.Contains(out, t_("remote.execution_dismissed", d.task)) {
		t.Fatalf("coordinator dismiss exit %d: %s %s", code, out, stderr)
	}
	if !d.terminalLog("prompt") || !d.terminalLog("close") {
		t.Fatal("the member terminal was not told to exit and closed")
	}
}

func TestRemoteDismissRefusesProcessesThatDoNotOwnTheMember(t *testing.T) {
	empty := t_("remote.local_execution.value_empty")
	cases := []struct {
		name                   string
		execution, coordinator string
		arrange                func(*testing.T, *remoteDismiss)
		want                   []string
	}{
		{"task executor", "predecessor-exec", "", func(t *testing.T, d *remoteDismiss) { d.joinGroup(t); d.saveGroupCredential(t, nil) },
			[]string{"predecessor-exec", dismissClaim, empty}},
		{"differing variables", dismissCoordinator, "other-coordinator", func(t *testing.T, d *remoteDismiss) { d.joinGroup(t); d.saveGroupCredential(t, nil) },
			[]string{dismissCoordinator, "other-coordinator"}},
		{"claim execution with another coordinator", dismissClaim, "other-coordinator", func(t *testing.T, d *remoteDismiss) { d.joinGroup(t); d.saveGroupCredential(t, nil) },
			[]string{dismissClaim, "other-coordinator"}},
		{"coordinator of a card without a group", dismissCoordinator, dismissCoordinator, func(t *testing.T, d *remoteDismiss) { d.saveGroupCredential(t, nil) },
			[]string{dismissCoordinator, "-refused-dismiss-task"}},
		{"coordinator without the group credential", dismissCoordinator, dismissCoordinator, func(t *testing.T, d *remoteDismiss) { d.joinGroup(t) },
			[]string{dismissGroup, dismissCoordinator, t_("remote.local_execution.reason_missing")}},
		{"coordinator that lost the group credential", dismissCoordinator, dismissCoordinator, func(t *testing.T, d *remoteDismiss) {
			d.joinGroup(t)
			d.saveGroupCredential(t, func(c *remote.Credential) { c.Lost, c.LostReason = true, "taken_over" })
		}, []string{dismissGroup, "taken_over"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newRemoteDismiss(t, "refused-dismiss")
			d.retain(t, nil)
			tc.arrange(t, d)
			bindDismissProcess(t, tc.execution, tc.coordinator)
			d.requireRefused(t, tc.want...)
		})
	}
}

func TestRemoteDismissRefusesAGroupCredentialWithAWrongIdentity(t *testing.T) {
	d := newRemoteDismiss(t, "wrong-group-credential")
	d.joinGroup(t)
	d.retain(t, nil)
	d.saveGroupCredential(t, nil)
	path := filepath.Join(d.home, "executions")
	var found string
	_ = filepath.WalkDir(path, func(p string, e os.DirEntry, err error) error {
		if err == nil && filepath.Base(p) == dismissCoordinator+".json" {
			found = p
		}
		return nil
	})
	if found == "" {
		t.Fatal("the group credential was not saved")
	}
	data, err := os.ReadFile(found)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	fields["execution_id"] = "someone-else"
	if data, err = json.Marshal(fields); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(found, data, 0o600); err != nil {
		t.Fatal(err)
	}
	bindDismissProcess(t, dismissCoordinator, dismissCoordinator)
	d.requireRefused(t, t_("remote.local_execution.reason_mismatch"))
}

func TestRemoteDismissRefusalsNameTheFailedCondition(t *testing.T) {
	t.Run("foreign client", func(t *testing.T) {
		d := newRemoteDismiss(t, "foreign-client")
		d.retain(t, nil)
		bindDismissProcess(t, "", "")
		foreign := filepath.Join(t.TempDir(), "foreign")
		t.Setenv(remote.EnvClientHome, foreign)
		other, err := remote.ClientID(foreign)
		if err != nil {
			t.Fatal(err)
		}
		d.requireRefused(t, d.identity, other)
	})
	t.Run("scene differs from the card", func(t *testing.T) {
		d := newRemoteDismiss(t, "scene-differs")
		d.retain(t, func(c *remote.Credential) { c.Window = "herdr:w1:t8:w1:p9" })
		bindDismissProcess(t, "", "")
		d.requireRefused(t, "herdr:w1:t8:w1:p9", "herdr:w1:t8:w1:p8", d.record.Session)
	})
	t.Run("self-claimed record", func(t *testing.T) {
		d := newRemoteDismiss(t, "self-claimed")
		d.retain(t, nil)
		d.record.Kind = board.ExecutionClaim
		bindDismissProcess(t, "", "")
		d.requireRefused(t, d.task, board.ExecutionClaim)
	})
	t.Run("record owner differs", func(t *testing.T) {
		d := newRemoteDismiss(t, "record-owner")
		d.retain(t, nil)
		d.record.Owner = "someone-else"
		bindDismissProcess(t, "", "")
		d.requireRefused(t, t_("remote.local_execution.record_owner", `"someone-else"`, `"claude"`))
	})
	t.Run("session without a reference", func(t *testing.T) {
		d := newRemoteDismiss(t, "session-invalid")
		d.card.Text = strings.Replace(d.card.Text, "- SESSION: claude session-1", "- SESSION: claude", 1)
		d.record.Session = "claude"
		d.retain(t, func(c *remote.Credential) { c.Session = "claude" })
		bindDismissProcess(t, "", "")
		d.requireRefused(t, "claude")
	})
	t.Run("card changed after the read", func(t *testing.T) {
		d := newRemoteDismiss(t, "task-changed")
		d.retain(t, nil)
		d.onShow = func(n int, card *api.Task) {
			if n >= 2 {
				card.Revision = 11
			}
		}
		bindDismissProcess(t, "", "")
		code, _, stderr := d.run(t)
		if code == 0 || !strings.Contains(stderr, t_("remote.local_execution.change_revision", 10, 11)) || !strings.Contains(stderr, d.task) {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		if d.terminalLog("prompt") || d.terminalLog("close") {
			t.Fatal("a terminal operation ran after the card changed")
		}
	})
	t.Run("pane no longer matches", func(t *testing.T) {
		d := newRemoteDismiss(t, "pane-stale")
		d.retain(t, nil)
		bindDismissProcess(t, "", "")
		changeAgentAfterFirstProbe(t, d.root)
		code, _, stderr := d.run(t)
		if code == 0 || !strings.Contains(stderr, t_("remote.local_execution_required")) || !strings.Contains(stderr, "w1:p8") {
			t.Fatalf("exit %d: %s", code, stderr)
		}
		if d.terminalLog("prompt") || d.terminalLog("close") {
			t.Fatal("a terminal operation ran against a stale pane")
		}
	})
}

// changeAgentAfterFirstProbe makes the fake herdr report another agent from
// the second pane lookup on, as when the pane is reused between two probes.
func changeAgentAfterFirstProbe(t *testing.T, root string) {
	t.Helper()
	real := filepath.Join(root, "fake-bin", "herdr")
	if err := os.Rename(real, real+".real"); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
if [ "$1" = "pane" ] && [ "$2" = "get" ]; then
  count="$KANBAN_HERDR_LOG.count"
  n=$(cat "$count" 2>/dev/null || echo 0)
  n=$((n+1))
  echo "$n" > "$count"
  if [ "$n" -ge 2 ]; then
    printf '%s\n' "{\"id\":\"cli:pane:get\",\"result\":{\"pane\":{\"pane_id\":\"$3\",\"tab_id\":\"w1:t8\",\"agent\":\"codex\",\"agent_status\":\"idle\",\"agent_session\":{\"value\":\"session-1\"}}}}"
    exit 0
  fi
fi
exec "$0.real" "$@"
`
	if err := os.WriteFile(real, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteDismissRecordsAnAbsentTerminalAsComplete(t *testing.T) {
	d := newRemoteDismiss(t, "terminal-gone")
	d.retain(t, nil)
	bindDismissProcess(t, "", "")
	t.Setenv("KANBAN_HERDR_STALE_PANE", "w1:p8")
	code, out, stderr := d.run(t)
	window := board.MetadataFrom(d.card.Text, board.FieldWindow)
	if code != 0 || !strings.Contains(out, t_("remote.execution_target_gone", d.task, window)) {
		t.Fatalf("exit %d: %s %s", code, out, stderr)
	}
	files := d.receipts(t)
	if len(files) != 1 {
		t.Fatalf("receipts %v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil || !strings.Contains(string(data), `"phase":"complete"`) {
		t.Fatalf("receipt is not complete: %s %v", data, err)
	}
	if d.terminalLog("prompt") || d.terminalLog("close") {
		t.Fatal("a terminal operation ran for an absent terminal")
	}
	if code, out, _ := d.run(t); code != 0 || !strings.Contains(out, t_("remote.execution_dismissed", d.task)) {
		t.Fatalf("replay exit %d: %s", code, out)
	}
}
