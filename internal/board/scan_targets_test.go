package board

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A targeted scan must classify a missing ID the same way a full scan does:
// a coded not-found error from Locate, with the problem still reported, while
// a damaged card keeps its uncoded blocked error.
func TestTargetedScanLocatesMissingAndDamagedTasks(tt *testing.T) {
	root := tempBoard(tt)
	missing, damaged := "20261128-missing-task", "20261128-damaged-task"
	if err := os.Mkdir(filepath.Join(root, "todo", damaged), 0o755); err != nil {
		tt.Fatal(err)
	}

	for _, scanner := range []struct {
		name string
		scan func() (Board, error)
	}{
		{"targeted", func() (Board, error) { return scanTargets(root, []string{missing, damaged}) }},
		{"full", func() (Board, error) { return scan(root) }},
	} {
		b, err := scanner.scan()
		if err != nil {
			tt.Fatalf("%s: %v", scanner.name, err)
		}
		_, err = Locate(b, missing)
		var located *Error
		if !errors.As(err, &located) || located.Code != "board.task_does_not_exist" || located.Message != t("board.task_does_not_exist", missing) {
			tt.Fatalf("%s: missing task error = %#v", scanner.name, err)
		}
		damagedPath := filepath.Join(root, "todo", damaged)
		_, err = Locate(b, damaged)
		if !errors.As(err, &located) || located.Code != "" || located.Message != t("board.large_task_is_missing_spec_md", damagedPath) {
			tt.Fatalf("%s: damaged task error = %#v", scanner.name, err)
		}
	}

	b, err := scanTargets(root, []string{missing})
	if err != nil {
		tt.Fatal(err)
	}
	want := Problem{Path: root, Message: t("board.task_does_not_exist", missing)}
	if !slices.ContainsFunc(b.Problems, func(p Problem) bool { return p.Path == want.Path && p.Message == want.Message }) {
		tt.Fatalf("targeted scan problems = %#v", b.Problems)
	}
	if _, problems, err := collectBoardProblems(root, []string{missing}, false, false); err != nil || len(problems) != 1 || problems[0].Message != want.Message {
		tt.Fatalf("check problems = %#v, %v", problems, err)
	}
}
