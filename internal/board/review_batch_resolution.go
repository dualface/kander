package board

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"time"
)

// ReviewBatchResolutionRequest is the extend-plan operation that accepts an
// unplanned batch: one closed planned batch covers its range, and every finding of
// its runs gets a result. The batch stays out of the chain and its evidence stays
// as it is.
type ReviewBatchResolutionRequest struct {
	BatchID         string                  `json:"batch_id"`
	CoveringBatchID string                  `json:"covering_batch_id"`
	Findings        []ReviewResolvedFinding `json:"findings"`
}

// ReviewResolvedFinding is the result of one finding of the unplanned batch.
type ReviewResolvedFinding struct {
	RunID        string `json:"run_id"`
	FindingID    string `json:"finding_id"`
	Status       string `json:"status"`
	Basis        string `json:"basis"`
	FixCommit    string `json:"fix_commit,omitempty"`
	Verification string `json:"verification,omitempty"`
	Decision     string `json:"decision,omitempty"`
}

// ReviewResolvedRun binds one run of the unplanned batch to its report.
type ReviewResolvedRun struct {
	RunID      string `json:"run_id"`
	ReportHash string `json:"report_hash"`
}

// ReviewResolutionGit is what the review layer verified. Board compares the edges
// with the ones it derives and runs no Git.
type ReviewResolutionGit struct {
	CWD        string          `json:"cwd"`
	VerifiedAt string          `json:"verified_at"`
	Edges      []ReviewGitEdge `json:"edges"`
}

// ReviewBatchResolution is the record the gate reads. Every field binds what the
// resolution looked at, so a later change to the batch fails the gate again.
type ReviewBatchResolution struct {
	Request       ReviewBatchResolutionRequest `json:"request"`
	Author        string                       `json:"author"`
	Basis         string                       `json:"basis"`
	BatchRevision uint64                       `json:"batch_revision"`
	Runs          []ReviewResolvedRun          `json:"runs"`
	Git           ReviewResolutionGit          `json:"git"`
	RecordedAt    string                       `json:"recorded_at"`
}

type batchResolutionFacts struct {
	batch ReviewBatch
	runs  []ReviewRun
	bound []ReviewResolvedRun
	edges []ReviewGitEdge
}

func validateResolutionProvenance(x ReviewPlanExtension) error {
	if x.ResolveBatch == nil || !ValidReviewID(x.PlanID) || x.ExpectedRevision == 0 || strings.TrimSpace(x.Author) == "" || strings.TrimSpace(x.Basis) == "" {
		return reviewError("plan extension provenance")
	}
	if x.Batch != nil || x.Seal || len(x.RebindCycles) > 0 || len(x.SyncTargets) > 0 || len(x.DropBatches) > 0 {
		return reviewError(t("board.review_resolve_exclusive"))
	}
	if !ValidReviewID(x.ResolveBatch.BatchID) || !ValidReviewID(x.ResolveBatch.CoveringBatchID) {
		return reviewError("resolve_batch batch IDs")
	}
	return nil
}

// resolutionScope locks every plan member and every member of the batch: the
// publication check reads the cards of a member outside the plan too.
func resolutionScope(root string, x ReviewPlanExtension) ([]string, error) {
	if err := validateResolutionProvenance(x); err != nil {
		return nil, err
	}
	var ids []string
	err := WithTransaction(root, reviewScope(nil, true), func(tx *Transaction) error {
		var p ReviewPlan
		ok, e := readReviewJSON(tx, planName(x.PlanID), &p)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError("missing plan")
		}
		var b ReviewBatch
		ok, e = readReviewJSON(tx, reviewBatchName(x.ResolveBatch.BatchID), &b)
		if e != nil {
			return e
		}
		if !ok {
			return reviewError(t("board.review_resolve_missing", x.ResolveBatch.BatchID))
		}
		ids = slices.Compact(slices.Sorted(slices.Values(slices.Concat(p.TaskIDs, b.TaskIDs))))
		return nil
	})
	return ids, err
}

