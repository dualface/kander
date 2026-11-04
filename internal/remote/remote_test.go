package remote

import (
	"testing"

	"github.com/dualface/kander/internal/api"
)

func TestClientIDPersists(t *testing.T) {
	home := t.TempDir() + "/client"
	first, err := ClientID(home)
	if err != nil || !api.ValidIdentity(first) {
		t.Fatalf("client id: %q %v", first, err)
	}
	second, err := ClientID(home)
	if err != nil || second != first {
		t.Fatalf("client id must persist: %q != %q (%v)", second, first, err)
	}
}

// A process bound to one execution never receives a later execution's
// credential, even after this host took the task over again.
func TestCredentialIsBoundToTheExecution(t *testing.T) {
	s := &Session{Home: t.TempDir(), Info: api.Project{ID: "p-1"}}
	old := Credential{ProjectID: "p-1", TaskID: "20261104-demo-task", ExecutionID: "x-old", Generation: 1, Token: "t1"}
	next := Credential{ProjectID: "p-1", TaskID: "20261104-demo-task", ExecutionID: "x-new", Generation: 2, Token: "t2"}
	for _, c := range []Credential{old, next} {
		if err := s.SaveCredential(c); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv(EnvExecutionID, "x-old")
	got, found, err := s.Credential(old.TaskID)
	if err != nil || !found || got.Generation != 1 || got.Token != "t1" {
		t.Fatalf("the old execution must get its own credential: %+v %v %v", got, found, err)
	}
	t.Setenv(EnvExecutionID, "x-unknown")
	if _, found, _ := s.Credential(old.TaskID); found {
		t.Fatal("an unknown execution gets no credential")
	}
	t.Setenv(EnvExecutionID, "")
	if got, _, _ := s.Credential(old.TaskID); got.Generation != 2 {
		t.Fatalf("an interactive process uses the latest claim: %+v", got)
	}
	if err := RemoveCredential(s.Home, next); err != nil {
		t.Fatal(err)
	}
	if _, found, _ := s.Credential(old.TaskID); found {
		t.Fatal("removing the latest credential clears the interactive pointer")
	}
	all, err := Credentials(s.Home)
	if err != nil || len(all) != 1 || all[0].ExecutionID != "x-old" {
		t.Fatalf("listing: %+v %v", all, err)
	}
}
