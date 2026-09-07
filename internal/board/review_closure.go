package board

import (
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/internal/fs"
)

type ReviewRunView struct {
	Run        ReviewRun           `json:"run"`
	Findings   *ReviewFindings     `json:"findings,omitempty"`
	Assignment *ReviewAssignment   `json:"assignment,omitempty"`
	Records    []ReviewDisposition `json:"records"`
}

// ReviewBatchView is generated from originals; it never substitutes an orchestrator's
// wording for author records. Members without findings need no author submission.
type ReviewBatchView struct {
	Schema int             `json:"schema"`
	Batch  ReviewBatch     `json:"batch"`
	Runs   []ReviewRunView `json:"runs"`
}
type ReviewRoleConclusion struct {
	RunID    string `json:"run_id"`
	PassedAt string `json:"passed_at"`
	Basis    string `json:"basis"`
}
type ReviewOpinion struct {
	Author  string      `json:"author"`
	Basis   string      `json:"basis"`
	Finding *FindingRef `json:"finding,omitempty"`
}
type ReviewCloseRequest struct {
	Mechanical       []ReviewMechanicalAssessment    `json:"mechanical,omitempty"`
	BatchID          string                          `json:"batch_id"`
	ExpectedRevision uint64                          `json:"expected_revision"`
	ViewHash         string                          `json:"view_hash"`
	Author           string                          `json:"author"`
	Roles            map[string]ReviewRoleConclusion `json:"roles"`
	ResolvedFailures map[string]string               `json:"resolved_failures"`
	Opinions         []ReviewOpinion                 `json:"opinions"`
}
type ReviewGitEdge struct {
	Ancestor   string `json:"ancestor"`
	Descendant string `json:"descendant"`
}
type ReviewGitEvidence struct {
	Mechanical    []ReviewMechanicalGit `json:"mechanical,omitempty"`
	NotApplicable string                `json:"not_applicable,omitempty"`
	CWD           string                `json:"cwd"`
	Head          string                `json:"head"`
	VerifiedAt    string                `json:"verified_at"`
	Edges         []ReviewGitEdge       `json:"edges"`
}
type ReviewClosure struct {
	Schema       int                `json:"schema"`
	PlanID       string             `json:"plan_id"`
	BatchID      string             `json:"batch_id"`
	TargetCommit string             `json:"target_commit"`
	Request      ReviewCloseRequest `json:"request"`
	Git          ReviewGitEvidence  `json:"git"`
	RoleStatuses map[string]string  `json:"role_statuses"`
	View         ReviewBatchView    `json:"view"`
	ClosedAt     string             `json:"closed_at"`
}

