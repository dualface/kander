package remote_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/remote"
)

type integrationChildInput struct {
	Server, Home, Client, Dir, Result string
	Info                              api.Project
	Options                           remote.IntegrationOptions
}

func TestIntegrationChildHelper(t *testing.T) {
	path := os.Getenv("KANDER_TEST_INTEGRATION_CHILD")
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var input integrationChildInput
	if err := json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	cl, err := client.New(input.Server, input.Client)
	if err != nil {
		t.Fatal(err)
	}
	cl.Retries = 0
	session := &remote.Session{Client: cl, Project: cl.Project(input.Info.ID), Info: input.Info, ServerURL: input.Server, Home: input.Home, WorkerStarter: func(string) error { return nil }}
	value, err := session.Integrate(t.Context(), input.Dir, input.Options)
	if err != nil {
		t.Fatal(err)
	}
	data, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(input.Result, data, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestIntegrationNewProcessRotatesOwnedTargetBeforeResuming(t *testing.T) {
	f := newIntegrationFixture(t)
	var hidden atomic.Bool
	hidden.Store(true)
	f.intercept = integrationDropAction(t, "pushing", &hidden)
	input := integrationChildInput{Server: f.a.ServerURL, Home: f.a.Home, Client: f.a.Client.ClientID(), Info: f.a.Info, Dir: f.first, Options: f.options("new-process-recovery"), Result: filepath.Join(f.root, "child-result.json")}
	data, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(f.root, "child-input.json")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	run := func() ([]byte, error) {
		cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestIntegrationChildHelper$", "-test.count=1")
		cmd.Env = append(os.Environ(), "KANDER_TEST_INTEGRATION_CHILD="+file)
		return cmd.CombinedOutput()
	}
	if output, err := run(); err == nil {
		t.Fatalf("first process should see hidden response: %s", output)
	}
	before, err := f.b.Project.Integration(t.Context(), api.IntegrationID(input.Options.TargetRef))
	if err != nil || before.Phase != "pushing" {
		t.Fatalf("durable first intent: %+v %v", before, err)
	}
	if got := deliveryGit(t, f.second, "ls-remote", "--refs", "origin", "refs/heads/v2"); !strings.HasPrefix(got, f.base+"\t") {
		t.Fatal("uncertain intent already changed target")
	}
	hidden.Store(false)
	if output, err := run(); err != nil {
		t.Fatalf("successor process: %s %v", output, err)
	}
	data, err = os.ReadFile(input.Result)
	if err != nil {
		t.Fatal(err)
	}
	var result api.Integration
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatal(err)
	}
	if result.Phase != "integrated" || result.Generation != before.Generation+1 || result.Request.ActorExecutionID != before.Request.ActorExecutionID {
		t.Fatalf("new process borrowed a target execution: %+v", result)
	}
	old, err := f.b.Project.IntegrationExecution(t.Context(), before.ID, before.Request.ExecutionID)
	if err != nil || old.Phase != "uncertain" || old.Status != api.ClaimReleased {
		t.Fatalf("old push authority was not audited and released: %+v %v", old, err)
	}
}
