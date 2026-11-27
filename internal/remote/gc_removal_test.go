package remote

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// gcRemoveLeavesRegistration is a Git wrapper whose `worktree remove` deletes
// only the directory and fails, and whose `worktree prune` fails, so Git keeps
// the registration of a worktree whose directory is gone.
const gcRemoveLeavesRegistration = `case "$*" in
*"worktree remove "*) for last; do :; done; rm -rf "$last"; echo "` + gcRemoveFailure + `" >&2; exit 1;;
*"worktree prune"*) echo "fatal: simulated prune failure" >&2; exit 1;;
esac
exec "$real" "$@"
`

// A Git error of the removal makes `kander gc` exit 1 even when the worktree
// went.
func TestGCCommandExitsOneAfterAFailedWorktreeRemoval(t *testing.T) {
	f, w := removable(t)
	wrapGit(t, `"$real" "$@"
code=$?
case "$*" in *"worktree remove"*) echo "`+gcRemoveFailure+`" >&2; exit 1;; esac
exit $code
`)
	c := &gcCommand{f: f, skipped: "remote"}
	code, out, errOut := c.run(t, "--yes")
	if code != 1 || f.registered(w.Path) || !strings.Contains(out, gcRemoveFailure) {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
}

// While Git still registers a worktree whose directory is gone, the branch, the
// intent and the removal record stay, in the run that removed the directory
// and in the run that resumes the record; a dry run then changes nothing in
// Git's registry, and the first run whose prune clears the registration
// finishes the removal.
func TestGCKeepsTheRemovalWhileGitStillRegistersTheWorktree(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	intent := filepath.Join(f.root, "worktree-intents", gcTask, "g1.json")
	record := filepath.Join(f.root, "gc-removals", gcTask, "g1.json")
	stays := func(step string, result GCProject) {
		t.Helper()
		if exists(t, w.Path) || !f.registered(w.Path) || !f.branchExists(w.Branch) || !exists(t, intent) || !exists(t, record) || !result.Incomplete() {
			t.Fatalf("%s: the branch, the intent and the record must stay: %+v", step, result)
		}
		if reason := keptReason(result.Worktrees, gcTask+"/g1"); !strings.HasPrefix(reason, config.Text("gc.client_still_registered", w.Path)) {
			t.Fatalf("%s: the kept reason names the registration: %q", step, reason)
		}
	}
	path := os.Getenv("PATH")
	wrapGit(t, gcRemoveLeavesRegistration)

	result := f.run(false)
	stays("removal", result)
	if !strings.Contains(strings.Join(result.Errors, "\n"), gcRemoveFailure) || result.Worktrees.Count != 0 {
		t.Fatalf("the Git error is reported and nothing counts as removed: %+v", result)
	}
	stays("resumed removal", f.run(false))

	t.Setenv("PATH", path)
	registry := filepath.Join(f.repo, ".git", "worktrees")
	before := snapshot(t, registry)
	plan := f.run(true)
	if !sameSnapshot(before, snapshot(t, registry)) || !f.registered(w.Path) || !f.branchExists(w.Branch) || !exists(t, record) || plan.Incomplete() || plan.Branches.Count != 1 {
		t.Fatalf("a dry run prunes nothing and plans the branch: %+v", plan)
	}

	prunes := filepath.Join(t.TempDir(), "prunes")
	wrapGit(t, `case "$*" in *"worktree prune"*) echo prune >> '`+prunes+`';; esac
exec "$real" "$@"
`)
	result = f.run(false)
	if result.Incomplete() || f.registered(w.Path) || f.branchExists(w.Branch) || exists(t, intent) || exists(t, record) {
		t.Fatalf("the run that clears the registration finishes the removal: %+v", result)
	}
	if log, err := os.ReadFile(prunes); err != nil || string(log) != "prune\n" {
		t.Fatalf("the registry is pruned once: %q %v", log, err)
	}
}