// resolutionEntry validates one request against the plan. It holds everything the
// gate does not repeat: the CAS, the repeat refusal and the card publications.
func resolutionEntry(tx *Transaction, x ReviewPlanExtension) (ReviewPlan, batchResolutionFacts, error) {
	var p ReviewPlan
	var facts batchResolutionFacts
	ok, err := readReviewJSON(tx, planName(x.PlanID), &p)
	if err != nil {
		return p, facts, err
	}
	if !ok || p.Revision != x.ExpectedRevision {
		return p, facts, reviewError("plan extension CAS/sealed conflict")
	}
	// The cycle may be stale: review progress cannot name rebind_cycles while the
	// unplanned batch fails the gate, so the resolution has to come first.
	if err = verifyPlanCopiesFor(tx, p, false); err != nil {
		return p, facts, err
	}
	if resolvedBatch(p, x.ResolveBatch.BatchID) != nil {
		return p, facts, reviewError(t("board.review_resolve_repeat", x.ResolveBatch.BatchID, p.PlanID))
	}
	if facts, err = batchResolutionCheck(tx, p, *x.ResolveBatch); err != nil {
		return p, facts, err
	}
	for _, run := range facts.runs {
		if err = verifyPublishedReview(tx, run); err != nil {
			return p, facts, err
		}
	}
	return p, facts, nil
}

// ReviewBatchResolutionEdges validates a request and returns the Git claims the
// review layer must verify before it calls ResolveReviewBatch.
func ReviewBatchResolutionEdges(root string, x ReviewPlanExtension) (edges []ReviewGitEdge, err error) {
	ids, err := resolutionScope(root, x)
	if err != nil {
		return nil, err
	}
	err = WithTransaction(root, reviewScope(ids, true), func(tx *Transaction) error {
		_, facts, e := resolutionEntry(tx, x)
		edges = facts.edges
		return e
	})
	return edges, err
}

// ResolveReviewBatch records a resolution in the plan and in plan-history. The
// plan keeps its batches and its seal, and no run or batch record changes.
func ResolveReviewBatch(root string, x ReviewPlanExtension, git ReviewResolutionGit) error {
	ids, err := resolutionScope(root, x)
	if err != nil {
		return err
	}
	return WithTransaction(root, reviewScope(ids, false), func(tx *Transaction) error {
		p, facts, err := resolutionEntry(tx, x)
		if err != nil {
			return err
		}
		if !SameReviewCWD(git.CWD, p.CWD) || !reflect.DeepEqual(facts.edges, git.Edges) {
			return reviewError("Git evidence binding")
		}
		if _, err = time.Parse(time.RFC3339Nano, git.VerifiedAt); err != nil {
			return reviewError("Git verification timestamp")
		}
		previous := p
		now := time.Now().UTC().Format(time.RFC3339Nano)
		record := ReviewBatchResolution{Request: *x.ResolveBatch, Author: x.Author, Basis: x.Basis, BatchRevision: facts.batch.Revision, Runs: facts.bound, Git: git, RecordedAt: now}
		p.ResolvedBatches = append(slices.Clone(p.ResolvedBatches), record)
		p.Revision++
		if err = putPlanCopies(tx, p); err != nil {
			return err
		}
		if err = tx.PutGroup(reviewControlGroup, fmt.Sprintf("plan-history/%s/%d.json", p.PlanID, p.Revision), reviewJSON(struct {
			Previous   ReviewPlan            `json:"previous"`
			Request    ReviewPlanExtension   `json:"request"`
			Resolution ReviewBatchResolution `json:"resolution"`
			RecordedAt string                `json:"recorded_at"`
		}{previous, x, record, now})); err != nil {
			return err
		}
		return tx.PutGroup(reviewControlGroup, planName(p.PlanID), reviewJSON(p))
	})
}

func resolvedBatch(p ReviewPlan, id string) *ReviewBatchResolution {
	at := slices.IndexFunc(p.ResolvedBatches, func(r ReviewBatchResolution) bool { return r.Request.BatchID == id })
	if at < 0 {
		return nil
	}
	return &p.ResolvedBatches[at]
}

