package remote

import (
	"errors"
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
	if err != nil || !found || current.AgentPID != 4242 || !current.RenewedAt.Equal(worker.RenewedAt) || len(current.Delivered) != 1 {
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
