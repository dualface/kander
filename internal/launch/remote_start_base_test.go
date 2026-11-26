package launch

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

const startBaseGroup = "20261127-start-base-group"

// startBaseFixture is a project whose registered remote holds main, develop,
// v2 and a group branch, each at its own commit, and whose caller checkout is
// on v2. It returns the remote's branch commits.
type startBaseFixture struct {
	cfg     *config.Config
	session *remote.Session
	bare    string
	cwd     string
	commits map[string]string
}

func newStartBaseFixture(t *testing.T) startBaseFixture {
	t.Helper()
	source, root := integrationGit(t)
	commits := map[string]string{}
	for _, branch := range []string{"main", "develop", "v2", "group/" + startBaseGroup} {
		commits[branch] = startBaseCommit(t, source, root, branch)
		lifecycleGit(t, source, "update-ref", "refs/heads/"+branch, commits[branch])
	}
	bare := filepath.Join(t.TempDir(), "common.git")
	lifecycleGit(t, source, "clone", "--bare", source, bare)
	cfg, session, _, _, _ := remoteLifecycleFixture(t, "start-base-unused", bare)
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return startBaseFixture{cfg: cfg, session: session, bare: bare, cwd: cwd, commits: commits}
}

// startBaseCommit creates a distinct child commit of parent in repo without
// touching any checkout.
func startBaseCommit(t *testing.T, repo, parent, message string) string {
	t.Helper()
	return lifecycleGit(t, repo, "commit-tree", "-p", parent, "-m", message, parent+"^{tree}")
}

// todo creates a todo card whose header carries the given lines. A line
// replaces the template's line with the same field, or is added after the
// header otherwise.
func (f startBaseFixture) todo(t *testing.T, slug string, header ...string) api.Task {
	t.Helper()
	ctx := t.Context()
	card, err := f.session.Project.NewTask(ctx, "", api.NewTask{Type: "feature", Slug: slug, Title: "Verify the start base", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(card.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: remote-fixture-branch", 1)
	for _, line := range header {
		field, _, _ := strings.Cut(line, ":")
		current := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(field) + `:.*$`)
		if current.MatchString(text) {
			text = current.ReplaceAllLiteralString(text, line)
		} else {
			text = strings.Replace(text, "\n- RESULT:", "\n"+line+"\n- RESULT:", 1)
		}
	}
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	card, err = f.session.Project.PutDocument(ctx, "", card.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: card.Revision}, nil)
	if err != nil {
		t.Fatal(err)
	}
	card, err = f.session.Project.Move(ctx, "", card.TaskID, api.Move{State: "todo"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return card
}

func (f startBaseFixture) worktree(t *testing.T, task string) string {
	t.Helper()
	root, err := f.session.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(root, "worktrees", task, "g1")
}

// start launches the card and asserts that the execution worktree, the
// credential and the published baseline all sit on want.
func (f startBaseFixture) start(t *testing.T, task, want string) {
	t.Helper()
	worktree := f.worktree(t, task)
	t.Setenv("KANBAN_LAUNCH_CWD", worktree)
	if _, err := StartRemote("claude", "tmux", task); err != nil {
		t.Fatal(err)
	}
	if head := lifecycleGit(t, worktree, "rev-parse", "HEAD"); head != want {
		t.Fatalf("worktree HEAD %s, want %s", head, want)
	}
	credential, found, err := f.session.Credential(task)
	if err != nil || !found || credential.BaseCommit != want {
		t.Fatalf("credential base %q (found=%t, %v), want %s", credential.BaseCommit, found, err, want)
	}
	view, err := f.session.Project.Show(t.Context(), task)
	if err != nil {
		t.Fatal(err)
	}
	if view.Delivery == nil || view.Delivery.Purpose != "baseline" || view.Delivery.Commit != want {
		t.Fatalf("baseline delivery %+v, want commit %s", view.Delivery, want)
	}
}

// refused asserts that a start failed before the claim: the card stays in
// todo with no claim, no evidence ref, worktree or task branch.
func (f startBaseFixture) refused(t *testing.T, task string, err error, text string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), text) {
		t.Fatalf("start error %v, want one naming %q", err, text)
	}
	view, showErr := f.session.Project.Show(t.Context(), task)
	if showErr != nil {
		t.Fatal(showErr)
	}
	if view.State != "todo" || view.Claim != nil {
		t.Fatalf("refused start left state %s claim %+v", view.State, view.Claim)
	}
	if refs := lifecycleGit(t, f.bare, "for-each-ref", "refs/heads/kander/"+f.session.Info.ID+"/"+task+"/"); refs != "" {
		t.Fatalf("refused start published evidence refs:\n%s", refs)
	}
	if _, statErr := os.Stat(f.worktree(t, task)); !os.IsNotExist(statErr) {
		t.Fatalf("refused start created a worktree: %v", statErr)
	}
	branch := "refs/heads/" + api.ExecutionBranch(f.session.Info.ID, task, 1)
	if exec.Command("git", "-C", f.cwd, "rev-parse", "--verify", "--quiet", branch).Run() == nil {
		t.Fatalf("refused start created the task branch %s", branch)
	}
}

func TestRemoteStartBasesTheWorktreeOnTheIntegrationTarget(t *testing.T) {
	skipOnWindows(t)
	f := newStartBaseFixture(t)
	if head := lifecycleGit(t, f.cwd, "rev-parse", "HEAD"); head != f.commits["v2"] {
		t.Fatalf("caller checkout at %s, want v2 %s", head, f.commits["v2"])
	}
	card := f.todo(t, "start-base-target", "- INTEGRATION_TARGET: develop")
	f.start(t, card.TaskID, f.commits["develop"])
}

func TestRemoteStartBasesACardWithoutTargetOnTheMainlineBranch(t *testing.T) {
	skipOnWindows(t)
	for _, tc := range []struct{ mode, branch string }{
		{config.GitBranchModeMainDevelop, "develop"},
		{config.GitBranchModeMainOnly, "main"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			f := newStartBaseFixture(t)
			f.cfg.Git.BranchMode = tc.mode
			card := f.todo(t, "start-base-mainline")
			f.start(t, card.TaskID, f.commits[tc.branch])
		})
	}
}

