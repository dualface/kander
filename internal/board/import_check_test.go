package board

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestClosedProblemTasksNeedsEveryCardClosed(t *testing.T) {
	root := "/board"
	b := Board{Entries: map[string]Entry{
		"done-task":    {TaskID: "done-task", State: "done", Path: root + "/done/done-task", Document: root + "/done/done-task/spec.md"},
		"trash-task":   {TaskID: "trash-task", State: "trash", Path: root + "/trash/trash-task.md", Document: root + "/trash/trash-task.md"},
		"working-task": {TaskID: "working-task", State: "working", Path: root + "/working/working-task", Document: root + "/working/working-task/spec.md"},
	}}
	for name, tc := range map[string]struct {
		problem Problem
		want    []string
	}{
		"task id":               {Problem{Path: "done-task"}, []string{"done-task"}},
		"entry path":            {Problem{Path: root + "/done/done-task"}, []string{"done-task"}},
		"file inside the entry": {Problem{Path: root + "/done/done-task/reviews/r1/report.md"}, []string{"done-task"}},
		"two closed cards":      {Problem{Path: "trash-task", RelatedPaths: []string{root + "/done/done-task"}}, []string{"done-task", "trash-task"}},
		"an active card":        {Problem{Path: "working-task"}, nil},
		"closed with active":    {Problem{Path: "done-task", RelatedPaths: []string{root + "/working/working-task"}}, nil},
		"no such card":          {Problem{Path: "stray-run"}, nil},
		"the board itself":      {Problem{Path: root}, nil},
		"a sibling name":        {Problem{Path: root + "/done/done-task-other"}, nil},
	} {
		t.Run(name, func(t *testing.T) {
			got, ok := closedProblemTasks(b, tc.problem)
			if ok != (tc.want != nil) || !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %v %v, want %v", got, ok, tc.want)
			}
		})
	}
}

// groupCard writes a task group member into state whose PREREQUISITES name
// prerequisite, and returns its ID.
func groupCard(t *testing.T, root, slug, state, prerequisite string) string {
	t.Helper()
	target, err := NewTask(root, "feature", slug, "Card "+slug, "en", false)
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(target)
	spec := filepath.Join(target, "spec.md")
	data, err := os.ReadFile(spec)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(data), "<FILL_IN>", "filled")
	text = strings.Replace(text, "- TASK_GROUP:\n", "- TASK_GROUP: 20261117-import-group\n", 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\n```text\nPREREQUISITES: "+prerequisite+"\n```\n", 1)
	if err := os.WriteFile(spec, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	if state != "backlog" {
		if err := os.Rename(target, filepath.Join(root, state, id)); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

func TestCheckBoardForImportProblemsSplitsWarningsFromBlockingProblems(t *testing.T) {
	root := tempBoard(t)
	if err := InitStorage(root); err != nil {
		t.Fatal(err)
	}
	// A dependency cycle between a done card and an active one is not a warning,
	// and neither is a problem that only concerns closed cards.
	first := groupCard(t, root, "first", "done", "N/A")
	second := groupCard(t, root, "second", "working", first)
	editPrerequisite(t, filepath.Join(root, "done", first, "spec.md"), second)
	check, err := CheckBoardForImportProblems(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Warnings) != 0 || !strings.Contains(strings.Join(check.Blocking, "\n"), "cycle") {
		t.Fatalf("a cycle through an active card blocks: %+v", check)
	}
	// Closing the second card turns the same cycle into a warning about both.
	if err := os.Rename(filepath.Join(root, "working", second), filepath.Join(root, "done", second)); err != nil {
		t.Fatal(err)
	}
	check, err = CheckBoardForImportProblems(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Blocking) != 0 || len(check.Warnings) != 1 || !strings.Contains(check.Warnings[0].Message, "cycle") {
		t.Fatalf("a cycle of closed cards is a warning: %+v", check)
	}
	if want := []string{first, second}; !reflect.DeepEqual(check.Warnings[0].TaskIDs, want) {
		t.Fatalf("the warning names every card of the cycle: %v, want %v", check.Warnings[0].TaskIDs, want)
	}
	// The unchanged strict check still reports it.
	if code, _, problems, err := CheckBoardForImport(root); err != nil || code == 0 || len(problems) != 1 {
		t.Fatalf("CheckBoardForImport: %d %v %v", code, problems, err)
	}
}

func editPrerequisite(t *testing.T, spec, prerequisite string) {
	t.Helper()
	data, err := os.ReadFile(spec)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "PREREQUISITES: N/A", "PREREQUISITES: "+prerequisite, 1)
	if err := os.WriteFile(spec, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestDefaultMissingSizeInsertsOneLineIntoDirectoryCardsOnly(t *testing.T) {
	root := tempBoard(t)
	if err := InitStorage(root); err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, slug := range []string{"alpha", "beta"} {
		target, err := NewTask(root, "feature", slug, "Card "+slug, "en", slug == "beta")
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, filepath.Base(target))
	}
	sizeLine := regexp.MustCompile(`(?m)^- SIZE: (small|large)\n`)
	spec := func(id string) string { return filepath.Join(root, "backlog", id, "spec.md") }
	original := map[string]string{}
	for _, id := range ids[:1] {
		data, err := os.ReadFile(spec(id))
		if err != nil {
			t.Fatal(err)
		}
		stripped := sizeLine.ReplaceAllString(string(data), "")
		if err := os.WriteFile(spec(id), []byte(stripped), 0o600); err != nil {
			t.Fatal(err)
		}
		original[id] = stripped
	}
	beta, err := os.ReadFile(spec(ids[1]))
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(root, "backlog", "20261117-legacy-task.md")
	if err := os.WriteFile(legacy, []byte("# Legacy\n\n- TYPE: Feature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	changed, err := DefaultMissingSize(root)
	if err != nil {
		t.Fatal(err)
	}
	if want := []SizeDefault{{TaskID: ids[0], Document: "backlog/" + ids[0] + "/spec.md"}}; !reflect.DeepEqual(changed, want) {
		t.Fatalf("changed %+v, want %+v", changed, want)
	}
	got, _ := os.ReadFile(spec(ids[0]))
	if string(got) != DefaultedSizeText(original[ids[0]]) || strings.Count(string(got), "- SIZE: large\n") != 1 {
		t.Fatalf("the card gains exactly one SIZE line:\n%s", got)
	}
	if after, _ := os.ReadFile(spec(ids[1])); string(after) != string(beta) {
		t.Fatal("a card with a SIZE line is left alone")
	}
	if after, _ := os.ReadFile(legacy); string(after) != "# Legacy\n\n- TYPE: Feature\n" {
		t.Fatal("a legacy file card is left alone")
	}
	if again, err := DefaultMissingSize(root); err != nil || len(again) != 0 {
		t.Fatalf("a second run changes nothing: %+v %v", again, err)
	}
}
