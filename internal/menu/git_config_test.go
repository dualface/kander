//go:build unix

package menu

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/config"
)

func TestConfigPrintsGitBranchMode(t *testing.T) {
	h := newHarness(t)
	h.writeConfig(defaultPayload(map[string]any{"git": map[string]any{"branch_mode": "main-only"}}))
	code, out, err := h.run("config")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	if !strings.Contains(out, ": main-only") {
		t.Fatalf("human output misses git.branch_mode:\n%s", out)
	}
	code, out, err = h.run("config", "--json")
	if code != 0 {
		t.Fatalf("%d %s", code, err)
	}
	var cfg struct {
		Git struct {
			BranchMode string `json:"branch_mode"`
		} `json:"git"`
	}
	if e := json.Unmarshal([]byte(out), &cfg); e != nil {
		t.Fatal(e)
	}
	if cfg.Git.BranchMode != "main-only" {
		t.Fatalf("json git.branch_mode = %q", cfg.Git.BranchMode)
	}
}

func TestSetGitBranchModeRejectsUnknownValue(t *testing.T) {
	s := &Session{Config: config.DefaultConfig()}
	if err := s.SetGitBranchMode("trunk"); err == nil {
		t.Fatal("accepted an unknown branch mode")
	}
	if s.Config.Git.BranchMode != config.GitBranchModeMainDevelop || s.ScopeDirty {
		t.Fatalf("rejected value changed the session: %q dirty=%v", s.Config.Git.BranchMode, s.ScopeDirty)
	}
}
