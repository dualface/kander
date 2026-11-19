package board

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func TestClosedProblemTasksNeedsEveryCardClosed(t *testing.T) {
	// Paths go through filepath because closedProblemTasks matches entry
	// directories with filepath.Separator.
	root := filepath.FromSlash("/board")
	doneDir := filepath.Join(root, "done", "done-task")
	workingDir := filepath.Join(root, "working", "working-task")
	trashFile := filepath.Join(root, "trash", "trash-task.md")
	b := Board{Entries: map[string]Entry{
		"done-task":    {TaskID: "done-task", State: "done", Path: doneDir, Document: filepath.Join(doneDir, "spec.md")},
		"trash-task":   {TaskID: "trash-task", State: "trash", Path: trashFile, Document: trashFile},
		"working-task": {TaskID: "working-task", State: "working", Path: workingDir, Document: filepath.Join(workingDir, "spec.md")},
	}}
	for name, tc := range map[string]struct {
		problem Problem
		want    []string
	}{
		"task id":               {Problem{Path: "done-task"}, []string{"done-task"}},
		"entry path":            {Problem{Path: doneDir}, []string{"done-task"}},
		"file inside the entry": {Problem{Path: filepath.Join(doneDir, "reviews", "r1", "report.md")}, []string{"done-task"}},
		"two closed cards":      {Problem{Path: "trash-task", RelatedPaths: []string{doneDir}}, []string{"done-task", "trash-task"}},
		"an active card":        {Problem{Path: "working-task"}, nil},
		"closed with active":    {Problem{Path: "done-task", RelatedPaths: []string{workingDir}}, nil},
		"no such card":          {Problem{Path: "stray-run"}, nil},
		"the board itself":      {Problem{Path: root}, nil},
		"a sibling name":        {Problem{Path: filepath.Join(root, "done", "done-task-other")}, nil},
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
	// A dependency cycle that includes an active card blocks the import, because
	// it does not concern only closed cards.
	first := groupCard(t, root, "first", "done", "N/A")
	second := groupCard(t, root, "second", "working", first)
	editPrerequisite(t, filepath.Join(root, "done", first, "spec.md"), second)
	check, err := CheckBoardForImportProblems(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(check.Warnings) != 0 || len(check.Blocking) != 1 || !strings.Contains(check.Blocking[0], first) || !strings.Contains(check.Blocking[0], second) {
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
	if len(check.Blocking) != 0 || len(check.Warnings) != 1 || !strings.Contains(check.Warnings[0].Message, first) || !strings.Contains(check.Warnings[0].Message, second) {
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

func TestClassifyImportProblemsOrdersItsResult(t *testing.T) {
	b := Board{Entries: map[string]Entry{
		"a-task": {TaskID: "a-task", State: "done", Path: "/b/done/a-task"},
		"b-task": {TaskID: "b-task", State: "archived", Path: "/b/archived/b-task"},
		"w-task": {TaskID: "w-task", State: "working", Path: "/b/working/w-task"},
	}}
	problems := []Problem{
		{Path: "b-task", Message: "second"},
		{Path: "w-task", Message: "z blocks"},
		{Path: "a-task", Message: "b later"},
		{Path: "w-task", Message: "a blocks"},
		{Path: "a-task", Message: "a first"},
	}
	want := classifyImportProblems(b, problems)
	if got := []string{want.Warnings[0].Message, want.Warnings[1].Message, want.Warnings[2].Message}; !reflect.DeepEqual(got, []string{"a first", "b later", "second"}) {
		t.Fatalf("warnings are ordered by card, then message: %v", got)
	}
	if len(want.Blocking) != 2 || want.Blocking[0] >= want.Blocking[1] {
		t.Fatalf("blocking lines are sorted: %v", want.Blocking)
	}
	for i := range problems {
		rotated := append(append([]Problem{}, problems[i:]...), problems[:i]...)
		if got := classifyImportProblems(b, rotated); !reflect.DeepEqual(got, want) {
			t.Fatalf("input order %d changes the result: %+v vs %+v", i, got, want)
		}
	}
}

func TestImportCheckNamesTheCardsOfABatchProblemOnce(t *testing.T) {
	root := tempBoard(t)
	if err := InitStorage(root); err != nil {
		t.Fatal(err)
	}
	first := gateCard(t, root, "batch-first")
	second := gateCard(t, root, "batch-second")
	gatePlan(t, root, []string{first, second}, noReviewRequirements())
	// A legacy board whose plan target lags the runtime batch.
	driftBatchTarget(t, root, "batch", strings.Repeat("c", 40))
	mismatch := strings.Repeat("b", 40) + " != batch target " + strings.Repeat("c", 40)
	matching := func(check ImportCheck) (warnings []ImportFinding, blocking int) {
		for _, w := range check.Warnings {
			if strings.Contains(w.Message, mismatch) {
				warnings = append(warnings, w)
			}
		}
		return warnings, strings.Count(strings.Join(check.Blocking, "\n"), mismatch)
	}
	run := func() ImportCheck {
		check, err := CheckBoardForImportProblems(root)
		if err != nil {
			t.Fatal(err)
		}
		return check
	}
	// One member is still working: the batch problem concerns it, and blocks once.
	if err := os.Rename(filepath.Join(root, "working", first), filepath.Join(root, "done", first)); err != nil {
		t.Fatal(err)
	}
	if warnings, blocking := matching(run()); len(warnings) != 0 || blocking == 0 {
		t.Fatalf("a batch problem with an active member blocks: warnings %+v blocking %d", warnings, blocking)
	}
	// Every member closed: the problem is one warning that names the whole batch.
	if err := os.Rename(filepath.Join(root, "working", second), filepath.Join(root, "done", second)); err != nil {
		t.Fatal(err)
	}
	check := run()
	warnings, blocking := matching(check)
	want := []string{first, second}
	slices.Sort(want)
	if len(warnings) == 0 || blocking != 0 {
		t.Fatalf("closed members: warnings %+v blocking %d", warnings, blocking)
	}
	seen := map[string]bool{}
	for _, w := range warnings {
		if !reflect.DeepEqual(w.TaskIDs, want) || seen[w.Message] {
			t.Fatalf("each warning names every card of the batch once: %+v", warnings)
		}
		seen[w.Message] = true
	}
	// The same checks reach the problem through both cards, and report it once per message.
	if len(warnings) != 1 {
		t.Fatalf("a batch problem is one warning, not one per checked card: %+v", warnings)
	}
}

func TestImportCheckLeavesAnUnrelatedActiveCardOutOfABatchProblem(t *testing.T) {
	root := tempBoard(t)
	if err := InitStorage(root); err != nil {
		t.Fatal(err)
	}
	closed := gateCard(t, root, "closed-batch")
	active := gateCard(t, root, "active-batch")
	requirements := noReviewRequirements()
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-two-batches", Author: "coordinator", Basis: "two batches of one plan", CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{closed, active}, Batches: []ReviewPlanBatch{
		{BatchID: "first", TaskIDs: []string{closed}, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: requirements},
		{BatchID: "second", PreviousBatchID: "first", TaskIDs: []string{active}, Base: strings.Repeat("b", 40), TargetCommit: strings.Repeat("d", 40), Requirements: requirements},
	}}
	if err := CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	driftBatchTarget(t, root, "first", strings.Repeat("c", 40))
	if err := os.Rename(filepath.Join(root, "working", closed), filepath.Join(root, "done", closed)); err != nil {
		t.Fatal(err)
	}
	check, err := CheckBoardForImportProblems(root)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := strings.Repeat("b", 40) + " != batch target " + strings.Repeat("c", 40)
	if strings.Contains(strings.Join(check.Blocking, "\n"), mismatch) {
		t.Fatalf("the problem of the closed batch must not block through the active card: %v", check.Blocking)
	}
	var named [][]string
	for _, w := range check.Warnings {
		if strings.Contains(w.Message, mismatch) {
			named = append(named, w.TaskIDs)
		}
	}
	if !reflect.DeepEqual(named, [][]string{{closed}}) {
		t.Fatalf("the warning names the closed batch alone: %v", named)
	}
}

func TestImportCheckReportsABatchProblemOnceAcrossTheEvidenceAndGateChecks(t *testing.T) {
	root := tempBoard(t)
	if err := InitStorage(root); err != nil {
		t.Fatal(err)
	}
	first := gateCard(t, root, "evidence-first")
	second := gateCard(t, root, "evidence-second")
	ids := []string{first, second}
	gatePlan(t, root, ids, archiveRequirements())
	// A published run makes the evidence check reach the batch problem too,
	// wrapped with the run ID, besides the import gate that reports it bare.
	gateRun(t, root, archiveInput(ids, "evidence-run", "PMQA"), emptyFindings())
	driftBatchTarget(t, root, "batch", strings.Repeat("c", 40))
	for _, id := range ids {
		if err := os.Rename(filepath.Join(root, "working", id), filepath.Join(root, "done", id)); err != nil {
			t.Fatal(err)
		}
	}
	check, err := CheckBoardForImportProblems(root)
	if err != nil {
		t.Fatal(err)
	}
	mismatch := strings.Repeat("b", 40) + " != batch target " + strings.Repeat("c", 40)
	if strings.Contains(strings.Join(check.Blocking, "\n"), mismatch) {
		t.Fatalf("a closed batch must not block: %v", check.Blocking)
	}
	var named []ImportFinding
	for _, w := range check.Warnings {
		if strings.Contains(w.Message, mismatch) {
			named = append(named, w)
		}
	}
	want := slices.Clone(ids)
	slices.Sort(want)
	if len(named) != 1 || !reflect.DeepEqual(named[0].TaskIDs, want) {
		t.Fatalf("one warning naming the whole batch, got %+v", named)
	}
}
