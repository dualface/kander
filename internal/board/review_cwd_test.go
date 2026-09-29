package board

import (
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSameReviewCWDWindowsSpellings(t *testing.T) {
	same := [][2]string{
		{`C:\x\wt`, `C:/x/wt`},
		{`C:\x\wt`, `c:/x/wt/`},
		{`C:\x\wt\`, `c:\X\WT`},
		{`C:\x\.\wt`, `C:/x/sub/../wt`},
		{`C:\x\\wt`, `C:/x/wt`},
		{`\\server\share\wt`, `//server/share/wt/`},
		{`C:\`, `c:/`},
	}
	for _, c := range same {
		if !sameReviewCWD(c[0], c[1], true) {
			t.Errorf("%q and %q must name the same directory", c[0], c[1])
		}
	}
	different := [][2]string{
		{`C:\x\wt`, `C:\x\wt2`},
		{`C:\x\wt`, `D:\x\wt`},
		{`C:\x\wt`, `C:\x`},
		{`\\server\share\wt`, `\server\share\wt`},
		{`C:\x\wt`, ``},
	}
	for _, c := range different {
		if sameReviewCWD(c[0], c[1], true) {
			t.Errorf("%q and %q must stay different", c[0], c[1])
		}
	}
	if !sameReviewCWD("", "", true) {
		t.Error("two empty CWDs are equal")
	}
}

func TestSameReviewCWDPOSIXStaysCaseSensitive(t *testing.T) {
	if !sameReviewCWD("/repo/wt", "/repo/wt/", false) || !sameReviewCWD("/repo/./wt", "/repo//wt", false) {
		t.Fatal("cleaned POSIX spellings must match")
	}
	for _, c := range [][2]string{{"/repo/wt", "/Repo/wt"}, {"/repo/wt", "/repo/wt2"}, {`/repo/wt`, `\repo\wt`}} {
		if sameReviewCWD(c[0], c[1], false) {
			t.Errorf("%q and %q must stay different on POSIX", c[0], c[1])
		}
	}
}

// A plan stored before this change may hold any spelling of the worktree root. Runs and closure
// evidence now carry the canonical `C:\...` form, and the batch must still close without rewriting the
// stored plan.
func TestHistoricalNonCanonicalPlanCWDStillCloses(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path spellings")
	}
	root := tempBoard(t)
	id := gateCard(t, root, "plan-cwd")
	p := ReviewPlan{Schema: 1, Sealed: true, PlanID: "plan-" + id, Author: "coordinator", Basis: "historical slash spelling", CWD: "c:/x/wt/", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []ReviewPlanBatch{{BatchID: "batch", TaskIDs: []string{id}, Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: archiveRequirements()}}}
	if err := createReviewPlan(root, p, false); err != nil {
		t.Fatal(err)
	}
	input := archiveInput([]string{id}, "pmqa", "PMQA")
	input.CWD = `C:\x\wt`
	run := gateRun(t, root, input, emptyFindings())
	assignGate(t, root, run, map[string][]string{})
	if _, err := PublishReviewDisposition(root, "batch"); err != nil {
		t.Fatal(err)
	}
	v, err := ReadReviewBatchView(root, "batch")
	if err != nil {
		t.Fatal(err)
	}
	r := ReviewCloseRequest{BatchID: "batch", ExpectedRevision: v.Batch.Revision, ViewHash: ReviewViewDigest(v), Author: "coordinator", Roles: map[string]ReviewRoleConclusion{"PMQA": passRole(run)}}
	edges, _, err := ReviewClosureEdges(v, r)
	if err != nil {
		t.Fatal(err)
	}
	closure, err := CloseReviewBatch(root, r, ReviewGitEvidence{CWD: `C:\X\wt`, Head: v.Batch.TargetCommit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges})
	if err != nil || closure.RoleStatuses["PMQA"] != "PASS" {
		t.Fatalf("%+v %v", closure, err)
	}
	stored, err := ReadReviewPlan(root, p.PlanID)
	if err != nil || stored.CWD != "c:/x/wt/" {
		t.Fatalf("stored plan CWD rewritten: %q %v", stored.CWD, err)
	}
	if problems := CheckReviewGate(root, []string{id}); len(problems) > 0 {
		t.Fatalf("gate: %+v", problems)
	}
}

func TestPlanCWDStillRejectsDifferentDirectory(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "plan-cwd-other")
	gatePlan(t, root, []string{id}, archiveRequirements())
	input := archiveInput([]string{id}, "pmqa", "PMQA")
	input.CWD = "/repo-other"
	if _, _, err := PrepareReviewRun(root, input, nil, nil, archiveOriginals(), "test"); err == nil || !strings.Contains(err.Error(), "worktree mismatch") {
		t.Fatalf("different directory accepted: %v", err)
	}
}
