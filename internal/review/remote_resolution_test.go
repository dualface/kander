//go:build unix

package review

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

func TestRemoteHistoricalResolutionVerifiesGitAndBindsOriginals(t *testing.T) {
	w := newResolveWorld(t, stayOnHead, onStrayTarget)
	requestPath := w.request(t, w.fixed(w.cover))
	var extension board.ReviewPlanExtension
	if err := readArchiveJSON(requestPath, &extension); err != nil {
		t.Fatal(err)
	}
	snapshot, err := board.ReadSnapshot(w.root, w.id)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(snapshot.Text, "- TASK_BRANCH: task\n", "- TASK_BRANCH: task\n- OWNER: codex\n- SESSION: codex legacy\n- WINDOW: foreground\n- STARTED_AT:\n", 1)
	text += "\n## EXPECTED_OUTCOME\n\nResolve historical review evidence.\n\n## ACCEPTANCE_CRITERIA\n\n- [ ] Historical originals remain intact.\n\n## OUT_OF_SCOPE\n\nNo unrelated changes.\n"
	originalPath := filepath.Join(snapshot.Entry.Path, "reviews", resolveRun, "report.md")
	originalReport, err := os.ReadFile(originalPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(snapshot.Entry.Document, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	dataDir := filepath.Join(t.TempDir(), "server")
	options := server.Options{DataDir: dataDir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	srv, err := server.Open(options)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the real explicit migration boundary. Unresolved historical
	// business work must retain its original evidence without becoming PASS.
	imported, err := srv.Import("historical-review", "", w.root)
	if err != nil {
		t.Fatal(err)
	}
	info := imported.Project
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	srv, err = server.Open(options)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	connection, _ := client.New(httpServer.URL, "fixture-host")
	project := connection.Project(info.ID)
	copied, err := project.Document(t.Context(), w.id, "reviews/"+resolveRun+"/report.md")
	if err != nil || copied.Text != string(originalReport) {
		t.Fatalf("migration changed original report: %v", err)
	}
	sourceReport, err := os.ReadFile(originalPath)
	if err != nil || string(sourceReport) != string(originalReport) {
		t.Fatalf("migration changed local backup: %v", err)
	}
	grant, err := project.Takeover(t.Context(), "historical-takeover", w.id, api.ClaimRequest{ClientID: "fixture-host", ExecutionID: "historical-executor", Owner: "codex", Decision: "explicit recovery of historical storage"})
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	if _, err := project.Execution(t.Context(), "confirm", w.id, api.ExecutionUpdate{Action: api.ExecutionConfirm}, proof); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(t.TempDir(), "client")
	t.Setenv(remote.EnvClientHome, home)
	identity, err := remote.ClientID(home)
	if err != nil {
		t.Fatal(err)
	}
	// Keep this fixture's private identity consistent with the granted holder.
	if err := os.WriteFile(filepath.Join(home, "client-id"), []byte("fixture-host"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = identity
	session := &remote.Session{Client: connection, Project: project, Info: info, ServerURL: httpServer.URL, Home: home}
	if err := session.SaveCredential(remote.Credential{ServerURL: session.ServerURL, ClientID: "fixture-host", ProjectID: info.ID, TaskID: w.id, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}); err != nil {
		t.Fatal(err)
	}
	cfg, err := config.Load(false)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Server.URL, cfg.Project.Name = httpServer.URL, info.Name
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	forbidden := filepath.Join(t.TempDir(), "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	t.Chdir(w.h.repo)
	raw, err := project.ReviewAction(t.Context(), "resolution-facts", "resolution-facts", extension, nil)
	if err != nil {
		t.Fatal(err)
	}
	var facts board.ReviewResolutionFacts
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	tampered := api.ReviewResolution{Extension: extension, Facts: facts, Git: board.ReviewResolutionGit{CWD: facts.CWD, Edges: facts.Edges}}
	tampered.Facts.BatchRevision++
	if _, err := project.ReviewAction(t.Context(), "tampered-facts", "resolve-batch", tampered, &proof); !api.IsCode(err, api.CodeEvidenceIncomplete) {
		t.Fatalf("changed originals were accepted: %v", err)
	}
	commandOK(t, "extend-plan", w.h.repo, requestPath)
	raw, err = project.ReviewResource(t.Context(), "plans", extension.PlanID)
	if err != nil {
		t.Fatal(err)
	}
	var plan board.ReviewPlan
	if err := json.Unmarshal(raw, &plan); err != nil {
		t.Fatal(err)
	}
	if len(plan.ResolvedBatches) != 1 || plan.ResolvedBatches[0].Git.CWD != w.h.repoReal || plan.ResolvedBatches[0].Runs[0].RunID != resolveRun {
		t.Fatalf("resolution lost history: %+v", plan)
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("resolution used a local board: %v", err)
	}
}