func ReviewViewDigest(v ReviewBatchView) string { return ReviewDigest([]byte(reviewJSON(v))) }
func batchRuns(tx *Transaction, b ReviewBatch) ([]ReviewRun, error) {
	entries, err := fs.ListDirectory(tx.root, control(tx.root, "groups", reviewControlGroup, "runs"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var result []ReviewRun
	for _, entry := range entries {
		if !ValidReviewID(entry.Name) || entry.Kind != fs.KindDirectory {
			return nil, reviewError("invalid run entry")
		}
		var run ReviewRun
		exists, err := readReviewJSON(tx, reviewRunName(entry.Name), &run)
		if err != nil {
			return nil, err
		}
		if !exists || run.RunID != entry.Name {
			return nil, reviewError("invalid run identity")
		}
		if run.BatchID == b.BatchID {
			result = append(result, run)
		}
	}
	slices.SortFunc(result, func(a, b ReviewRun) int { return strings.Compare(a.RunID, b.RunID) })
	return result, nil
}
func aggregateReviewBatch(tx *Transaction, b ReviewBatch) (ReviewBatchView, error) {
	view := ReviewBatchView{Schema: 1, Batch: b, Runs: []ReviewRunView{}}
	runs, err := batchRuns(tx, b)
	if err != nil {
		return view, err
	}
	for _, run := range runs {
		if err = verifyPublishedReview(tx, run); err != nil {
			return view, err
		}
		rv := ReviewRunView{Run: run, Records: []ReviewDisposition{}}
		if run.ExecutionStatus == "ok" {
			findings, err := runFindings(tx, run)
			if err != nil {
				return view, err
			}
			if err = validateFindingLineage(tx, run, findings); err != nil {
				return view, err
			}
			ledger, err := readDispositionLedger(tx, run, true)
			if err != nil {
				return view, err
			}
			rv.Findings = &findings
			rv.Assignment = &ledger.Assignment
			rv.Records = ledger.Records
		}
		view.Runs = append(view.Runs, rv)
	}
	return view, nil
}
func validateFindingLineage(tx *Transaction, run ReviewRun, f ReviewFindings) error {
	previousItems := map[string]bool{}
	if run.PreviousRunID != "" {
		var previous ReviewRun
		ok, err := readReviewJSON(tx, reviewRunName(run.PreviousRunID), &previous)
		if err != nil {
			return err
		}
		if !ok {
			return reviewError("missing lineage run")
		}
		if previous.ExecutionStatus == "ok" {
			pf, err := runFindings(tx, previous)
			if err != nil {
				return err
			}
			for _, item := range pf.all() {
				previousItems[item.ID] = true
			}
		}
	}
	seen := map[string]bool{}
	for _, item := range f.all() {
		if item.Lineage == nil {
			if previousItems[item.ID] {
				return reviewError("reused finding ID requires explicit lineage")
			}
			continue
		}
		if item.Lineage.RunID != run.PreviousRunID || seen[item.Lineage.key()] {
			return reviewError("finding lineage must reference unique immediate predecessor item")
		}
		seen[item.Lineage.key()] = true
		var previous ReviewRun
		ok, err := readReviewJSON(tx, reviewRunName(run.PreviousRunID), &previous)
		if err != nil {
			return err
		}
		if !ok {
			return reviewError("missing lineage run")
		}
		pf, err := runFindings(tx, previous)
		if err != nil {
			return err
		}
		found := false
		for _, p := range pf.all() {
			if p.ID == item.Lineage.FindingID {
				found = true
			}
		}
		if !found {
			return reviewError("lineage item not in predecessor")
		}
	}
	return nil
}
func ReadReviewBatchView(root, id string) (v ReviewBatchView, err error) {
	b, err := ReadReviewBatch(root, id)
	if err != nil {
		return v, err
	}
	err = WithTransaction(root, reviewScope(b.TaskIDs, true), func(tx *Transaction) error {
		ok, e := readReviewJSON(tx, reviewBatchName(id), &b)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing batch")
		}
		v, e = aggregateReviewBatch(tx, b)
		return e
	})
	return
}
func PublishReviewDisposition(root, id string) (v ReviewBatchView, err error) {
	b, err := ReadReviewBatch(root, id)
	if err != nil {
		return v, err
	}
	err = WithTransaction(root, reviewScope(b.TaskIDs, false), func(tx *Transaction) error {
		ok, e := readReviewJSON(tx, reviewBatchName(id), &b)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing batch")
		}
		v, e = aggregateReviewBatch(tx, b)
		if e != nil {
			return e
		}
		for _, task := range b.TaskIDs {
			if e = tx.Put(task, "reviews/batches/"+id+"/disposition.json", reviewJSON(v)); e != nil {
				return e
			}
		}
		return tx.PutGroup(reviewControlGroup, "dispositions/"+id+".json", reviewJSON(v))
	})
	return
}
func latestViewRecords(rv ReviewRunView) []ReviewDisposition {
	return latestDispositions(dispositionLedger{Records: rv.Records})
}

