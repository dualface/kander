package check

import (
	"context"
	"slices"
)

func runOverlap(ctx context.Context, git gitRunner, sourceRef, headRef string) (OverlapResult, int) {
	head, errInfo, _ := git.resolve(ctx, headRef)
	if errInfo != nil {
		return overlapError(errInfo.Code, errInfo.Message), exitExec
	}
	source, errInfo, _ := git.resolve(ctx, sourceRef)
	if errInfo != nil {
		result := overlapError(errInfo.Code, errInfo.Message)
		result.HeadCommit = head
		return result, exitExec
	}
	mergeBase, errInfo := git.mergeBase(ctx, head, source)
	if errInfo != nil {
		result := overlapError(errInfo.Code, errInfo.Message)
		result.HeadCommit = head
		result.SourceCommit = source
		return result, exitExec
	}
	headChanges, errInfo := git.nameStatus(ctx, mergeBase, head)
	if errInfo != nil {
		result := overlapError(errInfo.Code, errInfo.Message)
		result.MergeBase = mergeBase
		result.HeadCommit = head
		result.SourceCommit = source
		return result, exitExec
	}
	sourceChanges, errInfo := git.nameStatus(ctx, mergeBase, source)
	if errInfo != nil {
		result := overlapError(errInfo.Code, errInfo.Message)
		result.MergeBase = mergeBase
		result.HeadCommit = head
		result.SourceCommit = source
		return result, exitExec
	}
	paths := intersectPaths(headChanges, sourceChanges)
	status := overlapStatus(len(paths) > 0)
	return OverlapResult{
		SchemaVersion: schemaVersion,
		Check:         checkOverlap,
		Status:        status,
		MergeBase:     mergeBase,
		HeadCommit:    head,
		SourceCommit:  source,
		Paths:         paths,
		Error:         nil,
	}, completedExit(status)
}

func intersectPaths(headChanges, sourceChanges []change) []GitPath {
	headSet := pathSet(headChanges)
	var out []GitPath
	seen := map[string]struct{}{}
	for _, item := range sourceChanges {
		for _, raw := range changePaths(item) {
			key := string(raw)
			if _, ok := headSet[key]; !ok {
				continue
			}
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, gitPath(raw))
		}
	}
	slices.SortFunc(out, compareGitPath)
	if out == nil {
		return emptyPaths()
	}
	return out
}

func pathSet(changes []change) map[string]struct{} {
	set := make(map[string]struct{})
	for _, item := range changes {
		for _, raw := range changePaths(item) {
			set[string(raw)] = struct{}{}
		}
	}
	return set
}
