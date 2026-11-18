package main

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/cli"
	"github.com/dualface/kander/internal/config"
)

// The production package graph must register check: the no-mode form goes
// through the service and never touches a local board.

func TestCheckCommandUsesLivenessInFullBinary(t *testing.T) {
	t.Setenv(config.EnvLang, "cn")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "cn"})

	if cli.Commands["check"] == nil {
		t.Fatal("check is not registered")
	}

	t.Chdir(t.TempDir())
	root := filepath.Join(t.TempDir(), "forbidden-board")
	t.Setenv(board.EnvBoardDir, root)
	configDir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(config.EnvConfig, filepath.Join(configDir, "config.json"))
	cfg := config.DefaultConfig()
	cfg.WelcomeComplete = true
	cfg.Language = "en"
	cfg.AgentLanguage = "en"
	service := commandService(t, cfg)
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	id := readyCard(t, "full-binary-check")
	grant, err := service.Project.Claim(t.Context(), "", id, api.ClaimRequest{ClientID: service.Client.ClientID(), ExecutionID: "check-observation", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	out := runKander(t, "check", id).require(t, "service check")
	if !strings.Contains(out.stdout, id) || !strings.Contains(out.stdout, "status=unknown") {
		t.Fatalf("service liveness output missing: %+v", out)
	}
	after, err := service.Project.Show(t.Context(), id)
	if err != nil || after.Revision != grant.Task.Revision {
		t.Fatal("check changed the task")
	}
	if _, err := os.Lstat(root); !os.IsNotExist(err) {
		t.Fatalf("check used local board: %v", err)
	}
}

func TestCheckCommandDeliveryBoardFree(t *testing.T) {
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	config.ApplyLanguageArgument([]string{"kander", "--lang", "en"})
	t.Setenv(board.EnvBoardDir, filepath.Join(t.TempDir(), "missing-board"))
	t.Chdir(t.TempDir())

	got := cli.Commands["check"]
	if got == nil {
		t.Fatal("check is not registered")
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = stdoutW, stderrW
	code := got([]string{"delivery", "--base", "HEAD", "--json"})
	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout, os.Stderr = oldOut, oldErr
	outBytes, _ := io.ReadAll(stdoutR)
	errBytes, _ := io.ReadAll(stderrR)
	_ = stdoutR.Close()
	_ = stderrR.Close()
	if string(errBytes) != "" {
		t.Fatalf("json stderr=%q", errBytes)
	}
	if code != 3 {
		t.Fatalf("exit=%d stdout=%s", code, outBytes)
	}
	if !strings.Contains(string(outBytes), `"check":"delivery"`) || !strings.Contains(string(outBytes), `"status":"error"`) {
		t.Fatalf("expected delivery JSON error, got %s", outBytes)
	}
	if !strings.Contains(string(outBytes), `"code":"not-repository"`) {
		t.Fatalf("expected not-repository error, got %s", outBytes)
	}
}
