package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func bindWrapUpFixture(t *testing.T, root string, in *DispatchInput) {
	t.Helper()
	exemptReviewFixture(t, root, in.TaskID)
	in.Kind = "wrap-up"
	in.Base = strings.Repeat("b", 40)
	in.Evidence.WrapUp = &DispatchWrapUpBinding{Artifact: ArtifactReference{in.TaskID, dispatchPath(in.ID, "integration")}, Git: DispatchIntegration{DispatchID: in.ID, TaskID: in.TaskID, CWD: t.TempDir(), SourceCommit: in.Base, ReviewTarget: in.Base, ReviewBase: strings.Repeat("a", 40), TargetCommit: in.Base, TargetRef: "refs/heads/develop", Author: "coordinator", Basis: "structural fixture; no Git verification claimed", VerifiedAt: time.Now().UTC()}}
}

func fixBindingFixture(t *testing.T) (string, DispatchInput, ReviewRun, ReviewFinding) {
	t.Helper()
	root := tempBoard(t)
	id := gateCard(t, root, "binding")
	gatePlan(t, root, []string{id}, archiveRequirements())
	f := ReviewFinding{ID: "PM-01", Tier: "medium", Text: "需要修复", Evidence: "file.go:1"}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{f}
	run := gateRun(t, root, archiveInput([]string{id}, "pm-binding", "PMQA"), findings)
	assignGate(t, root, run, map[string][]string{f.ID: {id}})
	s := transactionSnapshot(t, root, id)
	if _, err := MoveEntry(s.Entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, id)
	in := dispatchInput(s, "fix-binding")
	in.Kind, in.Base = "fix", run.Commit
	in.Evidence.Fix = &DispatchFixBinding{BatchID: run.BatchID, Findings: []DispatchFindingReference{{FindingRef: FindingRef{run.RunID, f.ID}}}}
	return root, in, run, f
}

func TestDispatchFixBindingRejectsInvalidSources(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*DispatchInput)
	}{
		{"missing binding", func(in *DispatchInput) { in.Evidence.Fix = nil }},
		{"missing run", func(in *DispatchInput) { in.Evidence.Fix.Findings[0].RunID = "missing" }},
		{"missing finding", func(in *DispatchInput) { in.Evidence.Fix.Findings[0].FindingID = "PM-404" }},
		{"wrong predecessor", func(in *DispatchInput) { in.Evidence.Fix.Findings[0].PreviousRunID = "other" }},
		{"wrong batch", func(in *DispatchInput) { in.Evidence.Fix.BatchID = "other" }},
		{"wrong target", func(in *DispatchInput) { in.Base = strings.Repeat("c", 40) }},
		{"wrong task", func(in *DispatchInput) { in.TaskID = "20260908-foreign-task" }},
		{"duplicate", func(in *DispatchInput) {
			in.Evidence.Fix.Findings = append(in.Evidence.Fix.Findings, in.Evidence.Fix.Findings[0])
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root, in, _, _ := fixBindingFixture(t)
			id := in.TaskID
			before := transactionSnapshot(t, root, id)
			tc.change(&in)
			if _, err := PrepareDispatch(root, in); err == nil {
				t.Fatal("invalid source accepted")
			}
			after := transactionSnapshot(t, root, id)
			if before.Revision != after.Revision {
				t.Fatal("invalid evidence mutated card")
			}
		})
	}
}

func TestDispatchFixBindingRelocatesAndReplays(t *testing.T) {
	root, in, _, _ := fixBindingFixture(t)
	d := prepareTestDispatch(t, root, in)
	if err := ValidateDispatchEvidence(root, in.TaskID, in.ID); err != nil {
		t.Fatal(err)
	}
	if got := in.Evidence.Fix.Findings[0].Section; got != "" {
		t.Fatalf("prepare mutated caller section: %q", got)
	}
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	before := transactionSnapshot(t, root, in.TaskID)
	retry := in
	fix := *in.Evidence.Fix
	fix.Findings = append([]DispatchFindingReference(nil), fix.Findings...)
	for i := range fix.Findings {
		fix.Findings[i].Section = ""
	}
	retry.Evidence.Fix = &fix
	replay := prepareTestDispatch(t, root, retry)
	if !reflect.DeepEqual(replay.Input.Evidence.Fix.Findings[0].FindingRef, d.Input.Evidence.Fix.Findings[0].FindingRef) {
		t.Fatal("retry changed finding identity")
	}
	if before.Revision != transactionSnapshot(t, root, in.TaskID).Revision {
		t.Fatal("retry wrote card")
	}
	if err := ValidateDispatchEvidence(root, in.TaskID, in.ID); err != nil {
		t.Fatal("move invalidated relative identity", err)
	}
}