// verifyBatchResolution reruns every condition on the current records, so the gate
// and the entrance cannot disagree, and compares what the record bound.
func verifyBatchResolution(tx *Transaction, p ReviewPlan, r ReviewBatchResolution) error {
	batch := r.Request.BatchID
	stale := func(detail string) error { return reviewError(t("board.review_resolve_stale", batch, detail)) }
	facts, err := batchResolutionCheck(tx, p, r.Request)
	if err != nil {
		return stale(err.Error())
	}
	if facts.batch.Revision != r.BatchRevision {
		return stale(fmt.Sprintf("revision %d, resolved at %d", facts.batch.Revision, r.BatchRevision))
	}
	if !slices.Equal(facts.bound, r.Runs) {
		return stale("runs or report hashes")
	}
	if !reflect.DeepEqual(facts.edges, r.Git.Edges) || !SameReviewCWD(r.Git.CWD, p.CWD) {
		return stale("Git evidence")
	}
	if strings.TrimSpace(r.Author) == "" || strings.TrimSpace(r.Basis) == "" {
		return stale("author and basis")
	}
	if _, err = time.Parse(time.RFC3339Nano, r.Git.VerifiedAt); err != nil {
		return stale("Git timestamp")
	}
	if _, err = time.Parse(time.RFC3339Nano, r.RecordedAt); err != nil {
		return stale("recorded timestamp")
	}
	return nil
}

// batchResolutionCheck decides whether the covering batch can stand in for the
// unplanned batch. It reads control records and plan members only, because the gate
// holds no lock on a batch member outside the plan.
func batchResolutionCheck(tx *Transaction, p ReviewPlan, r ReviewBatchResolutionRequest) (facts batchResolutionFacts, err error) {
	if slices.ContainsFunc(p.Batches, func(pb ReviewPlanBatch) bool { return pb.BatchID == r.BatchID }) {
		return facts, reviewError(t("board.review_resolve_planned", r.BatchID, p.PlanID))
	}
	b := &facts.batch
	ok, err := readReviewJSON(tx, reviewBatchName(r.BatchID), b)
	if err != nil {
		return facts, err
	}
	if !ok {
		return facts, reviewError(t("board.review_resolve_missing", r.BatchID))
	}
	if err = validateRequirements(b.Requirements); err != nil {
		return facts, err
	}
	if facts.runs, err = batchRuns(tx, *b); err != nil {
		return facts, err
	}
	if len(facts.runs) == 0 {
		return facts, reviewError(t("board.review_resolve_no_run", r.BatchID))
	}
	closure, err := coveringClosure(tx, p, r.CoveringBatchID)
	if err != nil {
		return facts, err
	}
	cover := closure.View.Batch
	if err = requireCoveredMembers(tx, p, *b, cover); err != nil {
		return facts, err
	}
	for _, role := range reviewRequirementRoles(b.Requirements) {
		if b.Requirements[role] == "required" && cover.Requirements[role] != "required" {
			return facts, reviewError(t("board.review_resolve_role", role, r.BatchID, cover.BatchID))
		}
	}
	items, err := resolutionItems(tx, &facts)
	if err != nil {
		return facts, err
	}
	facts.edges = []ReviewGitEdge{{Ancestor: cover.Base, Descendant: b.Base}, {Ancestor: b.TargetCommit, Descendant: cover.TargetCommit}}
	seen := map[FindingRef]bool{}
	for _, f := range r.Findings {
		ref := FindingRef{RunID: f.RunID, FindingID: f.FindingID}
		item, ok := items[ref]
		if !ok {
			return facts, reviewError(t("board.review_resolve_finding_unknown", ref.key(), r.BatchID))
		}
		if seen[ref] {
			return facts, reviewError(t("board.review_resolve_finding_duplicate", ref.key()))
		}
		seen[ref] = true
		if err = validateResolvedFinding(f, ref, item.finding); err != nil {
			return facts, err
		}
		if f.Status != "fixed" {
			continue
		}
		if f.FixCommit == item.run.Commit {
			return facts, reviewError(t("board.review_resolve_fix_order", ref.key(), item.run.Commit))
		}
		conclusion, err := conclusionCommit(closure, item.run)
		if err != nil {
			return facts, err
		}
		facts.edges = append(facts.edges, ReviewGitEdge{Ancestor: item.run.Commit, Descendant: f.FixCommit}, ReviewGitEdge{Ancestor: f.FixCommit, Descendant: conclusion})
	}
	for ref := range items {
		if !seen[ref] {
			return facts, reviewError(t("board.review_resolve_finding_missing", ref.key()))
		}
	}
	slices.SortFunc(facts.edges, func(a, b ReviewGitEdge) int {
		return strings.Compare(a.Ancestor+"/"+a.Descendant, b.Ancestor+"/"+b.Descendant)
	})
	facts.edges = slices.Compact(facts.edges)
	for _, edge := range facts.edges {
		if !validCommit(edge.Ancestor) || !validCommit(edge.Descendant) {
			return facts, reviewError("full commit required")
		}
	}
	return facts, nil
}

