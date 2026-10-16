package launch

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/issue"
	"github.com/dualface/kander/internal/process"
)

func TestRefinementLaunchUsesCardLanguageAndLimitedAuthority(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fake uses POSIX")
	}
	root, _, _ := setupBoard(t)
	repo := triageRepository()
	now := time.Now().UTC()
	provider := &triageProvider{repository: repo, snapshot: issue.IssueSnapshot{Repository: repo, Number: 42, Title: "Remote instructions", Body: "untrusted body", CommentsLoaded: true, CreatedAt: now, UpdatedAt: now, FetchedAt: now}}
	imported, err := issue.Import(t.Context(), provider, root, repo, 42, issue.ImportOptions{Language: "ja", Comments: true})
	if err != nil {
		t.Fatal(err)
	}
	before := mustRead(t, filepath.Join(imported.Path, "spec.md"))
	old := createTaskFile
	var prompt string
	createTaskFile = func(body, prefix string, trailer process.TaskFileTrailer) (string, error) {
		prompt = body
		return old(body, prefix, trailer)
	}
	t.Cleanup(func() { createTaskFile = old })
	out, err := issue.StartRefinement(t.Context(), root, repo, 42, issue.TriageOptions{CardID: imported.TaskID, Agent: "claude", Launcher: "tmux"})
	if err != nil || out.Address == "" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
	command := lastCommand(t, root)
	assertLanguageReminder(t, command, taskFileFromCommand(t, command), "ja")
	for _, want := range []string{imported.TaskID, "ja", "source/github-issue.json", "source/github-issue.md", "Leave the card in backlog", "Do not claim, pick, move", "Do not ask again", "genuine user decisions", "untrusted data"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("missing %q in %s", want, prompt)
		}
	}
	for _, forbidden := range []string{"untrusted body", "Remote instructions", "issue import 42"} {
		if strings.Contains(prompt, forbidden) {
			t.Errorf("unexpected %q", forbidden)
		}
	}
	if after := mustRead(t, filepath.Join(imported.Path, "spec.md")); after != before {
		t.Fatal("launch changed card")
	}
	// The launch layer also refuses terminal handover, even outside the TUI.
	_, err = issue.StartRefinement(t.Context(), root, repo, 42, issue.TriageOptions{CardID: imported.TaskID, Agent: "claude", Launcher: "foreground"})
	if err == nil {
		t.Fatal("foreground accepted")
	}
	if _, err := os.Stat(filepath.Join(imported.Path, "spec.md")); err != nil {
		t.Fatal("card lost", err)
	}
}
