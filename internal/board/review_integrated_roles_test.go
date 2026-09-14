package board

import (
	"reflect"
	"strings"
	"testing"
)

func TestOriginalAndIntegratedReviewEvidenceLifecycle(t *testing.T) {
	t.Run("historical-four", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "original")
		requirements := map[string]string{"PM": "required", "QA": "required", "CSA": "required", "Hacker": "required"}
		historicalPlan(t, root, []string{id}, requirements)
		runHistoricalRoles(t, root, id, []string{"PM", "QA", "CSA", "Hacker"}, requirements)
	})
	t.Run("current-two", func(t *testing.T) {
		root := tempBoard(t)
		id := gateCard(t, root, "current")
		requirements := map[string]string{"PMQA": "required", "Security": "required"}
		gatePlan(t, root, []string{id}, requirements)
		runHistoricalRoles(t, root, id, []string{"PMQA", "Security"}, requirements)
	})
}

func runHistoricalRoles(t *testing.T, root, id string, roles []string, requirements map[string]string) {
	t.Helper()
	conclusions := map[string]ReviewRoleConclusion{}
	originals := map[string]string{}
	for _, role := range roles {
		run := gateRun(t, root, archiveInput([]string{id}, strings.ToLower(role), role), emptyFindings())
		assignGate(t, root, run, map[string][]string{})
		conclusions[role] = passRole(run)
		originals[run.RunID] = reviewJSON(run)
	}
	if _, err := PublishReviewDisposition(root, "batch"); err != nil {
		t.Fatal(err)
	}
	closure, err := gateClose(t, root, conclusions)
	if err != nil || len(closure.RoleStatuses) != len(requirements) {
		t.Fatalf("closure=%+v: %v", closure, err)
	}
	for _, role := range roles {
		if closure.RoleStatuses[role] != "PASS" {
			t.Fatal("missing role conclusion", role)
		}
	}
	for runID, original := range originals {
		if recovered := publishRun(t, root, runID); reviewJSON(recovered) != original {
			t.Fatal("recovery changed original", runID)
		}
	}
	again, err := gateClose(t, root, conclusions)
	if err != nil || !reflect.DeepEqual(closure, again) {
		t.Fatal("closure retry changed evidence", err)
	}
	if code, out, stderr, err := CheckBoard(root, []string{id}, true); err != nil || code != 0 {
		t.Fatalf("%d %s %v %v", code, out, stderr, err)
	}
}

func TestIntegratedRequirementsRejectIncompleteAndUnknownRoles(t *testing.T) {
	for _, requirements := range []map[string]string{
		{"QA": "required", "Security": "required"},
		{"PM": "required", "QA": "required", "CSA": "required", "Hacker": "required", "PMQA": "required"},
		{"PM": "required", "QA": "required", "CSA": "required", "Hacker": "required", "PMQA": "required", "Other": "required"},
		{"PM": "required", "QA": "required", "CSA": "required", "Hacker": "required"},
		{"PM": "required", "QA": "required", "CSA": "required", "Hacker": "required", "PMQA": "required", "Security": "required"},
	} {
		root := tempBoard(t)
		id := archiveCard(t, root, "invalid-integrated")
		if _, _, err := PrepareReviewRun(root, archiveInput([]string{id}, "invalid", "QA"), requirements, nil, archiveOriginals(), "test"); err == nil {
			t.Fatal("accepted incomplete, unknown, or new four/six-key role set", requirements)
		}
	}
}

func TestWaiversForOriginalAndIntegratedSecurityRoles(t *testing.T) {
	for _, tc := range []struct {
		role       string
		historical bool
		security   bool
	}{
		{"PM", true, false},
		{"QA", true, false},
		{"CSA", true, true},
		{"Hacker", true, true},
		{"PMQA", false, false},
		{"Security", false, true},
	} {
		t.Run(tc.role, func(t *testing.T) {
			root := tempBoard(t)
			id := gateCard(t, root, "waiver-"+strings.ToLower(tc.role))
			var requirements map[string]string
			if tc.historical {
				requirements = historicalFourRoleRequirements()
				requirements[tc.role] = "required"
				historicalPlan(t, root, []string{id}, requirements)
			} else {
				requirements = noReviewRequirements()
				requirements[tc.role] = "required"
				gatePlan(t, root, []string{id}, requirements)
			}
			f := ReviewFinding{ID: tc.role + "-01", Tier: "high", Text: "material risk", Evidence: "x:1"}
			findings := emptyFindings()
			findings.Findings = []ReviewFinding{f}
			run := gateRun(t, root, archiveInput([]string{id}, "risk", tc.role), findings)
			assignGate(t, root, run, map[string][]string{f.ID: {id}})
			d := ReviewDisposition{RecordID: "accepted-risk", RunID: run.RunID, BatchID: run.BatchID, FindingID: f.ID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "waived", Basis: "verified risk", Waiver: &ReviewWaiver{Policy: "accepted-risk", Decision: "explicit user acceptance"}}
			err := SubmitReviewDisposition(root, d, transactionSnapshot(t, root, id).Revision)
			if !tc.security {
				if err == nil {
					t.Fatal("non-security waiver accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			closure, err := gateClose(t, root, map[string]ReviewRoleConclusion{tc.role: passRole(run)})
			if err != nil || closure.RoleStatuses[tc.role] != "accepted-risk" {
				t.Fatalf("waiver became PASS: %+v %v", closure, err)
			}
		})
	}
}
