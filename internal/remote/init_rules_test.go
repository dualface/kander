package remote

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// initRulesRepository is a Git checkout with an origin and one commit, in a temporary home.
func initRulesRepository(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	repo := filepath.Join(t.TempDir(), "app")
	for _, args := range [][]string{
		{"init", "-q", repo},
		{"-C", repo, "remote", "add", "origin", "https://example.invalid/app.git"},
		{"-C", repo, "-c", "user.email=k@example.invalid", "-c", "user.name=K", "commit", "-q", "--allow-empty", "-m", "init"},
	} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return repo
}

func writeFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func gitHead(t *testing.T, repo string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", repo, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestInitWritesTheProjectReferences(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true}
	url := fake.serve(t)
	newPolicyClient(t, url, func(cfg *config.Config) { cfg.Server.URL, cfg.Project.Name = "", "" })
	repo := initRulesRepository(t)
	t.Chdir(repo)
	writeFile(t, filepath.Join(repo, "CLAUDE.md"), "# Mine\n")
	head := gitHead(t, repo)

	code, out, errOut := runCommand(NewInitCommand, "--server", url, "--project", "golf")
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	for _, file := range []string{"CLAUDE.md", "AGENTS.md"} {
		if !strings.Contains(out, config.Text("projectrules.init_written", filepath.Join(repo, file))) {
			t.Fatalf("%s not listed: %q", file, out)
		}
	}
	if !strings.Contains(errOut, config.Text("projectrules.init_commit_hint")) {
		t.Fatalf("no commit hint: %q", errOut)
	}
	if got := readFile(t, filepath.Join(repo, "CLAUDE.md")); got != "# Mine\n\n@~/.agents/kander/KANDER-AGENTS.md\n" {
		t.Fatalf("CLAUDE.md=%q", got)
	}
	if got := readFile(t, filepath.Join(repo, "AGENTS.md")); !strings.Contains(got, "`~/.agents/kander/KANDER-AGENTS.md`") || !strings.Contains(got, "does not exist, skip this section") {
		t.Fatalf("AGENTS.md=%q", got)
	}
	// init never commits.
	if gitHead(t, repo) != head {
		t.Fatal("init committed")
	}

	code, out, errOut = runCommand(NewInitCommand)
	if code != 0 || strings.Contains(errOut, config.Text("projectrules.init_commit_hint")) || !strings.Contains(out, config.Text("projectrules.init_present", filepath.Join(repo, "AGENTS.md"))) {
		t.Fatalf("second init: code=%d out=%q err=%q", code, out, errOut)
	}
}

func TestInitReportsTheProjectReferencesWithJSON(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true}
	url := fake.serve(t)
	newPolicyClient(t, url, nil)
	repo := initRulesRepository(t)
	t.Chdir(repo)
	code, out, errOut := runCommand(NewInitCommand, "--json")
	if code != 0 {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	var result struct {
		RulesFiles []string `json:"rules_files"`
		Notices    []struct {
			Code string `json:"code"`
		} `json:"notices"`
	}
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("%v: %q", err, out)
	}
	if len(result.RulesFiles) != 2 {
		t.Fatalf("rules_files=%v out=%s", result.RulesFiles, out)
	}
	found := false
	for _, notice := range result.Notices {
		found = found || notice.Code == noticeRulesCommit
	}
	if !found {
		t.Fatalf("notices=%+v", result.Notices)
	}
}

func TestInitOutsideGitWritesOnlyTheBinding(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true}
	url := fake.serve(t)
	newPolicyClient(t, url, func(cfg *config.Config) { cfg.Server.URL, cfg.Project.Name = "", "" })
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runCommand(NewInitCommand, "--server", url, "--project", "golf")
	if code != 0 || !strings.Contains(errOut, config.Text("projectrules.init_not_git", dir)) {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	for _, file := range []string{"CLAUDE.md", "AGENTS.md"} {
		if _, err := os.Lstat(filepath.Join(dir, file)); !os.IsNotExist(err) {
			t.Fatalf("%s written outside Git: %v", file, err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, config.OverlayFilename)); err != nil {
		t.Fatalf("binding not written: %v", err)
	}
}

func TestInitRefusesARulesFileOutsideTheWorktree(t *testing.T) {
	isolatePolicyCommands(t)
	fake := &fakePolicyServer{project: true}
	url := fake.serve(t)
	newPolicyClient(t, url, nil)
	repo := initRulesRepository(t)
	t.Chdir(repo)
	outside := filepath.Join(t.TempDir(), "dotfiles.md")
	writeFile(t, outside, "# dotfiles\n")
	if err := os.Symlink(outside, filepath.Join(repo, "AGENTS.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	code, out, errOut := runCommand(NewInitCommand)
	if code != 0 || !strings.Contains(errOut, filepath.Join(repo, "AGENTS.md")) {
		t.Fatalf("code=%d out=%q err=%q", code, out, errOut)
	}
	if got := readFile(t, outside); got != "# dotfiles\n" {
		t.Fatalf("file outside the worktree changed: %q", got)
	}
	if !strings.Contains(out, config.Text("projectrules.init_written", filepath.Join(repo, "CLAUDE.md"))) {
		t.Fatalf("CLAUDE.md not written: %q", out)
	}
}