func TestRemoteStartBasesAGroupMemberOnTheGroupBranch(t *testing.T) {
	skipOnWindows(t)
	f := newStartBaseFixture(t)
	card := f.todo(t, "start-base-member", "- TASK_GROUP: "+startBaseGroup, "- INTEGRATION_TARGET: v2")
	if got := taskGroupFrom(card.Text); got != startBaseGroup {
		t.Fatalf("fixture card group %q, want %s", got, startBaseGroup)
	}
	f.start(t, card.TaskID, f.commits["group/"+startBaseGroup])
}

func TestRemoteStartRefusesAMissingStartBranchBeforeTheClaim(t *testing.T) {
	skipOnWindows(t)
	f := newStartBaseFixture(t)
	card := f.todo(t, "start-base-missing", "- INTEGRATION_TARGET: release")
	// ls-remote matches this ref against the pattern by its tail; it is not the branch.
	lifecycleGit(t, f.bare, "update-ref", "refs/heads/shadow/refs/heads/release", f.commits["v2"])
	t.Setenv("KANBAN_LAUNCH_CWD", f.worktree(t, card.TaskID))
	_, err := StartRemote("claude", "tmux", card.TaskID)
	f.refused(t, card.TaskID, err, "release")

	// Once the branch exists on the registered remote, the same card starts.
	release := startBaseCommit(t, f.bare, f.commits["develop"], "release")
	lifecycleGit(t, f.bare, "update-ref", "refs/heads/release", release)
	f.start(t, card.TaskID, release)
}

func TestRemoteStartRefusesMainInMainDevelopModeBeforeTheClaim(t *testing.T) {
	skipOnWindows(t)
	f := newStartBaseFixture(t)
	card := f.todo(t, "start-base-main", "- INTEGRATION_TARGET: main")
	t.Setenv("KANBAN_LAUNCH_CWD", f.worktree(t, card.TaskID))
	_, err := StartRemote("claude", "tmux", card.TaskID)
	f.refused(t, card.TaskID, err, "main-develop")
}

func TestRemoteStartNeverPublishesTheCallersUnpushedCommit(t *testing.T) {
	skipOnWindows(t)
	f := newStartBaseFixture(t)
	lifecycleGit(t, f.cwd, "commit", "--allow-empty", "-m", "local only")
	local := lifecycleGit(t, f.cwd, "rev-parse", "HEAD")
	card := f.todo(t, "start-base-local")
	f.start(t, card.TaskID, f.commits["develop"])
	refs := lifecycleGit(t, f.bare, "for-each-ref", "--format=%(objectname) %(refname)", "refs/heads/kander/"+f.session.Info.ID+"/"+card.TaskID+"/")
	if refs == "" || strings.Contains(refs, local) {
		t.Fatalf("evidence refs %q, want the baseline only and never %s", refs, local)
	}
	if exec.Command("git", "-C", f.bare, "cat-file", "-e", local+"^{commit}").Run() == nil {
		t.Fatalf("the caller's unpushed commit %s reached the registered remote", local)
	}
}
