package launch

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
)

// A start base without the project reference of the executing agent is refused before the
// claim; once the reference is committed on the start branch, the same card starts.
func TestRemoteStartRefusesABaseWithoutTheProjectReference(t *testing.T) {
	skipOnWindows(t)
	f := newStartBaseFixture(t)
	card := f.todo(t, "start-rules-reference", "- INTEGRATION_TARGET: develop")
	// develop loses CLAUDE.md, the file Claude reads; AGENTS.md alone does not count.
	stripped := lifecycleGit(t, f.bare, "commit-tree", "-p", f.commits["develop"], "-m", "drop the project reference",
		emptyTreeWithout(t, f.bare, f.commits["develop"], "CLAUDE.md"))
	lifecycleGit(t, f.bare, "update-ref", "refs/heads/develop", stripped)
	t.Setenv("KANBAN_LAUNCH_CWD", f.worktree(t, card.TaskID))
	_, err := StartRemote("claude", "tmux", card.TaskID)
	f.refused(t, card.TaskID, err, "CLAUDE.md")
	var failure *command.Failure
	if !errors.As(err, &failure) || failure.Code != CodeRulesReferenceMissing || !strings.Contains(err.Error(), "develop") || !strings.Contains(err.Error(), "kander init") {
		t.Fatalf("refusal=%v", err)
	}
	lifecycleGit(t, f.bare, "update-ref", "refs/heads/develop", f.commits["develop"])
	f.start(t, card.TaskID, f.commits["develop"])
}

// An agent the reference check does not apply to starts with a notice.
func TestRemoteStartSkipsTheReferenceCheckWhenTheSwitchIsOff(t *testing.T) {
	skipOnWindows(t)
	f := newStartBaseFixture(t)
	card := f.todo(t, "start-rules-skip", "- INTEGRATION_TARGET: develop")
	stripped := lifecycleGit(t, f.bare, "commit-tree", "-p", f.commits["develop"], "-m", "drop the project reference",
		emptyTreeWithout(t, f.bare, f.commits["develop"], "CLAUDE.md"))
	lifecycleGit(t, f.bare, "update-ref", "refs/heads/develop", stripped)
	off := false
	if _, err := config.Update(func(cfg *config.Config) error {
		config.SetAgentInstallRules(cfg, "claude", &off)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	_, _, stderr := commandtest.Capture(func() int {
		f.start(t, card.TaskID, stripped)
		return 0
	})
	if !strings.Contains(stderr, config.Text("projectrules.start_check_skipped", "claude", config.Text("projectrules.start_skip_switch_off"))) {
		t.Fatalf("no skip notice: %q", stderr)
	}
}

// lifecycleGitInput runs Git in dir with input on standard input.
func lifecycleGitInput(t *testing.T, dir, input string, args ...string) string {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...)
	cmd.Stdin = strings.NewReader(input)
	data, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Git %v: %s: %v", args, data, err)
	}
	return strings.TrimSpace(string(data))
}

// emptyTreeWithout returns the tree of commit without the top-level entry name.
func emptyTreeWithout(t *testing.T, repo, commit, name string) string {
	t.Helper()
	listing := lifecycleGit(t, repo, "ls-tree", commit)
	var kept []string
	for _, line := range strings.Split(listing, "\n") {
		if line != "" && !strings.HasSuffix(line, "\t"+name) {
			kept = append(kept, line)
		}
	}
	return lifecycleGitInput(t, repo, strings.Join(kept, "\n")+"\n", "mktree")
}
