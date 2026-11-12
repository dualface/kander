package launch

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

type lostRecoveryResponse struct {
	remote.ProjectService
	hidden bool
}

func (p *lostRecoveryResponse) CoordinatorAction(ctx context.Context, key, group, action string, input any, proof client.Claim) (board.CoordinatorCheckpoint, error) {
	value, err := p.ProjectService.CoordinatorAction(ctx, key, group, action, input, proof)
	if err == nil && !p.hidden {
		p.hidden = true
		return board.CoordinatorCheckpoint{}, io.ErrUnexpectedEOF
	}
	return value, err
}
func TestRemoteGroupRecoveryReplaysFrozenRequestAfterUnknownResponse(t *testing.T) {
	var elapsed atomic.Int64
	initial := time.Now().UTC()
	srv, err := server.Open(server.Options{DataDir: t.TempDir(), Now: func() time.Time { return initial.Add(time.Duration(elapsed.Load())) }, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	cwd := t.TempDir()
	t.Chdir(cwd)
	forbidden := filepath.Join(cwd, "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	home := filepath.Join(t.TempDir(), "client")
	t.Setenv(remote.EnvClientHome, home)
	cfg := config.DefaultConfig()
	cfg.Server.URL = httpServer.URL
	cfg.Project.Name = "recovery-client"
	session, err := remote.Open(t.Context(), cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	group := "20261112-recovery-client-group"
	var members []string
	for _, slug := range []string{"recover-one", "recover-two"} {
		task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: "Recover a lost member", Language: "en"})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
		task, err = session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
		if err != nil {
			t.Fatal(err)
		}
		task, err = session.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil)
		if err != nil {
			t.Fatal(err)
		}
		members = append(members, task.TaskID)
	}
	coordinator, err := session.AcquireCoordinator(t.Context(), board.CoordinatorClaim{GroupID: group, Owner: "codex", Token: "public-label", Basis: "confirmed recovery fixture", Members: members}, "confirmed recovery fixture")
	if err != nil {
		t.Fatal(err)
	}
	other, err := client.New(httpServer.URL, "predecessor-host")
	if err != nil {
		t.Fatal(err)
	}
	taskGrant, err := other.Project(session.Info.ID).Claim(t.Context(), "", members[0], api.ClaimRequest{ClientID: "predecessor-host", ExecutionID: "predecessor-execution", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	proof, err := session.CoordinatorAuthority(group, nil)
	if err != nil {
		t.Fatal(err)
	}
	reconcile := board.CoordinatorReconcile{GroupID: group, ExpectedRevision: coordinator.Checkpoint.Revision, Authority: coordinator.Checkpoint.Authority, Members: map[string]board.CoordinatorObservation{}}
	for _, id := range members {
		task, err := session.Project.Show(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		reconcile.Members[id] = board.CoordinatorObservation{Revision: task.Revision}
	}
	reconcile.Members[members[0]] = board.CoordinatorObservation{Revision: taskGrant.Task.Revision, DispatchID: "missing-round", Epoch: 1}
	blocked, err := session.Project.CoordinatorAction(t.Context(), "", group, "reconcile", api.GroupReconcile{Request: reconcile}, proof)
	if err != nil {
		t.Fatal(err)
	}
	request := board.CoordinatorRecover{GroupID: group, Member: members[0], ExpectedRevision: blocked.Revision, ExpectedEpoch: blocked.Authority.Epoch, Decision: "user explicitly recovers the lost member", Reason: "restore the execution cycle"}
	elapsed.Store(int64(45 * time.Second))
	if _, err := session.Project.RenewGroup(t.Context(), group, proof); err != nil {
		t.Fatal(err)
	}
	elapsed.Store(int64(91 * time.Second))
	service := session.Project
	session.Project = &lostRecoveryResponse{ProjectService: service}
	if _, err := recoverRemoteCoordinatorMember(t.Context(), session, request); err == nil {
		t.Fatal("hidden response reported as success")
	}
	after, err := service.Show(t.Context(), members[0])
	if err != nil || after.Claim.Generation != 2 {
		t.Fatalf("server did not commit recovery: %+v %v", after, err)
	}
	// A restarted command has no current group credential. Only the frozen call
	// can reconcile the prior effect; acquiring fresh authority would fail.
	credentials, err := remote.Credentials(home)
	if err != nil || len(credentials) != 1 {
		t.Fatalf("credentials: %+v %v", credentials, err)
	}
	if err := remote.RemoveCredential(home, credentials[0]); err != nil {
		t.Fatal(err)
	}
	elapsed.Store(int64(200 * time.Second))
	reopened := *session
	reopened.Project = service
	value, err := recoverRemoteCoordinatorMember(t.Context(), &reopened, request)
	if err != nil || len(value.Recoveries) != 1 {
		t.Fatalf("frozen retry: %+v %v", value, err)
	}
	replay, err := recoverRemoteCoordinatorMember(t.Context(), &reopened, request)
	if err != nil || replay.ContentHash != value.ContentHash {
		t.Fatalf("completed retry: %+v %v", replay, err)
	}
	final, err := service.Show(t.Context(), members[0])
	if err != nil || final.Revision != after.Revision || final.Claim.Generation != 2 {
		t.Fatal("recovery repeated the reset")
	}
	if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("recovery used local board: %v", err)
	}
}
