//go:build unix

package review

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
	"github.com/dualface/kander/v2/internal/version"
)

// TestRemoteReviewRetriesAfterClientUpgrade interrupts the publication of a
// finished run, replaces the client binary by one of another version, and runs
// the same command again.
func TestRemoteReviewRetriesAfterClientUpgrade(t *testing.T) {
	t.Setenv(remote.EnvExecutionID, "")
	useServer(t)
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
	var failPublish atomic.Bool
	var mu sync.Mutex
	var prepares [][]byte
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/prepare") {
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Error(err)
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			mu.Lock()
			prepares = append(prepares, body)
			mu.Unlock()
		}
		if failPublish.Load() && strings.HasSuffix(r.URL.Path, "/publish") {
			w.Header().Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"code":"not_ready","message":"injected publication outage"}`))
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
	cfg["project"] = map[string]string{"name": "review-upgrade"}
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
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: "upgrade-review", Title: "Review across an upgrade", Language: "en"})
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
	report := "Upgrade report\n" + emptyStructuredReview
	t.Setenv("FAKE_CODEX_REPORT", report)
	args := []string{"codex", "--task", task.TaskID, "--run-id", "upgrade-run", "--batch-id", "upgrade-batch", "--requirements-file", requirements, h.repo, h.base, h.head, "PMQA", "Frozen upgrade task"}

	t.Cleanup(func(old string) func() { return func() { version.Version = old } }(version.Version))
	version.Version = "v-a"
	failPublish.Store(true)
	code, out, stderr := captureRun(t, args)
	if code == 0 {
		t.Fatalf("an interrupted publication succeeded: %q %s", out, stderr)
	}
	run := serviceRun(t, session, "upgrade-run")
	if run.Phase != "finalized" || run.ExecutionStatus != "ok" || run.Published[task.TaskID] {
		t.Fatalf("the interruption must leave a finished, unpublished run: %+v", run)
	}
	launched := readFile(t, h.argvLog)

	// The binary is replaced: it has another version and no reviewer to launch.
	version.Version = "v-b"
	failPublish.Store(false)
	if err := os.Remove(h.fake); err != nil {
		t.Fatal(err)
	}
	code, out, stderr = captureRun(t, args)
	if code != 0 || out != report+"\n" {
		t.Fatalf("retry after the upgrade: %d %q %s", code, out, stderr)
	}
	if readFile(t, h.argvLog) != launched {
		t.Fatal("the retry launched the reviewer again")
	}
	run = serviceRun(t, session, "upgrade-run")
	if !run.Published[task.TaskID] || run.KanderVersion != "v-a" {
		t.Fatalf("retry did not publish the run it found: %+v", run)
	}
	published, err := session.Project.Show(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	indexes, err := board.ParseReviewIndexes(published.Text)
	if err != nil || len(indexes) != 1 || indexes[0].RunID != "upgrade-run" {
		t.Fatalf("card index: %+v %v", indexes, err)
	}

	// A prepare for a run the service does not know carries the client version.
	mu.Lock()
	defer mu.Unlock()
	var first api.ReviewPrepare
	if len(prepares) == 0 || json.Unmarshal(prepares[0], &first) != nil || first.Version != "v-a" {
		t.Fatalf("the first prepare did not carry the client version: %s", prepares)
	}
}

func serviceRun(t *testing.T, session *remote.Session, id string) board.ReviewRun {
	t.Helper()
	raw, err := session.Project.ReviewResource(t.Context(), "runs", id)
	if err != nil {
		t.Fatal(err)
	}
	var run board.ReviewRun
	if err := json.Unmarshal(raw, &run); err != nil {
		t.Fatal(err)
	}
	return run
}
