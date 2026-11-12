package board

import "testing"

func TestWrapUpTargetRefAcceptsEitherMainline(t *testing.T) {
	for _, ref := range []string{"refs/heads/develop", "refs/remotes/origin/develop", "refs/heads/main", "refs/remotes/origin/main"} {
		if !mainlineTargetRef(ref) {
			t.Fatalf("rejected %s", ref)
		}
	}
	for _, ref := range []string{"", "develop", "main", "refs/heads/task", "refs/heads/group/g", "refs/remotes/upstream/main", "refs/tags/main"} {
		if mainlineTargetRef(ref) {
			t.Fatalf("accepted %s", ref)
		}
	}
}
