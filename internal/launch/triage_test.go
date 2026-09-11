package launch

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/issue"
)

type triageProvider struct {
	repository issue.Repository
	snapshot   issue.IssueSnapshot
}

func (p *triageProvider) ResolveRepository(context.Context, string, string) (issue.Repository, error) {
	return p.repository, nil
}

func (p *triageProvider) ListIssues(context.Context, issue.Repository, issue.IssueQuery) (issue.IssuePage, error) {
	return issue.IssuePage{}, nil
}

func (p *triageProvider) GetIssue(context.Context, issue.Repository, int, bool) (issue.IssueSnapshot, error) {
	return p.snapshot, nil
}

func triageRepository() issue.Repository {
	return issue.Repository{
		Host: "github.com", Owner: "dualface", Name: "kander",
		URL: "https://github.com/dualface/kander", Remote: "origin",
	}
}

// triageRequest prepares the real evidence layout the command layer writes and
// returns the request StartTriage consumes.
func triageRequest(t *testing.T, root string) issue.TriageLaunch {
	t.Helper()
	repository := triageRepository()
	at := func(hour int) time.Time { return time.Date(2026, 9, 11, hour, 0, 0, 0, time.UTC) }
	snapshot := issue.IssueSnapshot{
		Repository: repository,
		Number:     42,
		Title:      "Crash when importing an issue",
		Body:       "Steps to reproduce:\n\n1. run the import\n",
		State:      "open",
		Author:     "alice",
		Labels:     []string{"type/bug"},
		CreatedAt:  at(1),
		UpdatedAt:  at(2),
		FetchedAt:  at(3),
	}
	snapshot.CommentsLoaded = true
	snapshot.Comments = []issue.IssueComment{{Author: "carol", Body: "Confirmed on Linux.", CreatedAt: at(2)}}
	evidence, err := issue.PrepareTriage(context.Background(), &triageProvider{repository: repository, snapshot: snapshot}, root, repository, 42)
	if err != nil {
		t.Fatal(err)
	}
	return issue.TriageLaunch{
		Root:         root,
		Repository:   repository,
		Number:       42,
		EvidenceDir:  evidence.Directory,
		JSONPath:     evidence.JSONPath,
		MarkdownPath: evidence.MarkdownPath,
	}
}

func TestStartTriageLaunchesBackgroundSessionWithoutBoardWrites(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	request := triageRequest(t, root)
	request.Agent, request.Launcher = "claude", "tmux"

	oldCreate := createTaskFile
	var body, prefix, taskFile string
	createTaskFile = func(text, name string) (string, error) {
		body, prefix = text, name
		path, err := oldCreate(text, name)
		taskFile = path
		return path, err
	}
	t.Cleanup(func() { createTaskFile = oldCreate })

	result, err := StartTriage(request)
	if err != nil {
		t.Fatal(err)
	}
	if result.Agent != "claude" || result.Launcher != "tmux" {
		t.Fatalf("result=%+v", result)
	}
	if result.Address == "" || !strings.Contains(result.Address, ":") {
		t.Fatalf("address=%q", result.Address)
	}
	if taskFile == "" {
		t.Fatal("task file was not created")
	}
	if _, err := os.Stat(taskFile); err != nil {
		t.Fatalf("task file handed to the agent must stay: %v", err)
	}
	if !strings.HasPrefix(prefix, "kander-issue-dualface-kander-42-triage-") {
		t.Fatalf("prefix=%s", prefix)
	}
	if !strings.Contains(body, request.JSONPath) || !strings.Contains(body, request.MarkdownPath) {
		t.Fatalf("prompt is missing the evidence paths:\n%s", body)
	}
	if !strings.Contains(body, "KANDER-ISSUE-RULES.md") {
		t.Fatalf("prompt is missing the issue rules path:\n%s", body)
	}
	if !strings.Contains(body, "kander issue import 42") {
		t.Fatalf("prompt is missing the import instruction:\n%s", body)
	}
	for _, secret := range []string{"Steps to reproduce", "Crash when importing an issue", "Confirmed on Linux", "alice", "carol"} {
		if strings.Contains(body, secret) {
			t.Fatalf("prompt inlined remote text %q:\n%s", secret, body)
		}
	}

	args := mustRead(t, filepath.Join(root, "tmux.log"))
	if !strings.Contains(args, "new-window") || !strings.Contains(args, "issue-dualface-kander-42") {
		t.Fatalf("tmux=%s", args)
	}
	if !strings.Contains(lastCommand(t, root), "claude") {
		t.Fatalf("command=%s", lastCommand(t, root))
	}

	for _, state := range board.States {
		entries, err := os.ReadDir(filepath.Join(root, state))
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("takeover wrote board state %s: %v", state, entries)
		}
	}
}