// ReviewClosureEdges validates semantic coverage and returns the exact Git claims
// which the review layer must verify. Structural validation makes no Git calls.
func ReviewClosureEdges(v ReviewBatchView, r ReviewCloseRequest) ([]ReviewGitEdge, map[string]string, error) {
	b := v.Batch
	if _, err := ReviewMechanicalClaims(v, r); err != nil {
		return nil, nil, err
	}
	if r.BatchID != b.BatchID || r.ExpectedRevision != b.Revision || r.ViewHash != ReviewViewDigest(v) || strings.TrimSpace(r.Author) == "" {
		return nil, nil, reviewError("close target/revision/view CAS")
	}
	if err := validateRequirements(b.Requirements); err != nil {
		return nil, nil, err
	}
	statuses := map[string]string{}
	runs := map[string]ReviewRunView{}
	referenced := map[string]bool{}
	for _, rv := range v.Runs {
		runs[rv.Run.RunID] = rv
		if rv.Run.PreviousRunID != "" {
			referenced[rv.Run.PreviousRunID] = true
		}
	}
	var edges []ReviewGitEdge
	add := func(a, d string) { edges = append(edges, ReviewGitEdge{a, d}) }
	if noGitReviewBatch(b.Base, b.TargetCommit, b.Requirements) {
		if len(v.Runs) > 0 {
			return nil, nil, reviewError("non-Git N/A batch cannot contain reviewer runs")
		}
	} else {
		add(b.Base, b.TargetCommit)
	}
	required := 0
	for _, role := range []string{"PM", "QA", "CSA", "Hacker"} {
		if b.Requirements[role] != "required" {
			statuses[role] = b.Requirements[role]
			if _, ok := r.Roles[role]; ok {
				return nil, nil, reviewError("N/A role conclusion")
			}
			continue
		}
		required++
		c, ok := r.Roles[role]
		rv, found := runs[c.RunID]
		if !ok || !found || rv.Run.Role != role || rv.Run.ExecutionStatus != "ok" || strings.TrimSpace(c.Basis) == "" || !validCommit(c.PassedAt) {
			return nil, nil, reviewError("required successful role conclusion: " + role)
		}
		if referenced[c.RunID] {
			return nil, nil, reviewError("old role conclusion selected")
		}
		chain := map[string]bool{}
		current := rv.Run
		for {
			if chain[current.RunID] {
				return nil, nil, reviewError("role cycle")
			}
			chain[current.RunID] = true
			if current.PreviousRunID == "" {
				break
			}
			next, ok := runs[current.PreviousRunID]
			if !ok {
				return nil, nil, reviewError("missing role predecessor")
			}
			current = next.Run
		}
		for _, other := range v.Runs {
			if other.Run.Role != role {
				continue
			}
			if other.Run.ExecutionStatus == "ok" && !chain[other.Run.RunID] {
				return nil, nil, reviewError("unconnected successful role run")
			}
			if other.Run.ExecutionStatus != "ok" {
				replacement, ok := runs[r.ResolvedFailures[other.Run.RunID]]
				if !ok || !chain[replacement.Run.RunID] || replacement.Run.ExecutionStatus != "ok" {
					return nil, nil, reviewError("failed run requires explicit successful replacement")
				}
				add(other.Run.Commit, replacement.Run.Commit)
			}
		}
		statuses[role] = "PASS"
		add(rv.Run.Commit, c.PassedAt)
		add(c.PassedAt, b.TargetCommit)
		mechanicalFix := false
		for _, prior := range v.Runs {
			if prior.Run.Role != role || prior.Run.ExecutionStatus != "ok" {
				continue
			}
			if prior.Findings == nil || prior.Assignment == nil {
				return nil, nil, reviewError("missing parsed findings and assignment")
			}
			items := map[string]ReviewFinding{}
			for _, f := range prior.Findings.all() {
				items[f.ID] = f
			}
			for _, d := range latestViewRecords(prior) {
				item := items[d.FindingID]
				if d.Status == "confirmed" || d.Status == "unverifiable" {
					return nil, nil, reviewError("unresolved must-fix: " + d.RunID + "/" + d.FindingID)
				}
				if d.Status == "waived" {
					statuses[role] = d.Waiver.Policy
				}
				if d.Status != "fixed" {
					continue
				}
				if d.FixCommit == prior.Run.Commit {
					return nil, nil, reviewError("fix must follow reviewed commit")
				}
				add(prior.Run.Commit, d.FixCommit)
				add(d.FixCommit, b.TargetCommit)
				if !mustFix(item.Tier) {
					continue
				}
				if prior.Run.RunID == c.RunID {
					if d.Mechanical == "" {
						return nil, nil, reviewError("non-mechanical fix requires a new run")
					}
					mechanicalFix = true
					add(d.FixCommit, c.PassedAt)
				} else {
					add(d.FixCommit, rv.Run.Commit)
				}
			}
		}
		if !mechanicalFix && c.PassedAt != rv.Run.Commit {
			return nil, nil, reviewError("PASSED_AT advance requires mechanical fixes")
		}
	}
	if len(r.Roles) != required {
		return nil, nil, reviewError("unexpected role conclusions")
	}
	for id, replacement := range r.ResolvedFailures {
		rv, ok := runs[id]
		if !ok || rv.Run.ExecutionStatus == "ok" || replacement == "" {
			return nil, nil, reviewError("invalid failure resolution")
		}
	}
	for _, op := range r.Opinions {
		if strings.TrimSpace(op.Author) == "" || strings.TrimSpace(op.Basis) == "" {
			return nil, nil, reviewError("opinion author and basis required")
		}
		if op.Finding != nil {
			rv, ok := runs[op.Finding.RunID]
			found := false
			if ok && rv.Findings != nil {
				for _, f := range rv.Findings.all() {
					if f.ID == op.Finding.FindingID {
						found = true
					}
				}
			}
			if !found {
				return nil, nil, reviewError("opinion finding reference")
			}
		}
	}
	slices.SortFunc(edges, func(a, b ReviewGitEdge) int {
		return strings.Compare(a.Ancestor+"/"+a.Descendant, b.Ancestor+"/"+b.Descendant)
	})
	edges = slices.Compact(edges)
	for _, edge := range edges {
		if !validCommit(edge.Ancestor) || !validCommit(edge.Descendant) {
			return nil, nil, reviewError("full commit required")
		}
	}
	return edges, statuses, nil
}
func CloseReviewBatch(root string, r ReviewCloseRequest, evidence ReviewGitEvidence) (c ReviewClosure, err error) {
	b, err := ReadReviewBatch(root, r.BatchID)
	if err != nil {
		return c, err
	}
	var plan ReviewPlan
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error { var e error; plan, e = batchPlan(tx, b); return e })
	if err != nil {
		return c, err
	}
	err = WithTransaction(root, reviewScope(plan.TaskIDs, false), func(tx *Transaction) error {
		ok, e := readReviewJSON(tx, reviewBatchName(r.BatchID), &b)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing batch")
		}
		p, e := batchPlan(tx, b)
		if e != nil {
			return e
		}
		if e = verifyPlanCopies(tx, p); e != nil {
			return e
		}
		if e = validatePreviousClosure(tx, b); e != nil {
			return e
		}
		v, e := aggregateReviewBatch(tx, b)
		if e != nil {
			return e
		}
		edges, statuses, e := ReviewClosureEdges(v, r)
		if e != nil {
			return e
		}
		if noGitReviewBatch(b.Base, b.TargetCommit, b.Requirements) != (strings.TrimSpace(evidence.NotApplicable) != "") {
			return reviewError("Git N/A evidence binding")
		}
		if e = verifyMechanicalEvidence(v, r, evidence); e != nil {
			return e
		}
		if evidence.CWD != p.CWD || evidence.Head != b.TargetCommit || !reflect.DeepEqual(edges, evidence.Edges) {
			return reviewError("Git evidence binding")
		}
		if _, e = time.Parse(time.RFC3339Nano, evidence.VerifiedAt); e != nil {
			return reviewError("Git verification timestamp")
		}
		existing, e := readReviewJSON(tx, closureName(b.BatchID), &c)
		if e != nil {
			return e
		}
		if existing {
			if !reflect.DeepEqual(c.Request, r) {
				return reviewError("immutable closure")
			}
			return verifyClosureCopies(tx, c)
		}
		c = ReviewClosure{Schema: 1, PlanID: p.PlanID, BatchID: b.BatchID, TargetCommit: b.TargetCommit, Request: r, Git: evidence, RoleStatuses: statuses, View: v, ClosedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		for _, id := range b.TaskIDs {
			if e = tx.Put(id, "reviews/batches/"+b.BatchID+"/disposition.json", reviewJSON(v)); e != nil {
				return e
			}
			if e = tx.Put(id, "reviews/batches/"+b.BatchID+"/closed.json", reviewJSON(c)); e != nil {
				return e
			}
		}
		if e = tx.PutGroup(reviewControlGroup, "dispositions/"+b.BatchID+".json", reviewJSON(v)); e != nil {
			return e
		}
		return tx.PutGroup(reviewControlGroup, closureName(b.BatchID), reviewJSON(c))
	})
	return
}
func verifyClosureCopies(tx *Transaction, c ReviewClosure) error {
	for _, id := range c.View.Batch.TaskIDs {
		text, err := tx.Read(id, "reviews/batches/"+c.BatchID+"/closed.json")
		if err != nil {
			return err
		}
		if text != reviewJSON(c) {
			return reviewError("partial or conflicting closure publication")
		}
		text, err = tx.Read(id, "reviews/batches/"+c.BatchID+"/disposition.json")
		if err != nil {
			return err
		}
		if text != reviewJSON(c.View) {
			return reviewError("disposition publication mismatch")
		}
	}
	return nil
}

