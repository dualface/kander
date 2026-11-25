package api

import "fmt"

// ExecutionBranch is the task branch of one execution generation. It lies
// outside refs/heads/kander/, which holds evidence refs only: the baseline and
// the delivery of a generation are published below
// refs/heads/kander/<project>/<task>/g<generation>/, and Git cannot hold a
// branch and a directory of the same name.
func ExecutionBranch(project, task string, generation uint64) string {
	return fmt.Sprintf("kander-task/%s/%s/g%d", project, task, generation)
}

// LegacyExecutionBranch is the name Kander gave the task branch before
// ExecutionBranch. It collides with the evidence refs of its own generation,
// so it can never be pushed to the common remote under that name. Cards that
// started earlier still record it.
func LegacyExecutionBranch(project, task string, generation uint64) string {
	return fmt.Sprintf("kander/%s/%s/g%d", project, task, generation)
}

// KnownExecutionBranch reports whether branch is the current or the legacy
// name of that generation's task branch.
func KnownExecutionBranch(branch, project, task string, generation uint64) bool {
	return branch != "" && (branch == ExecutionBranch(project, task, generation) || branch == LegacyExecutionBranch(project, task, generation))
}
