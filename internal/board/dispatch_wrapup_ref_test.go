package board

import (
	"strings"
	"testing"
)

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

func TestWrapUpTargetFollowsEitherMainlineWithoutReceipt(t *testing.T) {
	for _, ref := range []string{"refs/heads/develop", "refs/remotes/origin/develop", "refs/heads/main", "refs/remotes/origin/main"} {
		if !validWrapUpTarget(DispatchIntegration{CWD: "/repo", TargetRef: ref}) {
			t.Fatalf("local wrap-up rejected %s", ref)
		}
	}
	if validWrapUpTarget(DispatchIntegration{CWD: "/repo", TargetRef: "refs/heads/release"}) {
		t.Fatal("local wrap-up accepted a non-mainline target")
	}
}

func TestWrapUpTargetWithReceiptKeepsServiceBinding(t *testing.T) {
	receipt := DispatchIntegration{
		CWD:                   "project:demo",
		TargetRef:             "refs/heads/main",
		IntegrationID:         "i-" + strings.Repeat("a", 64),
		IntegrationExecution:  "execution-1",
		IntegrationGeneration: 1,
	}
	if !validWrapUpTarget(receipt) {
		t.Fatal("portable wrap-up into main rejected")
	}
	local := receipt
	local.CWD = "/repo"
	if validWrapUpTarget(local) {
		t.Fatal("a receipt-bound wrap-up accepted a local CWD")
	}
}
