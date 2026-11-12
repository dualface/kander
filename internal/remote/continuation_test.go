package remote

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

type continuationService struct {
	ProjectService
	task api.Task
}

func (s continuationService) Show(context.Context, string) (api.Task, error) { return s.task, nil }

func continuationTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	command := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, output, err)
	}
	return strings.TrimSpace(string(output))
}

func TestContinuationReconcilesExactSnapshotAndRefusesPartialReplay(t *testing.T) {
	source := t.TempDir()
	continuationTestGit(t, source, "init")
	if err := os.WriteFile(filepath.Join(source, "tracked.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	continuationTestGit(t, source, "add", "tracked.txt")
	continuationTestGit(t, source, "commit", "-m", "Base")
	base := continuationTestGit(t, source, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(source, "tracked.txt"), []byte("staged\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	continuationTestGit(t, source, "add", "tracked.txt")
	if err := os.WriteFile(filepath.Join(source, "tracked.txt"), []byte("working\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	value, err := captureContinuation(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	cl, _ := client.New("http://127.0.0.1:1", "local-client")
	credential := Credential{ClientID: "local-client", ProjectID: "project", TaskID: "20261112-continuation-task", ExecutionID: "executor", Generation: 2}
	s := &Session{Client: cl, Info: api.Project{ID: "project"}, Home: t.TempDir(), Project: continuationService{task: api.Task{Claim: &api.ClaimView{ClientID: credential.ClientID, ExecutionID: credential.ExecutionID, Generation: credential.Generation, Status: api.ClaimActive}}}}
	target := filepath.Join(t.TempDir(), "target")
	continuationTestGit(t, source, "worktree", "add", "--detach", target, base)
	workspace := ExecutionWorktree{Path: target, Base: base, ExecutionID: credential.ExecutionID, Generation: credential.Generation}
	if err := s.RestoreContinuation(t.Context(), workspace, value, credential); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreContinuation(t.Context(), workspace, value, credential); err != nil {
		t.Fatalf("exact recovery: %v", err)
	}
	if err := os.WriteFile(filepath.Join(target, "tracked.txt"), []byte("partial or subsequent work\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.RestoreContinuation(t.Context(), workspace, value, credential); err == nil {
		t.Fatal("reapplied a snapshot over changed content")
	}
	if got, _ := os.ReadFile(filepath.Join(target, "tracked.txt")); string(got) != "partial or subsequent work\n" {
		t.Fatal("failed reconciliation changed target")
	}
	if err := VerifyContinuation(t.Context(), source, value); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "late.txt"), []byte("late edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyContinuation(t.Context(), source, value); err == nil {
		t.Fatal("accepted a concurrently edited source")
	}
}
