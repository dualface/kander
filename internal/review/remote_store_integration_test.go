//go:build unix

package review

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func TestRemoteArchiveCLIUsesServiceWithoutLocalBoard(t *testing.T) {
	t.Run("normal", func(t *testing.T) { remoteArchiveCLI(t, false) })
	t.Run("pending upload after outage", func(t *testing.T) { remoteArchiveCLI(t, true) })
}
func remoteArchiveCLI(t *testing.T, interrupted bool) {
	h := newCodexHarness(t)
	t.Chdir(h.repo)
	t.Setenv(board.EnvBoardDir, filepath.Join(h.root, "forbidden-board"))
	t.Setenv(remote.EnvClientHome, filepath.Join(h.root, "client"))
	srv, err := server.Open(server.Options{DataDir: filepath.Join(h.root, "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	handler := srv.Handler()
	var failFinalize atomic.Bool
	failFinalize.Store(interrupted)
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failFinalize.Load() && strings.HasSuffix(r.URL.Path, "/finalize") {
			w.Header().Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"not_ready","message":"injected upload outage"}`))
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer httpServer.Close()
	var cfg map[string]any
	configPath := os.Getenv(config.EnvConfig)
	if err := json.Unmarshal([]byte(readFile(t, configPath)), &cfg); err != nil {
		t.Fatal(err)
	}
	cfg["server"] = map[string]string{"url": httpServer.URL}
	cfg["project"] = map[string]string{"name": "review-demo"}
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(configPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	effective, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	session, err := remote.Open(t.Context(), effective, h.repo)
	if err != nil {
		t.Fatal(err)
	}
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "remote-review", Title: "Remote review", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if _, err := session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := session.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	grant, err := session.Project.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: session.Client.ClientID(), ExecutionID: "review-execution", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{ServerURL: session.ServerURL, ClientID: session.Client.ClientID(), ProjectID: session.Info.ID, TaskID: task.TaskID, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token, RenewedAt: grant.Claim.RenewedAt, TTL: grant.TTL, RenewInterval: grant.RenewInterval}
	if err := session.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	requirements := filepath.Join(h.root, "requirements.json")
	if err := os.WriteFile(requirements, []byte(`{"PMQA":"required","Security":"N/A: project"}`), 0600); err != nil {
		t.Fatal(err)
	}
	report := "Remote report\n" + emptyStructuredReview
	t.Setenv("FAKE_CODEX_REPORT", report)
	args := []string{"codex", "--task", task.TaskID, "--run-id", "remote-run", "--batch-id", "remote-batch", "--requirements-file", requirements, h.repo, h.base, h.head, "PMQA", "Frozen remote task"}
	code, out, stderr := captureRun(t, args)
	if interrupted {
		if code != 2 || out != report+"\n" {
			t.Fatalf("outage did not preserve the completed output: %d %q %s", code, out, stderr)
		}
		if err := os.Remove(h.fake); err != nil {
			t.Fatal(err)
		}
		failFinalize.Store(false)
		var recovered string
		code, recovered, stderr = captureRun(t, args)
		if recovered != out {
			t.Fatalf("recovery changed saved output: %q %q", recovered, out)
		}
	}

	if code != 0 || out != report+"\n" {
		t.Fatalf("remote execution: %d %q %s", code, out, stderr)
	}
	raw, err := session.Project.ReviewResource(t.Context(), "runs", "remote-run")
	if err != nil {
		t.Fatal(err)
	}
	var run board.ReviewRun
	if err := json.Unmarshal(raw, &run); err != nil {
		t.Fatal(err)
	}
	if run.Phase != "finalized" || run.ExecutionStatus != "ok" || !run.Published[task.TaskID] || run.CWD != "project:"+session.Info.ID {
		t.Fatalf("invalid service evidence: %+v", run)
	}
	original, err := session.Project.ReviewEvidence(t.Context(), run.RunID, "original", "report.md")
	if err != nil {
		t.Fatal(err)
	}
	var reportBytes []byte
	if err := json.Unmarshal(original, &reportBytes); err != nil || string(reportBytes) != out {
		t.Fatalf("original: %q %v", reportBytes, err)
	}
	published, err := session.Project.Show(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(h.fake); err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	// A manual retry gets the durable run. It cannot launch another reviewer.
	code, replayed, stderr := captureRun(t, args)
	if code != 0 || replayed != out {
		t.Fatalf("remote replay: %d %q %s", code, replayed, stderr)
	}
	after, err := session.Project.Show(t.Context(), task.TaskID)
	if err != nil || after.Revision != published.Revision {
		t.Fatal("retry republished the card")
	}
	for _, path := range []string{os.Getenv(board.EnvBoardDir), filepath.Join(h.repo, "kanban")} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("created a local board: %s %v", path, err)
		}
	}
}