func TestStartTriageFailureClosesTheContainerAndTaskFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	request := triageRequest(t, root)
	request.Agent, request.Launcher = "claude", "tmux"

	oldCreate := createTaskFile
	var taskFile string
	createTaskFile = func(text, name string) (string, error) {
		path, err := oldCreate(text, name)
		taskFile = path
		return path, err
	}
	t.Cleanup(func() { createTaskFile = oldCreate })
	t.Setenv("KANBAN_TMUX_RESPAWN_FAIL", "1")

	if _, err := StartTriage(request); err == nil {
		t.Fatal("expected the launch to fail")
	}
	if kill, err := os.ReadFile(filepath.Join(root, "tmux.log.kill")); err != nil || !strings.Contains(string(kill), "kill-window") {
		t.Fatalf("created window was not closed: %q %v", kill, err)
	}
	if taskFile == "" {
		t.Fatal("task file was not created")
	}
	if _, err := os.Stat(taskFile); !os.IsNotExist(err) {
		t.Fatalf("failed launch left task file %s", taskFile)
	}
}

func TestStartTriageRequiresEvidence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("tmux fakes are POSIX")
	}
	root, _, _ := setupBoard(t)
	request := triageRequest(t, root)
	request.MarkdownPath = filepath.Join(request.EvidenceDir, "missing.md")
	if _, err := StartTriage(request); err == nil || !strings.Contains(err.Error(), "证据") {
		t.Fatalf("err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "tmux.log")); err == nil {
		t.Fatal("missing evidence must not create a container")
	}
}

func TestTriageWindowNameIsBounded(t *testing.T) {
	repository := triageRepository()
	if name := triageWindowName(repository, 42); name != "issue-dualface-kander-42" {
		t.Fatalf("name=%s", name)
	}
	long := repository
	long.Owner = strings.Repeat("a", 80)
	long.Name = strings.Repeat("b", 80)
	name := triageWindowName(long, 1234567)
	if len([]rune(name)) > 50 {
		t.Fatalf("name too long (%d runes): %s", len([]rune(name)), name)
	}
	if !strings.HasPrefix(name, "issue-") || !strings.HasSuffix(name, "-1234567") {
		t.Fatalf("name lost its identity: %s", name)
	}
}

func TestTriagePromptResolvesTheIssueRulesPathFromScope(t *testing.T) {
	config.ApplyLanguageArgument(nil)
	config.BindConfigLanguage(nil)
	t.Cleanup(func() { config.BindConfigLanguage(nil) })
	t.Setenv(config.EnvLangCLI, "")
	paths := config.InstallPaths{
		Mode:        config.ModeProject,
		ProjectRoot: filepath.Join(t.TempDir(), "project"),
		BinDir:      filepath.Join(t.TempDir(), "bin"),
		RulesDir:    filepath.Join(t.TempDir(), "rules"),
	}
	request := issue.TriageLaunch{
		Root:         paths.ProjectRoot,
		Repository:   triageRepository(),
		Number:       42,
		JSONPath:     filepath.Join(paths.ProjectRoot, "cache", "issue.json"),
		MarkdownPath: filepath.Join(paths.ProjectRoot, "cache", "issue.md"),
	}
	for _, lang := range []string{"cn", "en", "ja"} {
		t.Run(lang, func(t *testing.T) {
			t.Setenv(config.EnvLang, lang)
			body, err := triageAgentPrompt(request, paths)
			if err != nil {
				t.Fatal(err)
			}
			want := filepath.Join(paths.RulesDir, "KANDER-ISSUE-RULES.md")
			if !strings.Contains(body, want) {
				t.Fatalf("prompt is missing the scope-resolved issue rules path %q:\n%s", want, body)
			}
		})
	}
}

func TestPreviewTriageResolvesConfiguredDefaults(t *testing.T) {
	setupBoard(t)
	loadEffective = func() (*config.Config, error) { return envConfig("grok", "tmux", nil), nil }
	preview, err := PreviewTriage("", "")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Agent != "grok" || preview.Launcher != "tmux" {
		t.Fatalf("preview=%+v", preview)
	}
	preview, err = PreviewTriage("claude", "herdr")
	if err != nil {
		t.Fatal(err)
	}
	if preview.Agent != "claude" || preview.Launcher != "herdr" {
		t.Fatalf("override ignored: %+v", preview)
	}
	if _, err := PreviewTriage("nope!", ""); err == nil {
		t.Fatal("unknown agent must fail")
	}
	if _, err := PreviewTriage("claude", "nope"); err == nil {
		t.Fatal("unknown launcher must fail")
	}
}
