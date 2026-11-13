package remote

import (
	"errors"
	"github.com/dualface/kander/internal/client"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
)

func TestCredentialWritersPreserveLauncherAndWorkerFacts(t *testing.T) {
	session := &Session{Home: t.TempDir(), Info: api.Project{ID: "project"}}
	initial := Credential{ProjectID: "project", TaskID: "20261112-credential-task", ExecutionID: "exec-one", Generation: 1, Token: "secret", RenewedAt: time.Now()}
	if err := session.SaveCredential(initial); err != nil {
		t.Fatal(err)
	}
	launcher, worker := initial, initial
	launcher.AgentPID = 4242
	launcher.Worktree, launcher.Branch, launcher.BaseCommit = "/private/worktree", "execution-branch", "base-sha"
	worker.RenewedAt = initial.RenewedAt.Add(time.Minute)
	worker.Delivered = []string{"request-one"}
	var writers sync.WaitGroup
	failures := make(chan error, 2)
	for _, value := range []Credential{launcher, worker} {
		writers.Go(func() { failures <- WriteCredential(session.Home, value) })
	}
	writers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	current, found, err := session.Credential(initial.TaskID)
	if err != nil || !found || current.AgentPID != 4242 || current.Worktree != launcher.Worktree || current.Branch != launcher.Branch || current.BaseCommit != launcher.BaseCommit || !current.RenewedAt.Equal(worker.RenewedAt) || len(current.Delivered) != 1 {
		t.Fatalf("independent execution facts were lost: %+v %v", current, err)
	}
	current.Lost, current.LostReason = true, "lease expired"
	if err := WriteCredential(session.Home, current); err != nil {
		t.Fatal(err)
	}
	// A delayed launch callback was prepared before the worker recorded loss.
	launcher.Delivered = []string{"request-two"}
	if err := WriteCredential(session.Home, launcher); err != nil {
		t.Fatal(err)
	}
	current, _, err = session.Credential(initial.TaskID)
	if err != nil || !current.Lost || current.LostReason != "lease expired" || len(current.Delivered) != 2 {
		t.Fatalf("late metadata erased irreversible worker facts: %+v %v", current, err)
	}
}

func TestCredentialReleaseFencesLateWritesAndKeepsSuccessorPointer(t *testing.T) {
	session := &Session{Home: t.TempDir(), Info: api.Project{ID: "project"}}
	old := Credential{ProjectID: "project", TaskID: "20261112-credential-task", ExecutionID: "exec-old", Generation: 1, Token: "old-secret"}
	next := old
	next.ExecutionID, next.Generation, next.Token = "exec-next", 2, "next-secret"
	if err := session.SaveCredential(old); err != nil {
		t.Fatal(err)
	}
	var writers sync.WaitGroup
	failures := make(chan error, 2)
	writers.Go(func() { failures <- RemoveCredential(session.Home, old) })
	writers.Go(func() { failures <- session.SaveCredential(next) })
	writers.Wait()
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := WriteCredential(session.Home, old); !errors.Is(err, ErrCredentialReleased) {
		t.Fatalf("late writer recreated a released secret: %v", err)
	}
	current, found, err := session.Credential(next.TaskID)
	if err != nil || !found || current.ExecutionID != next.ExecutionID {
		t.Fatalf("old release erased the successor pointer: %+v %v", current, err)
	}
	all, err := Credentials(session.Home)
	if err != nil || len(all) != 1 || all[0].ExecutionID != next.ExecutionID {
		t.Fatalf("released execution was rediscovered by the worker: %+v %v", all, err)
	}
}

func TestCredentialHeadNeverRegressesAfterSuccessorRelease(t *testing.T) {
	session := &Session{Home: t.TempDir(), Info: api.Project{ID: "project"}}
	old := Credential{ProjectID: "project", TaskID: "20261112-generation-task", ExecutionID: "old-execution", Generation: 1, Token: "old"}
	if err := session.SaveCredential(old); err != nil {
		t.Fatal(err)
	}
	next := old
	next.Generation, next.ExecutionID, next.Token = 2, "next-execution", "next"
	if err := session.SaveCredential(next); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveCredential(old); err == nil {
		t.Fatal("late initial save replaced the successor")
	}
	if err := RemoveCredential(session.Home, next); err != nil {
		t.Fatal(err)
	}
	if err := session.SaveCredential(old); err == nil {
		t.Fatal("successor release let an old execution reclaim latest")
	}
	if _, found, err := session.Credential(old.TaskID); err != nil || found {
		t.Fatalf("released generation became active: %v %v", found, err)
	}
}

func TestCredentialRemovalRetainsLatestRoutingWithoutAnySecret(t *testing.T) {
	cl, _ := client.New("http://127.0.0.1:1", "local-client")
	session := &Session{Home: t.TempDir(), Info: api.Project{ID: "project"}, Client: cl, ServerURL: "http://127.0.0.1:1"}
	old := Credential{ServerURL: session.ServerURL, ClientID: cl.ClientID(), ProjectID: "project", TaskID: "20261112-retained-task", ExecutionID: "executor", Generation: 1, Token: "never-retain-this-secret"}
	if err := session.SaveCredential(old); err != nil {
		t.Fatal(err)
	}
	latest := old
	latest.Session, latest.Window, latest.Worktree = "claude retained", "tmux:$1:@1:%1", "/private/worktree"
	if err := WriteCredential(session.Home, latest); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCredential(session.Home, old); err != nil {
		t.Fatal(err)
	}
	if err := RemoveCredential(session.Home, old); err != nil {
		t.Fatal(err)
	}
	task := api.Task{TaskID: old.TaskID, Claim: &api.ClaimView{ClientID: old.ClientID, ExecutionID: old.ExecutionID, Generation: old.Generation, Status: api.ClaimReleased}}
	retained, err := session.Retained(task)
	if err != nil || retained.Session != latest.Session || retained.Window != latest.Window || retained.Worktree != latest.Worktree {
		t.Fatalf("retained facts: %+v %v", retained, err)
	}
	data, err := os.ReadFile(filepath.Join(session.Home, "retained-executions", projectNamespace(old.ServerURL, old.ProjectID), old.TaskID, old.ExecutionID+".json"))
	if err != nil || strings.Contains(string(data), old.Token) || strings.Contains(string(data), "token") {
		t.Fatalf("retained a secret: %v", err)
	}
	t.Setenv(EnvExecutionID, "predecessor")
	if _, err := session.Retained(task); err == nil {
		t.Fatal("predecessor borrowed successor routing")
	}
}

func TestCredentialSessionDiscoveryIsMonotonic(t *testing.T) {
	session := &Session{Home: t.TempDir(), Info: api.Project{ID: "project"}}
	initial := Credential{ProjectID: "project", TaskID: "20261112-session-task", ExecutionID: "executor", Generation: 1, Token: "secret", Session: "devin"}
	if err := session.SaveCredential(initial); err != nil {
		t.Fatal(err)
	}
	discovered := initial
	discovered.Session = "devin discovered-reference"
	if err := WriteCredential(session.Home, discovered); err != nil {
		t.Fatal(err)
	}
	if err := WriteCredential(session.Home, initial); err != nil {
		t.Fatal(err)
	}
	actual, _, err := session.Credential(initial.TaskID)
	if err != nil || actual.Session != discovered.Session {
		t.Fatalf("discovery regressed: %+v %v", actual, err)
	}
	discovered.Session = "devin another-reference"
	if err := WriteCredential(session.Home, discovered); err == nil {
		t.Fatal("replaced an immutable discovered session")
	}
}
