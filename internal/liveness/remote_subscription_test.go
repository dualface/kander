package liveness

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func remoteSubscriptionService(t *testing.T) (*remote.Session, *client.Project) {
	t.Helper()
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "service"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	clients := make([]*client.Client, 2)
	var info api.Project
	for i, id := range []string{"observer-host", "executor-host"} {
		clients[i], err = client.New(httpServer.URL, id)
		if err != nil {
			t.Fatal(err)
		}
		info, err = clients[i].Register(t.Context(), "subscription-project", "")
		if err != nil {
			t.Fatal(err)
		}
	}
	service := &remote.Session{ServerURL: httpServer.URL, Client: clients[0], Project: clients[0].Project(info.ID), Info: info, Home: t.TempDir()}
	return service, clients[1].Project(info.ID)
}
func subscriptionRemoteCard(t *testing.T, p *client.Project, slug, group string) api.Task {
	t.Helper()
	task, err := p.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: "Remote subscriber fixture", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+group, 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	task, err = p.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return task
}
func TestRemoteSubscriptionWatchesGroupsThroughTheService(t *testing.T) {
	service, executor := remoteSubscriptionService(t)
	root := t.TempDir()
	t.Chdir(root)
	forbidden := filepath.Join(root, "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	group := "20261112-remote-watch-group"
	first := subscriptionRemoteCard(t, executor, "remote-first", group)
	events := make(runtimeEvents, 64)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		done <- subscribeContext(ctx, service.Location(""), subscribeOptions{Watch: []string{group}, Refresh: 0.01, Heartbeat: 0.05}, events, service)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("remote subscription did not join")
		}
	})
	initial := nextRuntimeEvent(t, events, func(event groupEvent) bool { return event.Event == "snapshot" })
	if initial.Tasks[first.TaskID] != "backlog" || initial.Cursor == "" || !initial.MembershipComplete {
		t.Fatalf("incomplete service snapshot: %+v", initial)
	}
	second := subscriptionRemoteCard(t, executor, "remote-second", group)
	changed := nextRuntimeEvent(t, events, func(event groupEvent) bool {
		return event.Event == "membership-change" && len(event.Memberships[group]) == 2
	})
	if changed.Tasks[second.TaskID] != "backlog" || changed.MembershipVersions[group] == initial.MembershipVersions[group] {
		t.Fatalf("new remote member was missed: %+v", changed)
	}
	if _, err := executor.Move(t.Context(), "", first.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	state := nextRuntimeEvent(t, events, func(event groupEvent) bool {
		return event.Event == "state-change" && event.Tasks[first.TaskID] == "todo"
	})
	if state.TaskRevisions[first.TaskID] <= initial.TaskRevisions[first.TaskID] {
		t.Fatal("service version was lost")
	}
	if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("subscription used a local board: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "kanban")); !os.IsNotExist(err) {
		t.Fatal("subscription created local kanban")
	}
}
func TestRemoteSubscriptionUsesGenerationBoundAgentObservation(t *testing.T) {
	service, executor := remoteSubscriptionService(t)
	group := "20261112-observe-watch-group"
	task := subscriptionRemoteCard(t, executor, "remote-agent", group)
	if _, err := executor.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	grant, err := executor.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: "executor-host", ExecutionID: "remote-agent-execution", Owner: "codex", Start: &api.StartInput{Session: "codex remote-session", Window: "tmux:$999:@999:%999"}})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	if _, err := executor.ObserveAgent(t.Context(), "", task.TaskID, api.AgentObservation{Status: "alive", Reason: "confirmed by the owning worker"}, proof); err != nil {
		t.Fatal(err)
	}
	subscription, err := newSubscription(subscribeOptions{Watch: []string{group}, Refresh: 0.01, Heartbeat: 0.1})
	if err != nil {
		t.Fatal(err)
	}
	subscription.service = service
	facts, err := subscription.readRemote(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	report := remoteObservation(task.TaskID, facts)
	if report.Status != Alive || !report.ObservationValid || report.Channel != "server" {
		t.Fatalf("remote pane was probed locally or observation lost: %+v", report)
	}
	// Simulate a local clock on another host. Freshness uses the service's
	// observation clock and translates the age only for local display.
	facts.observedAt = facts.observedAt.Add(24 * time.Hour)
	report = remoteObservation(task.TaskID, facts)
	if report.Status != Alive || facts.observedAt.Sub(report.ObservedAt) > time.Minute || report.ObservedAt.After(facts.observedAt) {
		t.Fatalf("client wall-clock offset invalidated service facts: %+v", report)
	}
}

func TestRemoteCheckReadsServiceEvidenceWithoutLocalKanban(t *testing.T) {
	service, executor := remoteSubscriptionService(t)
	root := t.TempDir()
	t.Chdir(root)
	forbidden := filepath.Join(root, "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	t.Setenv(remote.EnvClientHome, service.Home)
	t.Setenv(config.EnvConfig, filepath.Join(root, "scope-config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Server.URL = service.ServerURL
	cfg.Project.Name = service.Info.Name
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	task := subscriptionRemoteCard(t, executor, "check-remote", "")
	code, out, stderr := capture(t, func() int { return RunCheck([]string{task.TaskID}) })
	if code != 0 {
		t.Fatalf("remote check: %d %s %s", code, out, stderr)
	}
	if _, err := os.Lstat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("check used local board: %v", err)
	}
}
