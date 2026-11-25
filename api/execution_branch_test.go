package api

import (
	"strings"
	"testing"
)

func TestExecutionBranchNames(t *testing.T) {
	if got := ExecutionBranch("p-1", "20261118-x-task", 2); got != "kander-task/p-1/20261118-x-task/g2" {
		t.Fatalf("task branch = %q", got)
	}
	if got := LegacyExecutionBranch("p-1", "20261118-x-task", 2); got != "kander/p-1/20261118-x-task/g2" {
		t.Fatalf("legacy task branch = %q", got)
	}
}

func TestKnownExecutionBranch(t *testing.T) {
	const project, task, generation = "p-1", "20261118-x-task", 2
	for _, branch := range []string{ExecutionBranch(project, task, generation), LegacyExecutionBranch(project, task, generation)} {
		if !KnownExecutionBranch(branch, project, task, generation) {
			t.Fatalf("%q was refused for its own generation", branch)
		}
	}
	for name, branch := range map[string]string{
		"other generation":  ExecutionBranch(project, task, generation+1),
		"other task":        ExecutionBranch(project, "20261118-y-task", generation),
		"other project":     ExecutionBranch("p-2", task, generation),
		"empty":             "",
		"full ref":          "refs/heads/" + ExecutionBranch(project, task, generation),
		"legacy other gen":  LegacyExecutionBranch(project, task, generation+1),
		"legacy other task": LegacyExecutionBranch(project, "20261118-y-task", generation),
	} {
		if KnownExecutionBranch(branch, project, task, generation) {
			t.Fatalf("%s: %q was accepted", name, branch)
		}
	}
}

func TestExecutionBranchNeverSharesAPathWithEvidenceRefs(t *testing.T) {
	const commit = "0123456789abcdef0123456789abcdef01234567"
	ref := "refs/heads/" + ExecutionBranch("p-1", "20261118-x-task", 2)
	if strings.HasPrefix(ref, "refs/heads/kander/") {
		t.Fatalf("task branch %q lies below the evidence namespace", ref)
	}
	for _, evidence := range []string{DeliveryRef("p-1", "20261118-x-task", 2, commit), IntegrationSourceRef("p-1", "exec-1", commit)} {
		if ref == evidence || strings.HasPrefix(evidence, ref+"/") || strings.HasPrefix(ref, evidence+"/") {
			t.Fatalf("task branch %q and evidence ref %q cannot coexist in one ref store", ref, evidence)
		}
	}
}
