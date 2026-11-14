package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/version"
)

func TestRequiredEntrancesUseInjectedDefaultServer(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	forbidden := filepath.Join(t.TempDir(), "forbidden")
	t.Setenv(board.EnvBoardDir, forbidden)
	var requests atomic.Int64
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		switch r.URL.Path {
		case "/api/v1/version":
			_ = json.NewEncoder(w).Encode(api.VersionInfo{Protocol: api.ProtocolVersion, MinProtocol: api.MinProtocolVersion, ServerVersion: version.String()})
		case "/api/v1/projects":
			_ = json.NewEncoder(w).Encode(api.Project{ID: api.ProjectID("default-service"), Name: "default-service"})
		default:
			w.WriteHeader(404)
			_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeNotFound, Message: "test resource absent"})
		}
	}))
	defer hs.Close()
	old := config.DefaultServerURL
	config.DefaultServerURL = hs.URL
	defer func() { config.DefaultServerURL = old }()
	cfg := config.DefaultConfig()
	cfg.Project.Name = "default-service"
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	task := "20261114-default-service-task"
	for _, args := range [][]string{
		{"check"}, {"notify", task, "--message", "continue"}, {"resume", task, "--message", "continue"}, {"dismiss", task},
		{"subscribe", "--watch", task}, {"review", "progress", t.TempDir(), task}, {"orchestrate", "--message", "continue", task},
		{"coordinator", "show", "20261114-default-group"}, {"dispatch", "show", task, "round"},
	} {
		requests.Store(0)
		result := runKander(t, args...)
		if requests.Load() < 2 {
			t.Fatalf("required entrance %v did not open default service: %d requests; %+v", args, requests.Load(), result)
		}
	}
	if _, err := os.Stat(forbidden); !os.IsNotExist(err) {
		t.Fatalf("required entrance touched storage: %v", err)
	}
}
