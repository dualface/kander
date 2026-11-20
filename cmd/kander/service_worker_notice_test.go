package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/version"
)

// The fixture runs a worker that holds worker.lock and records this binary's
// version. A claim says nothing about it, until the record names another
// version.
func TestServiceClaimWarnsWhenTheRunningWorkerIsAnotherVersion(t *testing.T) {
	session, _ := moveServiceFixture(t)
	claim := func(slug string) string {
		task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: slug, Language: "en"})
		if err != nil {
			t.Fatal(err)
		}
		todoServiceCard(t, session, task)
		return runKander(t, "move", task.TaskID, "working", "--owner", "codex").require(t, "claim").stderr
	}
	if stderr := claim("same-worker"); strings.Contains(stderr, "background worker") {
		t.Fatalf("a worker of this version needs no notice:\n%s", stderr)
	}
	if err := remote.WriteWorkerRecord(session.Home, remote.WorkerRecord{Version: "v9.9.9", PID: 4711, Executable: "/old/kander", Handover: true, StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	stderr := claim("other-worker")
	for _, want := range []string{"kander v9.9.9", "kander " + version.String(), "pid 4711", "/old/kander", "does not hand over", "Stop process 4711"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("the claim warning lacks %q:\n%s", want, stderr)
		}
	}
}
