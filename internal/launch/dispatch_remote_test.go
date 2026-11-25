package launch

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command/commandtest"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func TestRemoteDispatchCLIUsesHolderAuthorityAndProposesForForeignHolder(t *testing.T) {
	setupBoard(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	forbidden := filepath.Join(cwd, "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	home := filepath.Join(t.TempDir(), "holder")
	t.Setenv(remote.EnvClientHome, home)
	srv, err := server.Open(server.Options{DataDir: filepath.Join(t.TempDir(), "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	cfg := envConfig("claude", "tmux", nil)
	cfg.Server.URL, cfg.Project.Name = httpServer.URL, "dispatch-project"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	session, err := remote.Open(t.Context(), cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	project := session.Project
	card, err := project.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "dispatch-cli", Title: "Dispatch through service", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(card.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: dispatch-cli", 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	card, err = project.PutDocument(t.Context(), "", card.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = project.Move(t.Context(), "", card.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	grant, err := project.Claim(t.Context(), "", card.TaskID, api.ClaimRequest{ClientID: session.Client.ClientID(), ExecutionID: "dispatch-holder", Owner: "claude", Start: &api.StartInput{Session: "claude fixture", Window: "tmux:$1:@1:%1"}})
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{ServerURL: httpServer.URL, ClientID: session.Client.ClientID(), ProjectID: session.Info.ID, TaskID: card.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}
	if err := session.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Execution(t.Context(), "", card.TaskID, api.ExecutionUpdate{Action: api.ExecutionConfirm}, *credential.Claim()); err != nil {
		t.Fatal(err)
	}
	input := board.DispatchInput{ID: "cli-sync", TaskID: card.TaskID, Kind: "sync", Base: strings.Repeat("a", 40), Message: "Inspect this synchronization"}
	path := filepath.Join(cwd, "intent.json")
	write := func() {
		t.Helper()
		data, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	code, out := captureStdout(t, func() int { return RunDispatchRemote([]string{"prepare", path}) })
	if code != 0 {
		t.Fatalf("holder prepare: %d %s", code, out)
	}
	var prepared board.Dispatch
	if err := json.Unmarshal([]byte(out), &prepared); err != nil || prepared.State != board.DispatchPrepared {
		t.Fatalf("prepare: %s %v", out, err)
	}
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "collaborator"))
	input.ID = "proposed-sync"
	write()
	code, out = captureStdout(t, func() int { return RunDispatchRemote([]string{"prepare", path}) })
	if code != 0 {
		t.Fatalf("foreign proposal: %d %s", code, out)
	}
	var request api.CollabRequest
	if err := json.Unmarshal([]byte(out), &request); err != nil || request.Status != api.RequestOpen || request.Dispatch == nil || request.CardRevision == 0 {
		t.Fatalf("proposal: %s %v", out, err)
	}
	code, retryOut := captureStdout(t, func() int { return RunDispatchRemote([]string{"prepare", path}) })
	var same api.CollabRequest
	if err := json.Unmarshal(retryOut, &same); code != 0 || err != nil || same.ID != request.ID {
		t.Fatalf("proposal replay: %d %s %v", code, retryOut, err)
	}
	if _, err := project.Dispatch(t.Context(), card.TaskID, input.ID); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("foreign proposal became a dispatch: %v", err)
	}
	if code := commandtest.Run(remote.NewRequestCommand(), "show", card.TaskID, request.ID, "--json"); code != 0 {
		t.Fatalf("request inspection failed: %d", code)
	}

	// With --json the forms print the same objects and nothing on stderr: the holder's show and
	// the end of the prepared dispatch.
	t.Setenv(remote.EnvClientHome, home)
	run := func(args ...string) (int, string, string) {
		return commandtest.Capture(func() int { return RunDispatchRemote(append(args, "--json")) })
	}
	code, shown, diagnostics := run("show", card.TaskID, "cli-sync")
	var viewed board.Dispatch
	if err := json.Unmarshal([]byte(shown), &viewed); code != 0 || diagnostics != "" || err != nil || viewed.State != board.DispatchPrepared || strings.Count(shown, "\n") != 1 {
		t.Fatalf("show --json: %d %q %q %v", code, shown, diagnostics, err)
	}
	code, ended, diagnostics := run("fail", card.TaskID, "cli-sync", strconv.FormatUint(viewed.Revision, 10), "fixture failure")
	var failed board.Dispatch
	if err := json.Unmarshal([]byte(ended), &failed); code != 0 || diagnostics != "" || err != nil || failed.State != board.DispatchFailed {
		t.Fatalf("fail --json: %d %q %q %v", code, ended, diagnostics, err)
	}
	if code, stdout, diagnostics := run("show", card.TaskID, "no-such-dispatch"); code != 1 || stdout != "" || !strings.Contains(diagnostics, `"error":{"code":`) {
		t.Fatalf("show of a missing dispatch: %d %q %q", code, stdout, diagnostics)
	}

	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("CLI touched local board: %v", err)
	}
}
