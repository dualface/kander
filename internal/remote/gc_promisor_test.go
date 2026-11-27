package remote

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/config"
)

// A promisor remote counts in every spelling Git reads as true, and a value
// Git would not accept counts as a risk; only the spellings of false do not.
func TestPartialCloneReadsPromisorAsAGitBoolean(t *testing.T) {
	for _, tc := range []struct {
		line string
		want bool
	}{
		{"remote.origin.promisor=true", true},
		{"remote.origin.promisor=TRUE", true},
		{"remote.origin.promisor=yes", true},
		{"remote.origin.promisor=on", true},
		{"remote.origin.promisor=1", true},
		{"remote.origin.promisor=2", true},
		{"remote.origin.promisor", true},
		{"remote.origin.promisor=false", false},
		{"remote.origin.promisor=no", false},
		{"remote.origin.promisor=off", false},
		{"remote.origin.promisor=0", false},
		{"remote.origin.promisor=OFF", false},
		{"remote.origin.promisor=", false},
		{"remote.origin.promisor=-0", false},
		{"remote.origin.promisor=0x0", false},
		{"remote.origin.promisor=0k", false},
		{"remote.origin.promisor=0x", true},
		{"remote.origin.promisor=false ", true},
		{"remote.origin.promisor= 0", false},
		{"remote.origin.promisor=0 ", true},
		{"remote.origin.promisor=maybe", true},
		{"extensions.partialclone=origin", true},
		{"extensions.partialclone=", false},
		{"extensions.partialclone= ", false},
		{"remote.origin.url=/tmp/origin.git", false},
	} {
		key, value, set := strings.Cut(tc.line, "=")
		entry := key
		if set {
			entry += "\n" + value
		}
		if got := partialClone("core.bare\nfalse\x00" + entry + "\x00"); got != tc.want {
			t.Errorf("%q: got %v, want %v", tc.line, got, tc.want)
		}
	}
}

// On a Git that cannot stop lazy fetching, a partial clone whose promisor is
// enabled as "yes" and that has no extensions.partialclone is still deferred
// before any probe, and the object store does not change.
func TestGCDryRunDefersAPromisorEnabledAsYes(t *testing.T) {
	withoutLazyFetchControl(t)
	f := newGCFixture(t)
	partial := f.usePartialClone()
	f.card(gcTask, "done")
	f.worktree(gcTask, 1)
	// Recent Git no longer writes extensions.partialclone for a blobless
	// clone; exit status 5 means the key was not there.
	if err := exec.Command("git", "-C", partial, "config", "--unset-all", "extensions.partialclone").Run(); err != nil {
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 5 {
			t.Fatal(err)
		}
	}
	gcGit(t, partial, "config", "remote.origin.promisor", "yes")
	if listed := gcGit(t, partial, "config", "--list"); strings.Contains(strings.ToLower(listed), "extensions.partialclone") || !strings.Contains(listed, "remote.origin.promisor=yes") {
		t.Fatalf("the fixture must name the promisor only as yes:\n%s", listed)
	}
	before := gcGit(t, partial, "count-objects", "-v")
	plan := f.run(true)
	if len(plan.Worktrees.Deferred) != 1 || plan.Worktrees.Deferred[0].Reason != config.Text("gc.client_lazy_fetch_deferred") || plan.Worktrees.Count != 0 {
		t.Fatalf("plan: %+v", plan.Worktrees)
	}
	if after := gcGit(t, partial, "count-objects", "-v"); after != before {
		t.Fatalf("the object store changed:\n%s\n%s", before, after)
	}
}

// The value reaches the check exactly as Git stores it, also on the last
// entry, whose trailing white space a trimmed listing would lose: Git rejects
// "false " as a boolean, so it counts as a risk.
func TestPartialCloneKeepsTheWhiteSpaceOfTheLastValue(t *testing.T) {
	isolateGitConfig(t)
	repo := t.TempDir()
	gcGit(t, repo, "init", "-q")
	file, err := os.OpenFile(filepath.Join(repo, ".git", "config"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = file.WriteString("[remote \"origin\"]\n\tpromisor = \"false \"\n")
	if err := errors.Join(err, file.Close()); err != nil {
		t.Fatal(err)
	}
	listed, err := gitCommand(t.Context(), repo, "config", "--list", "-z")
	if err != nil || !strings.HasSuffix(listed, "remote.origin.promisor\nfalse \x00") {
		t.Fatalf("the promisor entry must come last and keep its space: %q %v", listed, err)
	}
	if !partialClone(listed) {
		t.Fatal("a value Git rejects as a boolean counts as a risk")
	}
}
