package check

import (
	"context"
	"slices"
)

func runDelivery(ctx context.Context, git gitRunner, baseRef, targetRef string) (DeliveryResult, int) {
	base, errInfo, _ := git.resolve(ctx, baseRef)
	if errInfo != nil {
		result := deliveryError(errInfo.Code, errInfo.Message)
		return result, exitExec
	}
	target, errInfo, _ := git.resolve(ctx, targetRef)
	if errInfo != nil {
		result := deliveryError(errInfo.Code, errInfo.Message)
		result.BaseCommit = base
		return result, exitExec
	}
	if ancestorErr := git.isAncestor(ctx, base, target); ancestorErr != nil {
		result := deliveryError(ancestorErr.Code, ancestorErr.Message)
		result.BaseCommit = base
		result.TargetCommit = target
		return result, exitExec
	}
	diff, errInfo := git.diffCheck(ctx, base, target)
	if errInfo != nil {
		result := deliveryError(errInfo.Code, errInfo.Message)
		result.BaseCommit = base
		result.TargetCommit = target
		return result, exitExec
	}
	changes, errInfo := git.nameStatus(ctx, base, target)
	if errInfo != nil {
		result := deliveryError(errInfo.Code, errInfo.Message)
		result.BaseCommit = base
		result.TargetCommit = target
		result.DiffCheck = diff
		return result, exitExec
	}
	added, crossed, errInfo := lineCandidates(ctx, git, base, target, changes)
	if errInfo != nil {
		result := deliveryError(errInfo.Code, errInfo.Message)
		result.BaseCommit = base
		result.TargetCommit = target
		result.DiffCheck = diff
		return result, exitExec
	}
	slices.SortFunc(added, compareCandidates)
	slices.SortFunc(crossed, compareCandidates)
	status := deliveryStatus(diff.Status == statusFail, len(added)+len(crossed) > 0)
	return DeliveryResult{
		SchemaVersion:  schemaVersion,
		Check:          checkDelivery,
		Status:         status,
		BaseCommit:     base,
		TargetCommit:   target,
		DiffCheck:      diff,
		AddedOverLimit: added,
		CrossedLimit:   crossed,
		Error:          nil,
	}, completedExit(status)
}

func lineCandidates(ctx context.Context, git gitRunner, base, target string, changes []change) ([]LineCandidate, []LineCandidate, *CheckError) {
	added := emptyCandidates()
	crossed := emptyCandidates()
	for _, item := range changes {
		switch item.letter {
		case 'D':
			continue
		case 'A', 'C':
			targetLines, errInfo := countBlob(ctx, git, target, item.path)
			if errInfo != nil {
				return nil, nil, errInfo
			}
			if targetLines <= lineLimit {
				continue
			}
			baseLines := 0
			var basePath *GitPath
			if item.letter == 'C' && item.oldPath != nil {
				n, errInfo := countBlob(ctx, git, base, item.oldPath)
				if errInfo != nil {
					return nil, nil, errInfo
				}
				baseLines = n
				basePath = gitPathPtr(item.oldPath)
			}
			added = append(added, LineCandidate{
				Status:      candidateStatus,
				Path:        gitPath(item.path),
				BasePath:    basePath,
				BaseLines:   baseLines,
				TargetLines: targetLines,
			})
		default:
			oldPath := item.path
			if item.oldPath != nil {
				oldPath = item.oldPath
			}
			baseLines, errInfo := countBlob(ctx, git, base, oldPath)
			if errInfo != nil {
				return nil, nil, errInfo
			}
			targetLines, errInfo := countBlob(ctx, git, target, item.path)
			if errInfo != nil {
				return nil, nil, errInfo
			}
			if baseLines > lineLimit || targetLines <= lineLimit {
				continue
			}
			var basePath *GitPath
			if item.oldPath != nil {
				basePath = gitPathPtr(item.oldPath)
			}
			crossed = append(crossed, LineCandidate{
				Status:      candidateStatus,
				Path:        gitPath(item.path),
				BasePath:    basePath,
				BaseLines:   baseLines,
				TargetLines: targetLines,
			})
		}
	}
	return added, crossed, nil
}

func countBlob(ctx context.Context, git gitRunner, commit string, path []byte) (int, *CheckError) {
	blob, errInfo := git.blob(ctx, commit, path)
	if errInfo != nil {
		return 0, errInfo
	}
	return physicalLines(blob), nil
}
