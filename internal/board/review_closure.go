package board

import (
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/dualface/kander/internal/fs"
	"github.com/dualface/kander/internal/statemachine/reviewobligation"
	"github.com/dualface/kander/internal/statemachine/reviewplan"
)

type ReviewRunView struct {
	FindingsStatus string                `json:"findings_status,omitempty"`
	Interpretation *ReviewInterpretation `json:"interpretation,omitempty"`
	Run            ReviewRun             `json:"run"`
	Findings       *ReviewFindings       `json:"findings,omitempty"`
	Assignment     *ReviewAssignment     `json:"assignment,omitempty"`
	Records        []ReviewDisposition   `json:"records"`
	// Withdrawn repeats the records whose finding was withdrawn with its member.
	// omitempty keeps views written before the field byte-identical on re-encode.
	Withdrawn []ReviewDisposition `json:"withdrawn,omitempty"`
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
	SupersededRuns   map[string]ReviewSupersededRun  `json:"superseded_runs,omitempty"`
	Opinions         []ReviewOpinion                 `json:"opinions"`
}

// ReviewSupersededRun retires a parallel duplicate of a conclusion-chain run. With
// a user decision reference it also retires a root run that cannot join the chain,
// and the chain that grew from it. Findings and failures of a retired run stay
// binding, and the entry stays in the closure. omitempty keeps closures written
// before the field byte-identical on re-encode.
type ReviewSupersededRun struct {
	Replacement string `json:"replacement"`
	Basis       string `json:"basis"`
	Decision    string `json:"decision,omitempty"`
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

// aggregateReviewBatch builds the view of b from its originals. The plan kernel
// accepts the view only when every run's publication verifies, every finding
// lineage holds, and every disposition ledger reads; the first that failed names
// the refusal. The view reads no plan state, so the batch is projected alone.
func aggregateReviewBatch(tx *Transaction, b ReviewBatch) (ReviewBatchView, error) {
	view := ReviewBatchView{Schema: 1, Batch: b, Runs: []ReviewRunView{}}
	runs, err := batchRuns(tx, b)
	if err != nil {
		return view, err
	}
	f := reviewplan.Aggregate{Published: true, Lineage: true, Ledger: true}
	refusal := func() error {
		for _, run := range runs {
			if err := verifyPublishedReview(tx, run); err != nil {
				f.Published, f.Lineage, f.Ledger = false, false, false
				return err
			}
			rv := ReviewRunView{Run: run, Records: []ReviewDisposition{}}
			if run.ExecutionStatus == "ok" {
				findings, err := runFindings(tx, run)
				if errors.Is(err, errInterpretationPending) {
					rv.FindingsStatus = "pending"
					view.Runs = append(view.Runs, rv)
					continue
				}
				if err == nil {
					err = validateFindingLineage(tx, run, findings)
				}
				if err != nil {
					f.Lineage, f.Ledger = false, false
					return err
				}
				ledger, err := readDispositionLedger(tx, run, true)
				if err != nil {
					f.Ledger = false
					return err
				}
				if run.FindingsSchema == FindingsSchemaReceiver {
					rv.FindingsStatus = "ready"
					var interpretation ReviewInterpretation
					exists, err := readReviewJSON(tx, interpretationName(run.RunID), &interpretation)
					if err != nil {
						f.Ledger = false
						return err
					}
					if exists {
						rv.Interpretation = &interpretation
					}
				}
				rv.Findings = &findings
				rv.Assignment = &ledger.Assignment
				rv.Records = ledger.Records
				rv.Withdrawn = withdrawnRecords(ledger)
			}
			view.Runs = append(view.Runs, rv)
		}
		return nil
	}()
	if !batchViewKernel().step(f) {
		return view, firstError(refusal, reviewError("batch view refused"))
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
func PublishReviewDispositionWithTransaction(root, id string, extensions ...TransactionExtension) (v ReviewBatchView, err error) {
	b, err := ReadReviewBatch(root, id)
	if err != nil {
		return v, err
	}
	err = WithTransaction(root, reviewScope(b.TaskIDs, false), func(tx *Transaction) error {
		defer func() { tx.SetOperationResult(v) }()
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
		targets, e := reviewCopyTargets(tx, b.TaskIDs)
		if e != nil {
			return e
		}
		for _, task := range targets {
			if e = tx.Put(task, "reviews/batches/"+id+"/disposition.json", reviewJSON(v)); e != nil {
				return e
			}
		}
		return tx.PutGroup(reviewControlGroup, "dispositions/"+id+".json", reviewJSON(v))
	}, extensions...)
	return
}

// supersededReplacement admits a successful run outside the conclusion chain
// as a parallel duplicate: a chain run with the same commit and reviewer shares
// its predecessor, or its predecessor is itself superseded and it matches the
// conclusion's commit and reviewer. Anything else needs a user decision
// reference, and then only a root run or a run on a retired chain qualifies:
// without one, a full re-run could bypass the incremental fix round or switch
// reviewers mid-batch.
func supersededReplacement(runs map[string]ReviewRunView, chain map[string]bool, conclusion, run ReviewRun, superseded map[string]ReviewSupersededRun) (string, error) {
	s, ok := superseded[run.RunID]
	if !ok {
		return "", reviewError("unconnected successful role run: " + t("board.review_unlinked_run_hint", run.RunID, run.Role, conclusion.RunID))
	}
	if strings.TrimSpace(s.Basis) == "" {
		return "", reviewError("superseded_runs basis required: " + run.RunID)
	}
	if s.Decision != "" && strings.TrimSpace(s.Decision) == "" {
		return "", reviewError("superseded_runs decision is blank: " + run.RunID)
	}
	replacement, ok := runs[s.Replacement]
	if !ok || !chain[s.Replacement] || replacement.Run.ExecutionStatus != "ok" {
		return "", reviewError("superseded_runs replacement must be a successful run on the conclusion chain of " + conclusion.RunID + ": " + run.RunID)
	}
	for id := range chain {
		twin := runs[id].Run
		if twin.Commit == run.Commit && twin.Reviewer == run.Reviewer && twin.PreviousRunID == run.PreviousRunID {
			return replacement.Run.Commit, nil
		}
	}
	if _, ok := superseded[run.PreviousRunID]; ok && run.Commit == conclusion.Commit && run.Reviewer == conclusion.Reviewer {
		return replacement.Run.Commit, nil
	}
	if s.Decision != "" && (run.PreviousRunID == "" || superseded[run.PreviousRunID].Decision != "") {
		return replacement.Run.Commit, nil
	}
	return "", reviewError("superseded_runs entry has no parallel twin (same commit, reviewer and predecessor) on the conclusion chain: " + run.RunID + "; " + t("board.review_superseded_decision_hint"))
}
func latestViewRecords(rv ReviewRunView) []ReviewDisposition {
	return latestDispositions(dispositionLedger{Records: rv.Records})
}

// ReviewClosureEdges validates semantic coverage and returns the exact Git claims
// which the review layer must verify. Structural validation makes no Git calls.
func ReviewClosureEdges(v ReviewBatchView, r ReviewCloseRequest) ([]ReviewGitEdge, map[string]string, error) {
	b := v.Batch
	for _, run := range v.Runs {
		if run.FindingsStatus == "pending" {
			return nil, nil, reviewError("report needs review interpret: " + run.Run.RunID)
		}
	}
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
	superseded := map[string]bool{}
	for _, role := range reviewRequirementRoles(b.Requirements) {
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
		if !ok || !found || rv.Run.Role != role || rv.Run.ExecutionStatus != "ok" || strings.TrimSpace(c.Basis) == "" {
			return nil, nil, reviewError("required successful role conclusion: " + role)
		}
		if !validCommit(c.PassedAt) {
			return nil, nil, reviewError("role " + role + " passed_at must be a 40 or 64 character lowercase hex commit, not a timestamp")
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
				replacement, e := supersededReplacement(runs, chain, rv.Run, other.Run, r.SupersededRuns)
				if e != nil {
					return nil, nil, e
				}
				superseded[other.Run.RunID] = true
				add(other.Run.Commit, replacement)
				continue
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
						return nil, nil, reviewError("same-run must-fix " + d.FindingID + " needs disposition.mechanical or a new review run")
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
	for id := range r.SupersededRuns {
		if !superseded[id] {
			return nil, nil, reviewError("superseded_runs entry is not a successful run outside its role's conclusion chain: " + id)
		}
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
func CloseReviewBatchWithTransaction(root string, r ReviewCloseRequest, evidence ReviewGitEvidence, extensions ...TransactionExtension) (c ReviewClosure, err error) {
	b, err := ReadReviewBatch(root, r.BatchID)
	if err != nil {
		return c, err
	}
	var plan ReviewPlan
	err = WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error { var e error; plan, e = batchPlan(tx, b); return e })
	if err != nil {
		return c, err
	}
	err = WithTransaction(root, reviewLedgerScope(plan.TaskIDs, false), func(tx *Transaction) error {
		defer func() { tx.SetOperationResult(c) }()
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
		at := slices.IndexFunc(p.Batches, func(pb ReviewPlanBatch) bool { return pb.BatchID == b.BatchID })
		pk, e := projectPlan(tx, p, kernelBound(p))
		if e != nil {
			return e
		}
		f := reviewplan.Close{At: uint64(at)}
		var refusal error
		hold := func(fact *bool, err error) {
			if refusal == nil {
				*fact, refusal = err == nil, err
			}
		}
		hold(&f.Bound, verifyPlanCopies(tx, p))
		if refusal == nil {
			hold(&f.PreviousClosed, validatePreviousClosure(tx, b))
		}
		var v ReviewBatchView
		if refusal == nil {
			// The view is its own kernel event; a refused view refuses the close.
			if v, e = aggregateReviewBatch(tx, b); e != nil {
				return e
			}
			f.Revision = r.BatchID == b.BatchID && r.ExpectedRevision == b.Revision
			f.ViewHash = r.ViewHash == ReviewViewDigest(v)
		}
		var edges []ReviewGitEdge
		var statuses map[string]string
		if refusal == nil {
			edges, statuses, e = ReviewClosureEdges(v, r)
			hold(&f.Conclusions, e)
		}
		if refusal == nil {
			hold(&f.Git, closeGitEvidence(v, b, p, r, evidence, edges))
		}
		existing := false
		if refusal == nil {
			if existing, e = readReviewJSON(tx, closureName(b.BatchID), &c); e != nil {
				return e
			}
		}
		if existing {
			if !reflect.DeepEqual(c.Request, r) {
				return reviewError("immutable closure")
			}
			return verifyClosureCopies(tx, c)
		}
		if refusal == nil {
			hold(&f.Current, requireCurrentConclusions(tx, b, r))
		}
		if !pk.step(f) {
			return firstError(refusal, reviewError("batch close refused"))
		}
		c = ReviewClosure{Schema: 1, PlanID: p.PlanID, BatchID: b.BatchID, TargetCommit: b.TargetCommit, Request: r, Git: evidence, RoleStatuses: statuses, View: v, ClosedAt: time.Now().UTC().Format(time.RFC3339Nano)}
		if e = closeMemberKernels(tx, p, at, c); e != nil {
			return e
		}
		targets, e := reviewCopyTargets(tx, b.TaskIDs)
		if e != nil {
			return e
		}
		for _, id := range targets {
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
	}, extensions...)
	return
}

// closeGitEvidence checks the Git evidence of a close: the N/A binding, the
// mechanical fixes, the checkout, head and edges, and the verification time.
func closeGitEvidence(v ReviewBatchView, b ReviewBatch, p ReviewPlan, r ReviewCloseRequest, evidence ReviewGitEvidence, edges []ReviewGitEdge) error {
	if noGitReviewBatch(b.Base, b.TargetCommit, b.Requirements) != (strings.TrimSpace(evidence.NotApplicable) != "") {
		return reviewError("Git N/A evidence binding")
	}
	if err := verifyMechanicalEvidence(v, r, evidence); err != nil {
		return err
	}
	if !SameReviewCWD(evidence.CWD, p.CWD) || evidence.Head != b.TargetCommit || !reflect.DeepEqual(edges, evidence.Edges) {
		return reviewError("Git evidence binding")
	}
	if _, err := time.Parse(time.RFC3339Nano, evidence.VerifiedAt); err != nil {
		return reviewError("Git verification timestamp")
	}
	return nil
}

// closeMemberKernels runs the close of the batch at position at through the
// kernel of every member that stayed in the plan, with the roles whose
// conclusion run reviewed the member's current cycle.
func closeMemberKernels(tx *Transaction, p ReviewPlan, at int, c ReviewClosure) error {
	for _, id := range p.TaskIDs {
		excluded, err := memberExcluded(tx, id)
		if err != nil {
			return err
		}
		if excluded {
			continue
		}
		s, err := tx.Snapshot(id)
		if err != nil {
			return err
		}
		k, err := loadMemberKernel(tx, p, true, id, kernelBound(p))
		if err != nil {
			return err
		}
		current, err := currentRoles(p.Batches[at], c, true, id, planCycle(s))
		if err != nil {
			return err
		}
		if !k.step(reviewobligation.Close{At: uint64(at), Current: current}) {
			return errKernelState(id)
		}
	}
	return nil
}

func verifyClosureCopies(tx *Transaction, c ReviewClosure) error {
	// A member that left after the closure keeps its copies, and one that left
	// before it never took them. Neither is damage.
	targets, err := reviewCopyTargets(tx, c.View.Batch.TaskIDs)
	if err != nil {
		return err
	}
	for _, id := range targets {
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
	if !SameReviewCWD(c.Git.CWD, p.CWD) {
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
