package board

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GuardVerdict is the decision of guard-write.
type GuardVerdict struct {
	Allowed bool
	Reason  string
}

// GuardWrite decides whether writing to path would resurrect a stale card in a board state directory.
// A direct child of a state directory (a card entry) that does not currently exist is rejected: a legitimate new card only comes from kander new,
// so a missing entry means the card has moved or never existed, and writing it in place would create a cross-state duplicate.
// Files inside a directory card likewise require the owning card entry to exist, otherwise the write would silently rebuild the whole directory card.
// An empty root means no board can be located in the current environment, which always passes.
func GuardWrite(root, path string) (GuardVerdict, error) {
	if root == "" {
		return GuardVerdict{Allowed: true}, nil
	}
	abs, err := absoluteUserPath(path)
	if err != nil {
		return GuardVerdict{}, err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || rel == ".." {
		return GuardVerdict{Allowed: true}, nil
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) < 2 {
		return GuardVerdict{Allowed: true}, nil
	}
	state := parts[0]
	if _, ok := stateSet[state]; !ok {
		return GuardVerdict{Allowed: true}, nil
	}
	entryPath := filepath.Join(root, state, parts[1])
	if _, err := os.Lstat(entryPath); err == nil {
		return GuardVerdict{Allowed: true}, nil
	} else if !os.IsNotExist(err) {
		return GuardVerdict{}, err
	}
	taskID := strings.TrimSuffix(parts[1], ".md")
	for _, other := range States {
		if other == state {
			continue
		}
		if _, err := os.Lstat(filepath.Join(root, other, taskID)); err == nil {
			return GuardVerdict{Reason: t("board.guard_task_moved", taskID, other)}, nil
		}
		if _, err := os.Lstat(filepath.Join(root, other, taskID+".md")); err == nil {
			return GuardVerdict{Reason: t("board.guard_task_moved", taskID, other)}, nil
		}
	}
	return GuardVerdict{Reason: t("board.guard_missing_direct_child", state)}, nil
}

// RunGuardWrite implements kander guard-write. Exit code 0 allows, 1 rejects, 2 is a usage error.
func RunGuardWrite(args []string) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "-") {
		return usageFail("guard-write", "board.guard_path_required")
	}
	root, err := BoardRoot()
	if err != nil {
		// When no board can be located (not a Git project and KANBAN_DIR unset) the host project's write is not blocked;
		// other location errors (illegal path, unsafe entry) still fail instead of silently passing.
		if IsBoardNotFound(err) {
			return 0
		}
		return fail(err)
	}
	verdict, err := GuardWrite(root, args[0])
	if err != nil {
		return fail(err)
	}
	if verdict.Allowed {
		return 0
	}
	fmt.Fprintln(os.Stderr, t("board.guard_rejected", args[0], verdict.Reason))
	return 1
}
