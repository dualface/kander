package launch

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/internal/remote"
)

// A complete reconcile runs the cleanup of the group's evidence in the
// checkout it verified in, and a failed cleanup only warns.
func TestReconcileRunsTheGroupEvidenceCleanup(t *testing.T) {
	f := newReconcileFixture(t)
	var groups, dirs []string
	saved := cleanGroupEvidence
	t.Cleanup(func() { cleanGroupEvidence = saved })
	cleanGroupEvidence = func(_ context.Context, _ *remote.Session, group, dir string) remote.EvidenceCleanupReport {
		groups, dirs = append(groups, group), append(dirs, dir)
		return remote.EvidenceCleanupReport{Err: errors.New("remote unreachable in the fixture")}
	}
	status, stdout, stderr := f.run()
	if status != 0 {
		t.Fatalf("status %d: %s %s", status, stdout, stderr)
	}
	if len(groups) != 1 || groups[0] != f.group || dirs[0] != f.clone {
		t.Fatalf("cleanup calls: %v %v", groups, dirs)
	}
	if !strings.Contains(stderr, "remote unreachable in the fixture") {
		t.Fatalf("stderr %q lacks the cleanup warning", stderr)
	}
}

// The real cleanup of a group whose members are still in flight deletes
// nothing and prints nothing.
func TestReconcileOfAnUnsettledGroupKeepsItsEvidence(t *testing.T) {
	f := newReconcileFixture(t)
	before := runGit(t, f.clone, "ls-remote", "--refs", "origin")
	status, stdout, stderr := f.run()
	if status != 0 || strings.Contains(stderr, "warning") {
		t.Fatalf("status %d: %s %s", status, stdout, stderr)
	}
	if after := runGit(t, f.clone, "ls-remote", "--refs", "origin"); after != before || !strings.Contains(after, "refs/heads/kander/") {
		t.Fatalf("refs changed:\n%s\n%s", before, after)
	}
}