func verifyClosureIntegrity(tx *Transaction, c ReviewClosure) error {
	var b ReviewBatch
	ok, err := readReviewJSON(tx, reviewBatchName(c.BatchID), &b)
	if err != nil {
		return err
	}
	if !ok || c.Schema != 1 || c.TargetCommit != b.TargetCommit || c.PlanID != b.PlanID || c.Git.Head != b.TargetCommit {
		return reviewError("closed batch target mismatch")
	}
	p, err := batchPlan(tx, b)
	if err != nil {
		return err
	}
	if noGitReviewBatch(b.Base, b.TargetCommit, b.Requirements) != (strings.TrimSpace(c.Git.NotApplicable) != "") {
		return reviewError("Git N/A evidence binding")
	}
	if c.Git.CWD != p.CWD {
		return reviewError("closed Git CWD mismatch")
	}
	v, err := aggregateReviewBatch(tx, b)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(v, c.View) {
		return reviewError("closed view mismatch")
	}
	edges, statuses, err := ReviewClosureEdges(v, c.Request)
	if err != nil {
		return err
	}
	if err = verifyMechanicalEvidence(v, c.Request, c.Git); err != nil {
		return err
	}
	if !reflect.DeepEqual(edges, c.Git.Edges) || !reflect.DeepEqual(statuses, c.RoleStatuses) {
		return reviewError("closed evidence mismatch")
	}
	if _, err = time.Parse(time.RFC3339Nano, c.Git.VerifiedAt); err != nil {
		return reviewError("closed Git timestamp")
	}
	if _, err = time.Parse(time.RFC3339Nano, c.ClosedAt); err != nil {
		return reviewError("closed timestamp")
	}
	return verifyClosureCopies(tx, c)
}
