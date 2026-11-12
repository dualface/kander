package takeover

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

func TestRemoteDismissUsesRetainedLocalRoutingAfterSecretRemoval(t *testing.T) {
	root, _ := setupBoard(t)
	task, path := makeDone(t, root, "remote-dismiss", "herdr:w1:t8:w1:p8")
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	record, _, err := board.CurrentExecution(root, task)
	if err != nil {
		t.Fatal(err)
	}
	cwd := t.TempDir()
	t.Chdir(cwd)
	forbidden := filepath.Join(cwd, "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	home := filepath.Join(t.TempDir(), "client")
	t.Setenv(remote.EnvClientHome, home)
	identity, err := remote.ClientID(home)
	if err != nil {
		t.Fatal(err)
	}
	project := api.Project{ID: "dismiss-project", Name: "dismiss-project"}
	card := api.Task{TaskID: task, Text: string(text), State: "done", Revision: 10, Claim: &api.ClaimView{ClientID: identity, ExecutionID: "finished-executor", Generation: 1, Status: api.ClaimReleased}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(api.VersionInfo{Protocol: api.ProtocolVersion, MinProtocol: api.MinProtocolVersion})
	})
	mux.HandleFunc("POST /api/v1/projects", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(project) })
	mux.HandleFunc("GET /api/v1/projects/{project}/tasks/{task}", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(card) })
	mux.HandleFunc("GET /api/v1/projects/{project}/tasks/{task}/execution/record", func(w http.ResponseWriter, r *http.Request) { _ = json.NewEncoder(w).Encode(record) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Server.URL, cfg.Project.Name = srv.URL, project.Name
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	service, err := remote.Open(t.Context(), cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{ServerURL: srv.URL, ClientID: identity, ProjectID: project.ID, TaskID: task, ExecutionID: card.Claim.ExecutionID, Generation: 1, Token: "removed-secret", Session: record.Session, Window: board.MetadataFrom(card.Text, board.FieldWindow)}
	if err := service.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	if err := remote.RemoveCredential(home, credential); err != nil {
		t.Fatal(err)
	}
	if _, found, err := service.Credential(task); err != nil || found {
		t.Fatalf("released secret remained: %v", err)
	}
	if code := RunDismiss([]string{task}); code != 0 {
		t.Fatalf("dismiss exit %d", code)
	}
	if _, err := os.Stat(filepath.Join(root, "herdr.log.close")); err != nil {
		t.Fatalf("owned container was not closed: %v", err)
	}
	if code := RunDismiss([]string{task}); code != 0 {
		t.Fatalf("completed receipt replay exit %d", code)
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("remote dismiss read local board: %v", err)
	}
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "foreign"))
	if code := RunDismiss([]string{task}); code == 0 {
		t.Fatal("foreign client dismissed an execution")
	}
}
