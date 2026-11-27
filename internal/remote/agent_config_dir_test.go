package remote

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
)

func TestAgentConfigDirPrefersTheExecutionDirectory(t *testing.T) {
	for _, tc := range []struct {
		name, project, worktree, want string
	}{
		{"execution directory", "/work/project", "/work/project/worktrees/task", "/work/project"},
		{"worktree only", "", "/work/project/worktrees/task", "/work/project/worktrees/task"},
		{"neither", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Credential{ProjectDir: tc.project, Worktree: tc.worktree}
			if got := c.AgentConfigDir(); got != tc.want {
				t.Fatalf("AgentConfigDir() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCredentialWritesKeepTheRecordedExecutionDirectory(t *testing.T) {
	session := &Session{Home: t.TempDir(), Info: api.Project{ID: "project"}}
	initial := Credential{ProjectID: "project", TaskID: "20261127-project-dir-task", ExecutionID: "exec-one", Generation: 1, Token: "secret", RenewedAt: time.Now(), ProjectDir: "/work/project"}
	if err := session.SaveCredential(initial); err != nil {
		t.Fatal(err)
	}
	later := initial
	later.ProjectDir = ""
	later.Delivered = []string{"request-one"}
	if err := WriteCredential(session.Home, later); err != nil {
		t.Fatal(err)
	}
	current, found, err := session.Credential(initial.TaskID)
	if err != nil || !found || current.ProjectDir != initial.ProjectDir {
		t.Fatalf("a write without the execution directory dropped it: %+v %v", current, err)
	}
	other := initial
	other.ProjectDir = "/work/another"
	if err := WriteCredential(session.Home, other); err == nil || !strings.Contains(err.Error(), "managed agent identity conflict") {
		t.Fatalf("a different execution directory was accepted: %v", err)
	}
}

// A coordinator credential takes the execution directory of the orchestrator
// launch, or of the task execution that coordinates its own group.
func TestGroupCredentialTakesTheExecutionDirectory(t *testing.T) {
	const server, client, project = "http://server", "c-1", "project"
	group := func(execution string) Credential {
		return Credential{ResourceKind: "group", ServerURL: server, ClientID: client, ProjectID: project, TaskID: "20261127-coordinated-group", ExecutionID: execution, Generation: 1, Token: "group-secret"}
	}
	t.Run("orchestrator launch", func(t *testing.T) {
		home := t.TempDir()
		if err := SaveExecutionContext(home, ExecutionContext{ServerURL: server, ClientID: client, ProjectID: project, ExecutionID: "exec-orchestrator", Window: "herdr:w1:t1:w1:p1", Session: "codex id-1", ProjectDir: "/work/project"}); err != nil {
			t.Fatal(err)
		}
		c := group("exec-orchestrator")
		if err := RefreshExecutionContext(home, &c); err != nil {
			t.Fatal(err)
		}
		if c.ProjectDir != "/work/project" || c.AgentConfigDir() != "/work/project" {
			t.Fatalf("group credential = %+v", c)
		}
	})
	t.Run("task execution", func(t *testing.T) {
		home := t.TempDir()
		session := &Session{Home: home, ServerURL: server, Info: api.Project{ID: project}}
		task := Credential{ServerURL: server, ClientID: client, ProjectID: project, TaskID: "20261127-coordinating-task", ExecutionID: "exec-task", Generation: 1, Token: "task-secret", Window: "tmux:$1:@1:%1", Session: "codex id-2", ProjectDir: "/work/project"}
		if err := session.SaveCredential(task); err != nil {
			t.Fatal(err)
		}
		c := group("exec-task")
		if err := RefreshExecutionContext(home, &c); err != nil {
			t.Fatal(err)
		}
		if c.ProjectDir != "/work/project" || c.Window != task.Window {
			t.Fatalf("group credential = %+v", c)
		}
	})
}