func TestDispatchFixExistingAuthorOriginals(t *testing.T) {
	root, in, run, f := fixBindingFixture(t)
	d := ReviewDisposition{RecordID: "author-one", RunID: run.RunID, FindingID: f.ID, BatchID: run.BatchID, TaskID: in.TaskID, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "confirmed", Basis: "已复现"}
	s := transactionSnapshot(t, root, in.TaskID)
	if _, err := MoveEntry(s.Entry, root, "working"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, in.TaskID)
	if err := SubmitReviewDisposition(root, d, s.Revision); err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareDispatch(root, in); err == nil {
		t.Fatal("omitted existing author original")
	}
	ref := DispatchAuthorReference{Finding: FindingRef{run.RunID, f.ID}, RecordID: d.RecordID, Author: d.Author, Artifact: ArtifactReference{in.TaskID, dispositionPath(d)}}
	in.Evidence.Fix.Authors = []DispatchAuthorReference{ref}
	in.Evidence.Fix.Authors[0].Author = "coordinator"
	if _, err := PrepareDispatch(root, in); err == nil {
		t.Fatal("impersonated original author")
	}
	in.Evidence.Fix.Authors[0] = ref
	grant := prepareTestDispatch(t, root, in)
	if _, err := dispatchMove(t, root, grant, "working"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, in.TaskID)
	path := filepath.Join(s.Entry.Path, dispositionPath(d))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err = ValidateDispatchEvidence(root, in.TaskID, in.ID); err == nil {
		t.Fatal("missing author original accepted")
	}
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	d.RecordID = "author-two"
	d.PreviousRecordID = "author-one"
	d.Authorization = &grant.Authorization
	if err = SubmitReviewDisposition(root, d, s.Revision); err != nil {
		t.Fatal(err)
	}
	replay := prepareTestDispatch(t, root, in)
	if len(replay.Input.Evidence.Fix.Authors) != 1 {
		t.Fatal("retry rebound later author revision")
	}
	if err = ValidateDispatchEvidence(root, in.TaskID, in.ID); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchFixIncompleteOriginalRejectedBeforeAttempt(t *testing.T) {
	root, in, run, _ := fixBindingFixture(t)
	d := prepareTestDispatch(t, root, in)
	s := transactionSnapshot(t, root, in.TaskID)
	if err := os.Remove(filepath.Join(s.Entry.Path, "reviews", run.RunID, "report.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := BeginDispatchAttempt(root, in.TaskID, in.ID, d.Revision); err == nil {
		t.Fatal("incomplete original sent")
	}
	current, err := ReadDispatch(root, in.TaskID, in.ID)
	if err != nil {
		t.Fatal(err)
	}
	if current.Attempts != 0 || current.State != DispatchPrepared {
		t.Fatal("attempt persisted on invalid evidence")
	}
}

func TestDispatchInputRejectsUnknownEvidenceJSON(t *testing.T) {
	var in DispatchInput
	if err := DecodeReviewJSON([]byte(`{"evidence":{"fix":{"batch_id":"one","findings":[],"typo":true}}}`), &in); err == nil {
		t.Fatal("unknown binding silently ignored")
	}
	data, _ := json.Marshal(DispatchEvidence{})
	if string(data) != "{}" {
		t.Fatal(string(data))
	}
}

func TestDispatchFixLegacyRequiresExplicitMapping(t *testing.T) {
	root := tempBoard(t)
	id := gateCard(t, root, "legacy-dispatch")
	run := finalizedRun(t, root, archiveInput([]string{id}, "legacy-fix", "PMQA"))
	run = publishRun(t, root, run.RunID)
	in := DispatchInput{ID: "legacy-dispatch", TaskID: id, Kind: "fix", Message: "修复映射问题", Base: run.Commit, Evidence: DispatchEvidence{Fix: &DispatchFixBinding{BatchID: run.BatchID, Findings: []DispatchFindingReference{{FindingRef: FindingRef{run.RunID, "PM-01"}}}}}}
	if _, err := PrepareDispatch(root, in); err == nil {
		t.Fatal("legacy prose inferred findings")
	}
	findings := emptyFindings()
	findings.Findings = []ReviewFinding{{ID: "PM-01", Tier: "medium", Text: "人工映射条目", Evidence: "report.md:1"}}
	mapping := LegacyFindingMap{RunID: run.RunID, ReportHash: run.Hashes["report.md"], Author: "human", Basis: "显式完整映射", Complete: true, Findings: findings, Locations: []LegacyFindingLocation{{FindingID: "PM-01", StartLine: 1, EndLine: 1, Quote: "PASS"}}}
	if err := MapLegacyReview(root, mapping); err != nil {
		t.Fatal(err)
	}
	assignGate(t, root, run, map[string][]string{"PM-01": {id}})
	d := prepareTestDispatch(t, root, in)
	if _, err := BeginDispatchAttempt(root, id, d.Input.ID, d.Revision); err != nil {
		t.Fatal(err)
	}
}

func TestDispatchFixRejectsSupersededRoundAndCarriesLineageAuthors(t *testing.T) {
	root, in, run, f := fixBindingFixture(t)
	s := transactionSnapshot(t, root, in.TaskID)
	if _, err := MoveEntry(s.Entry, root, "working"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, in.TaskID)
	record := ReviewDisposition{RecordID: "prior-author", RunID: run.RunID, FindingID: f.ID, BatchID: run.BatchID, TaskID: in.TaskID, Author: "codex", ReportHash: run.Hashes["report.md"], Original: f.Text, Status: "confirmed", Basis: "已确认原问题"}
	if err := SubmitReviewDisposition(root, record, s.Revision); err != nil {
		t.Fatal(err)
	}
	nextInput := archiveInput([]string{in.TaskID}, "pm-next", "PMQA")
	nextInput.PreviousRunID = run.RunID
	nextInput.ReviewedCommit = run.Commit
	nextFindings := emptyFindings()
	nextFindings.Findings = []ReviewFinding{{ID: "PM-02", Tier: "medium", Text: "前轮仍未关闭", Evidence: "file.go:2", Lineage: &FindingRef{run.RunID, f.ID}}}
	next := gateRun(t, root, nextInput, nextFindings)
	assignGate(t, root, next, map[string][]string{"PM-02": {in.TaskID}})
	if _, err := PrepareDispatch(root, in); err == nil {
		t.Fatal("superseded run dispatched")
	}
	in.Evidence.Fix.Findings = []DispatchFindingReference{{FindingRef: FindingRef{next.RunID, "PM-02"}, PreviousRunID: run.RunID}}
	if _, err := PrepareDispatch(root, in); err == nil {
		t.Fatal("prior author omitted from lineage")
	}
	in.Evidence.Fix.Authors = []DispatchAuthorReference{{Finding: FindingRef{run.RunID, f.ID}, RecordID: record.RecordID, Author: record.Author, Artifact: ArtifactReference{in.TaskID, dispositionPath(record)}}}
	prepareTestDispatch(t, root, in)
}

func nonBlockingFixFixture(t *testing.T, blocking bool) (string, DispatchInput, ReviewRun, ReviewFinding, ReviewFinding) {
	t.Helper()
	root := tempBoard(t)
	id := gateCard(t, root, "nonblock")
	gatePlan(t, root, []string{id}, archiveRequirements())
	findings := emptyFindings()
	nb := ReviewFinding{ID: "PM-NB", Tier: "low", Text: "建议整理命名", Evidence: "file.go:3"}
	findings.NonBlocking = []ReviewFinding{nb}
	var blockingFinding ReviewFinding
	if blocking {
		blockingFinding = ReviewFinding{ID: "PM-01", Tier: "medium", Text: "需要修复", Evidence: "file.go:1"}
		findings.Findings = []ReviewFinding{blockingFinding}
	}
	run := gateRun(t, root, archiveInput([]string{id}, "pm-nb", "PMQA"), findings)
	items := map[string][]string{nb.ID: {id}}
	if blocking {
		items[blockingFinding.ID] = []string{id}
	}
	assignGate(t, root, run, items)
	s := transactionSnapshot(t, root, id)
	if _, err := MoveEntry(s.Entry, root, "review"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, id)
	in := dispatchInput(s, "fix-nb")
	in.Kind, in.Base = "fix", run.Commit
	in.Evidence.Fix = &DispatchFixBinding{BatchID: run.BatchID, Findings: []DispatchFindingReference{{FindingRef: FindingRef{run.RunID, nb.ID}}}}
	return root, in, run, nb, blockingFinding
}

func TestDispatchFixBindsNonBlockingFindings(t *testing.T) {
	t.Run("only non-blocking without author originals", func(t *testing.T) {
		root, in, run, nb, _ := nonBlockingFixFixture(t, false)
		d := prepareTestDispatch(t, root, in)
		if got := d.Input.Evidence.Fix.Findings[0].Section; got != "NON_BLOCKING" {
			t.Fatalf("section=%q", got)
		}
		text := DispatchEvidenceInstruction(d)
		if !strings.Contains(text, "NON_BLOCKING: "+run.RunID+"/"+nb.ID) || strings.Contains(text, "FINDING: "+run.RunID+"/"+nb.ID) {
			t.Fatalf("instruction missing non-blocking mark: %s", text)
		}
		if err := ValidateDispatchEvidence(root, in.TaskID, in.ID); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("mixed findings and non-blocking", func(t *testing.T) {
		root, in, run, nb, blocking := nonBlockingFixFixture(t, true)
		in.Evidence.Fix.Findings = []DispatchFindingReference{
			{FindingRef: FindingRef{run.RunID, blocking.ID}},
			{FindingRef: FindingRef{run.RunID, nb.ID}},
		}
		d := prepareTestDispatch(t, root, in)
		sections := map[string]string{}
		for _, ref := range d.Input.Evidence.Fix.Findings {
			sections[ref.FindingID] = ref.Section
		}
		if sections[blocking.ID] != "FINDINGS" || sections[nb.ID] != "NON_BLOCKING" {
			t.Fatalf("%v", sections)
		}
		text := DispatchEvidenceInstruction(d)
		if !strings.Contains(text, "FINDING: "+run.RunID+"/"+blocking.ID) || !strings.Contains(text, "NON_BLOCKING: "+run.RunID+"/"+nb.ID) {
			t.Fatalf("instruction=%s", text)
		}
	})
	t.Run("lineage requires existing author originals", func(t *testing.T) {
		root, in, run, nb, _ := nonBlockingFixFixture(t, false)
		s := transactionSnapshot(t, root, in.TaskID)
		if _, err := MoveEntry(s.Entry, root, "working"); err != nil {
			t.Fatal(err)
		}
		s = transactionSnapshot(t, root, in.TaskID)
		record := ReviewDisposition{RecordID: "nb-prior", RunID: run.RunID, FindingID: nb.ID, BatchID: run.BatchID, TaskID: in.TaskID, Author: "codex", ReportHash: run.Hashes["report.md"], Original: nb.Text, Status: "deferred", Basis: "下轮再处理"}
		if err := SubmitReviewDisposition(root, record, s.Revision); err != nil {
			t.Fatal(err)
		}
		nextInput := archiveInput([]string{in.TaskID}, "pm-nb-next", "PMQA")
		nextInput.PreviousRunID = run.RunID
		nextInput.ReviewedCommit = run.Commit
		nextFindings := emptyFindings()
		nextFindings.NonBlocking = []ReviewFinding{{ID: "PM-NB2", Tier: "suggest", Text: "命名仍待整理", Evidence: "file.go:4", Lineage: &FindingRef{run.RunID, nb.ID}}}
		next := gateRun(t, root, nextInput, nextFindings)
		assignGate(t, root, next, map[string][]string{"PM-NB2": {in.TaskID}})
		in.Evidence.Fix.Findings = []DispatchFindingReference{{FindingRef: FindingRef{next.RunID, "PM-NB2"}, PreviousRunID: run.RunID}}
		if _, err := PrepareDispatch(root, in); err == nil {
			t.Fatal("prior non-blocking author omitted")
		}
		in.Evidence.Fix.Authors = []DispatchAuthorReference{{Finding: FindingRef{run.RunID, nb.ID}, RecordID: record.RecordID, Author: record.Author, Artifact: ArtifactReference{in.TaskID, dispositionPath(record)}}}
		d := prepareTestDispatch(t, root, in)
		if d.Input.Evidence.Fix.Findings[0].Section != "NON_BLOCKING" {
			t.Fatal(d.Input.Evidence.Fix.Findings[0].Section)
		}
	})
	t.Run("missing versus unassigned errors", func(t *testing.T) {
		root, in, _, nb, _ := nonBlockingFixFixture(t, false)
		in.Evidence.Fix.Findings[0].FindingID = "PM-404"
		if _, err := PrepareDispatch(root, in); err == nil || !strings.Contains(err.Error(), "missing finding") {
			t.Fatalf("missing: %v", err)
		}
		in.Evidence.Fix.Findings[0].FindingID = nb.ID
		root2 := tempBoard(t)
		id := gateCard(t, root2, "unassigned-nb")
		other := gateCard(t, root2, "other-owner")
		gatePlan(t, root2, []string{id, other}, archiveRequirements())
		findings := emptyFindings()
		findings.NonBlocking = []ReviewFinding{{ID: "PM-UA", Tier: "recommend", Text: "可延后", Evidence: "a:1"}}
		run2 := gateRun(t, root2, archiveInput([]string{id, other}, "pm-ua", "PMQA"), findings)
		assignGate(t, root2, run2, map[string][]string{"PM-UA": {other}})
		s := transactionSnapshot(t, root2, id)
		if _, err := MoveEntry(s.Entry, root2, "review"); err != nil {
			t.Fatal(err)
		}
		s = transactionSnapshot(t, root2, id)
		bad := dispatchInput(s, "fix-ua")
		bad.Kind, bad.Base = "fix", run2.Commit
		bad.Evidence.Fix = &DispatchFixBinding{BatchID: run2.BatchID, Findings: []DispatchFindingReference{{FindingRef: FindingRef{run2.RunID, "PM-UA"}}}}
		if _, err := PrepareDispatch(root2, bad); err == nil || !strings.Contains(err.Error(), "finding not assigned to task") {
			t.Fatalf("unassigned: %v", err)
		}
	})
}

func TestDispatchFixNonBlockingDispositionOnlyRound(t *testing.T) {
	root, in, run, nb, _ := nonBlockingFixFixture(t, false)
	d := prepareTestDispatch(t, root, in)
	if _, err := dispatchMove(t, root, d, "working"); err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, in.TaskID)
	record := ReviewDisposition{
		RecordID: "nb-done", RunID: run.RunID, FindingID: nb.ID, BatchID: run.BatchID, TaskID: in.TaskID,
		Author: "codex", ReportHash: run.Hashes["report.md"], Original: nb.Text, Status: "deferred",
		Basis: "本轮仅处置, 不改代码", Authorization: &d.Authorization,
	}
	if err := SubmitReviewDisposition(root, record, s.Revision); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, in.TaskID)
	if _, err := MoveWithOptions(s.Entry, root, "review", MoveOptions{Authorization: d.Authorization, DeliveryCommit: run.Commit}); err != nil {
		t.Fatal(err)
	}
	completed, err := ReadDispatch(root, in.TaskID, in.ID)
	if err != nil || completed.State != DispatchCompleted || completed.Completed.DeliveryCommit != run.Commit {
		t.Fatalf("%+v %v", completed, err)
	}
	closed, err := gateClose(t, root, map[string]ReviewRoleConclusion{"PMQA": passRole(run)})
	if err != nil {
		t.Fatal(err)
	}
	if closed.RoleStatuses["PMQA"] != "PASS" || closed.TargetCommit != run.Commit {
		t.Fatalf("%+v", closed)
	}
}
