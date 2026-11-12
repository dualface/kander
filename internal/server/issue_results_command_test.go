package server

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/remote"
)

func captureIssueResultCommand(t *testing.T, run func() int) (int, []byte) {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = old }()
	defer writer.Close()
	type readResult struct {
		data []byte
		err  error
	}
	done := make(chan readResult, 1)
	go func() { data, err := io.ReadAll(reader); reader.Close(); done <- readResult{data, err} }()
	code := run()
	writer.Close()
	result := <-done
	if result.err != nil {
		t.Fatal(result.err)
	}
	return code, result.data
}
func TestIssueResultCommandUsesServiceWithoutLocalBoard(t *testing.T) {
	f := newIssueResultFixture(t)
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	t.Setenv(remote.EnvClientHome, f.a.Home)
	forbidden := filepath.Join(cwd, "forbidden-board")
	t.Setenv(board.EnvBoardDir, forbidden)
	cfg := config.DefaultConfig()
	cfg.Server.URL = f.http.URL
	cfg.Project.Name = f.a.Info.Name
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	command := issue.Command(func() issue.IssueProvider { return f.provider })
	base := []string{"result", "42", "--repo", "github.com/test/repo", "--card", f.cardID}
	code, data := captureIssueResultCommand(t, func() int { return command(append(base, "--action", "inspect")) })
	if code != 0 {
		t.Fatalf("remote inspection: %d %s", code, data)
	}
	var inspection issue.ResultInspection
	if err := json.Unmarshal(data, &inspection); err != nil {
		t.Fatal(err)
	}
	proposal := resultFixtureProposal(inspection)
	raw, err := json.Marshal(proposal)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(cwd, "proposal.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	code, data = captureIssueResultCommand(t, func() int { return command(append(base, "--action", "apply", "--file", path)) })
	if code != 0 || f.provider.posts != 1 {
		t.Fatalf("remote apply: %d %s posts=%d", code, data, f.provider.posts)
	}
	for _, path := range []string{forbidden, filepath.Join(cwd, "kanban")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("command used local board: %s %v", path, err)
		}
	}
	// A typo must not silently disappear before a potentially public operation.
	if err := os.WriteFile(path, []byte(`{"unknown":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if code, _ := captureIssueResultCommand(t, func() int { return command(append(base, "--action", "apply", "--file", path)) }); code != 1 {
		t.Fatalf("unknown proposal field code=%d", code)
	}
	if f.provider.posts != 1 {
		t.Fatal("invalid input published another result")
	}
}
