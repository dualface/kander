package launch

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
)

func gitIn(t *testing.T, cwd string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s %v", args, out, err)
	}
	return strings.TrimSpace(string(out))
}

func commitIn(t *testing.T, cwd, message string) string {
	t.Helper()
	gitIn(t, cwd, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", message)
	return gitIn(t, cwd, "rev-parse", "HEAD")
}

func targetEvidence(cwd, commit, ref string) board.DispatchIntegration {
	return board.DispatchIntegration{CWD: cwd, SourceCommit: commit, ReviewTarget: commit, ReviewBase: commit, TargetCommit: commit, TargetRef: ref}
}

// targetRepo returns a checkout whose `v2` branch holds one commit beyond the
// first. The bare repository stands in for origin; it only has the refs the
// caller pushes.
func targetRepo(t *testing.T) (cwd, first, head string) {
	t.Helper()
	cwd, first = integrationGit(t)
	gitIn(t, cwd, "branch", "v2")
	gitIn(t, cwd, "switch", "-q", "v2")
	head = commitIn(t, cwd, "v2 delivery")
	gitIn(t, cwd, "switch", "-q", "develop")
	origin := t.TempDir()
	gitIn(t, origin, "init", "-q", "--bare")
	gitIn(t, cwd, "remote", "add", "origin", origin)
	return cwd, first, head
}

func TestIntegrationTargetAcceptsOnlyTheNamedRef(t *testing.T) {
	cwd, _, head := targetRepo(t)
	ctx := context.Background()

	t.Run("origin ref", func(t *testing.T) {
		gitIn(t, cwd, "push", "-q", "origin", "v2")
		gitIn(t, cwd, "fetch", "-q", "origin")
		if err := verifyIntegrationTarget(ctx, targetEvidence(cwd, head, "refs/remotes/origin/v2"), "v2"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("origin ref without a local branch", func(t *testing.T) {
		gitIn(t, cwd, "branch", "-D", "v2")
		defer gitIn(t, cwd, "branch", "v2", head)
		if err := verifyIntegrationTarget(ctx, targetEvidence(cwd, head, "refs/remotes/origin/v2"), "v2"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("local ref without an origin ref", func(t *testing.T) {
		gitIn(t, cwd, "update-ref", "-d", "refs/remotes/origin/v2")
		if err := verifyIntegrationTarget(ctx, targetEvidence(cwd, head, "refs/heads/v2"), "v2"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("named ref missing", func(t *testing.T) {
		if err := verifyIntegrationTarget(ctx, targetEvidence(cwd, head, "refs/remotes/origin/v2"), "v2"); err == nil {
			t.Fatal("a missing ref was accepted")
		}
	})
	t.Run("mainline ref for a card that names another target", func(t *testing.T) {
		if err := verifyIntegrationTarget(ctx, targetEvidence(cwd, head, "refs/heads/develop"), "v2"); err == nil {
			t.Fatal("develop accepted for a card targeting v2")
		}
	})
	t.Run("target commit is not on the target", func(t *testing.T) {
		other := commitIn(t, cwd, "develop only")
		if err := verifyIntegrationTarget(ctx, targetEvidence(cwd, other, "refs/heads/v2"), "v2"); err == nil {
			t.Fatal("a commit outside v2 was accepted")
		}
	})
	t.Run("no target field keeps the mainline rule", func(t *testing.T) {
		if err := verifyIntegrationTarget(ctx, targetEvidence(cwd, head, "refs/heads/v2"), ""); err == nil {
			t.Fatal("v2 accepted for a card with no target field")
		}
	})
}

func TestIntegrationTargetMainFollowsBranchMode(t *testing.T) {
	cwd, first := integrationGit(t)
	gitIn(t, cwd, "branch", "main", first)
	evidence := targetEvidence(cwd, first, "refs/heads/main")
	ctx := context.Background()

	useGitBranchMode(t, config.GitBranchModeMainDevelop)
	if err := verifyIntegrationTarget(ctx, evidence, "main"); err == nil {
		t.Fatal("main accepted as a target in main-develop mode")
	}
	useGitBranchMode(t, config.GitBranchModeMainOnly)
	if err := verifyIntegrationTarget(ctx, evidence, "main"); err != nil {
		t.Fatalf("main rejected as the mainline of main-only mode: %v", err)
	}
	// develop is an ordinary branch once main is the mainline.
	evidence.TargetRef = "refs/heads/develop"
	if err := verifyIntegrationTarget(ctx, evidence, "develop"); err != nil {
		t.Fatalf("develop rejected as a target in main-only mode: %v", err)
	}
}
