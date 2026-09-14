package board

import (
	"reflect"
	"strings"
	"testing"
)

func TestOriginalAndIntegratedReviewEvidenceLifecycle(t *testing.T) {
	for _, integrated := range []bool{false, true} {
		name := "original"
		roles := []string{"PM", "QA", "CSA", "Hacker"}
		requirements := noReviewRequirements()
		if integrated {
			name, roles = "integrated", []string{"PMQA", "Security"}
		}
		for _, role := range roles {
			requirements[role] = "required"
		}
		t.Run(name, func(t *testing.T) {
			root := tempBoard(t)
			id := gateCard(t, root, name)
			gatePlan(t, root, []string{id}, requirements)
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
			if integrated {
				partial := map[string]ReviewRoleConclusion{"Security": conclusions["Security"]}
				if _, err := gateClose(t, root, partial); err == nil || !strings.Contains(err.Error(), "PMQA") {
					t.Fatal("closure skipped required integrated role", err)
				}
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
		})
	}
}

func TestIntegratedRequirementsRejectIncompleteAndUnknownRoles(t *testing.T) {
	for _, requirements := range []map[string]string{
		{"QA": "required", "Security": "required"},
		{"PM": "required", "QA": "required", "CSA": "required", "Hacker": "required", "PMQA": "required"},
		{"PM": "required", "QA": "required", "CSA": "required", "Hacker": "required", "PMQA": "required", "Other": "required"},
	} {
		root := tempBoard(t)
		id := archiveCard(t, root, "invalid-integrated")
		if _, _, err := PrepareReviewRun(root, archiveInput([]string{id}, "invalid", "QA"), requirements, nil, archiveOriginals(), "test"); err == nil {
			t.Fatal("accepted incomplete or unknown role set", requirements)
		}
	}
}

func TestWaiversForOriginalAndIntegratedSecurityRoles(t *testing.T) {
	for _, role := range []string{"PM", "QA", "CSA", "Hacker", "PMQA", "Security"} {
		t.Run(role, func(t *testing.T) {
			root := tempBoard(t)
			id := gateCard(t, root, "waiver-"+strings.ToLower(role))
			requirements := noReviewRequirements()
			requirements["PMQA"], requirements["Security"] = "N/A: fixture", "N/A: fixture"
			requirements[role] = "required"
			gatePlan(t, root, []string{id}, requirements)
			f := ReviewFinding{ID: role + "-01", Tier: "high", Text: "material risk", Evidence: "x:1"}
			findings := emptyFindings()
			findings.Findings = []ReviewFinding{f}
			run := gateRun(t, root, archiveInput([]string{id}, "risk", role), findings)
			assignGate(t, root, run, map[string][]string{f.ID: {id}})
			d := ReviewDisposition{RecordID: "accepted-risk", RunID: run.RunID, BatchID: run.BatchID, FindingID: f.ID, TaskID: id, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "waived", Basis: "verified risk", Waiver: &ReviewWaiver{Policy: "accepted-risk", Decision: "explicit user acceptance"}}
			err := SubmitReviewDisposition(root, d, transactionSnapshot(t, root, id).Revision)
			security := role == "CSA" || role == "Hacker" || role == "Security"
			if !security {
				if err == nil {
					t.Fatal("non-security waiver accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			closure, err := gateClose(t, root, map[string]ReviewRoleConclusion{role: passRole(run)})
			if err != nil || closure.RoleStatuses[role] != "accepted-risk" {
				t.Fatalf("waiver became PASS: %+v %v", closure, err)
			}
		})
	}
}