func coveringClosure(tx *Transaction, p ReviewPlan, id string) (c ReviewClosure, err error) {
	if !slices.ContainsFunc(p.Batches, func(pb ReviewPlanBatch) bool { return pb.BatchID == id }) {
		return c, reviewError(t("board.review_resolve_cover_unknown", id, p.PlanID))
	}
	ok, err := readReviewJSON(tx, closureName(id), &c)
	if err != nil {
		return c, err
	}
	if !ok {
		return c, reviewError(t("board.review_resolve_cover_open", id))
	}
	return c, verifyClosureIntegrity(tx, c)
}

// requireCoveredMembers needs one plan member in the batch and the covering batch
// for every plan member that remains. A member of another plan answers there.
func requireCoveredMembers(tx *Transaction, p ReviewPlan, b, cover ReviewBatch) error {
	shared := false
	for _, id := range b.TaskIDs {
		if !containsID(p.TaskIDs, id) {
			continue
		}
		shared = true
		excluded, err := memberExcluded(tx, id)
		if err != nil {
			return err
		}
		if !excluded && !containsID(cover.TaskIDs, id) {
			return reviewError(t("board.review_resolve_member", id, b.BatchID, cover.BatchID))
		}
	}
	if !shared {
		return reviewError(t("board.review_resolve_no_member", b.BatchID, p.PlanID))
	}
	return nil
}

type resolutionItem struct {
	run     ReviewRun
	finding ReviewFinding
}

// resolutionItems binds the runs and collects their findings. A failed run has no
// finding, and the covering batch reviewed its range.
func resolutionItems(tx *Transaction, facts *batchResolutionFacts) (map[FindingRef]resolutionItem, error) {
	items := map[FindingRef]resolutionItem{}
	for _, run := range facts.runs {
		if run.Phase != "finalized" || !allPublished(run) {
			return nil, reviewError(t("board.review_resolve_publication", run.RunID))
		}
		facts.bound = append(facts.bound, ReviewResolvedRun{RunID: run.RunID, ReportHash: run.Hashes["report.md"]})
		if run.ExecutionStatus != "ok" {
			continue
		}
		findings, err := runFindings(tx, run)
		if errors.Is(err, errInterpretationPending) {
			return nil, reviewError(t("board.review_resolve_interpret", run.RunID))
		}
		if err != nil {
			return nil, err
		}
		for _, f := range findings.all() {
			items[FindingRef{RunID: run.RunID, FindingID: f.ID}] = resolutionItem{run: run, finding: f}
		}
	}
	return items, nil
}

// validateResolvedFinding keeps the rules of validateDisposition, with one addition:
// a must-fix finding that was not fixed needs a user decision, because this entrance
// has no author round and basis text alone proves nothing.
func validateResolvedFinding(f ReviewResolvedFinding, ref FindingRef, item ReviewFinding) error {
	id := ref.key()
	must := mustFix(item.Tier)
	if must && strings.TrimSpace(f.Decision) == "" && (f.Status != "fixed" || f.FixCommit == "") {
		return reviewError(t("board.review_resolve_must_fix", id))
	}
	if f.Status != "fixed" && f.Status != "rejected" && (f.Status != "deferred" || must) || strings.TrimSpace(f.Basis) == "" {
		return reviewError(t("board.review_resolve_status", id))
	}
	if f.Status == "fixed" && (!validCommit(f.FixCommit) || strings.TrimSpace(f.Verification) == "") {
		return reviewError(t("board.review_resolve_fixed", id))
	}
	if f.Status != "fixed" && (f.FixCommit != "" || f.Verification != "") {
		return reviewError(t("board.review_resolve_fix_evidence", id))
	}
	return nil
}

func conclusionCommit(c ReviewClosure, run ReviewRun) (string, error) {
	conclusion, ok := c.Request.Roles[run.Role]
	if ok {
		for _, rv := range c.View.Runs {
			if rv.Run.RunID == conclusion.RunID {
				return rv.Run.Commit, nil
			}
		}
	}
	return "", reviewError(t("board.review_resolve_role", run.Role, run.BatchID, c.BatchID))
}
