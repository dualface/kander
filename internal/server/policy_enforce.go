package server

import (
	"slices"
	"strconv"
	"strings"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

// This file enforces the project policy on the writes that depend on it: a
// request that creates a review batch is checked against the review stages of
// the policy, and the first claim of a card or of a task group fixes the
// branch model the card or group delivers to. Both carry the policy revision
// the client worked with, and a set policy refuses a stale one.

// requireCurrentPolicy refuses a request whose policy revision is not the
// current one while the project has a policy.
func (p *project) requireCurrentPolicy(revision uint64) error {
	current := p.policyView()
	if current.Revision == 0 || revision == current.Revision {
		return nil
	}
	return apiError(api.CodePolicyRevisionStale, "the project policy is at revision "+strconv.FormatUint(current.Revision, 10)+", the request was made with revision "+strconv.FormatUint(revision, 10)+"; read the policy again and retry")
}

// currentPolicyValues decodes the values of the current policy. ok is false
// while the project has none.
func (p *project) currentPolicyValues() (api.ProjectPolicy, config.PolicyValues, bool, error) {
	current := p.policyView()
	if current.Revision == 0 {
		return current, config.PolicyValues{}, false, nil
	}
	values, _, err := config.ReadPolicyValues(current.Values)
	if err != nil {
		return current, config.PolicyValues{}, false, apiError(api.CodeInternal, "the project policy cannot be read: "+err.Error())
	}
	return current, values, true, nil
}

// validReportedBranchMode refuses a branch mode a client reported that is not
// a git.branch_mode value. An empty one passes here; only a claim that
// creates a snapshot needs it.
func validReportedBranchMode(mode string) error {
	if mode == "" || slices.Contains(config.GitBranchModes, mode) {
		return nil
	}
	return apiError(api.CodeInvalidRequest, "invalid branch_mode "+strconv.Quote(mode))
}

// newBranchSnapshot is the snapshot a first claim records: the branch mode of
// the policy and its revision, or, while the project has no policy, the branch
// mode the client reported with revision 0.
func (p *project) newBranchSnapshot(scope, reported string) (*api.BranchSnapshot, error) {
	current, values, set, err := p.currentPolicyValues()
	if err != nil {
		return nil, err
	}
	if set {
		return &api.BranchSnapshot{BranchMode: values.Git.BranchMode, PolicyRevision: current.Revision, Scope: scope}, nil
	}
	if !slices.Contains(config.GitBranchModes, reported) {
		return nil, apiError(api.CodeInvalidRequest, "a first claim of a project without a policy needs the branch_mode of the client")
	}
	return &api.BranchSnapshot{BranchMode: reported, PolicyRevision: 0, Scope: scope}, nil
}

// checkSnapshotClaim is the check of a claim request that runs before its
// receipt is written. Only a claim that will create a snapshot depends on the
// policy: it needs a valid branch mode, also when the policy supplies the
// snapshot's, and a current policy revision.
func (p *project) checkSnapshotClaim(creates bool, revision uint64, branchMode string) error {
	if err := validReportedBranchMode(branchMode); err != nil {
		return err
	}
	if !creates {
		return nil
	}
	if branchMode == "" {
		return apiError(api.CodeInvalidRequest, "a first claim needs the branch_mode of the client")
	}
	return p.requireCurrentPolicy(revision)
}

// taskClaimCreatesSnapshot reports whether a claim of task id creates its
// branch snapshot: the card is no task group member and has no claim record,
// so this is its first claim. A card that cannot be read answers false; the
// claim itself reports that.
func (p *project) taskClaimCreatesSnapshot(id string) bool {
	s, err := board.ReadSnapshot(p.board, id)
	if err != nil || board.TaskGroupFrom(s.Text) != "" {
		return false
	}
	_, found, err := p.readClaim(s.Entry.TaskID)
	return err == nil && !found
}

// groupClaimCreatesSnapshot reports whether a group claim creates the branch
// snapshot of the group: the group has no group claim record yet.
func (p *project) groupClaimCreatesSnapshot(group string) bool {
	_, found, err := p.readGroupClaim(group)
	return err == nil && !found
}

// effectiveSnapshot is the branch snapshot a card delivers under: its own,
// or for a task group member that of its group. nil when neither has one.
func (p *project) effectiveSnapshot(own *api.BranchSnapshot, text string) *api.BranchSnapshot {
	if own != nil {
		return own
	}
	group := board.TaskGroupFrom(text)
	if group == "" || !board.ValidTaskGroup(group) {
		return nil
	}
	claim, found, err := p.readGroupClaim(group)
	if err != nil || !found {
		return nil
	}
	return claim.BranchSnapshot
}

// claimView is the public claim of a card with its effective snapshot.
func (p *project) claimView(r claimRecord, text string) *api.ClaimView {
	view := r.view(p.server.now())
	view.BranchSnapshot = p.effectiveSnapshot(view.BranchSnapshot, text)
	return view
}

// reviewRoles are the roles a new batch requires a decision for.
var reviewRoles = []string{"PMQA", "Security"}

// newReviewBatch is one batch a request creates, as the policy check needs it.
type newReviewBatch struct {
	id           string
	taskIDs      []string
	requirements map[string]string
	decisions    map[string]string
}

// checkNewReviewBatches checks the batches a request creates against the
// project policy and returns the attestation each one records. With none it
// checks nothing. A set policy refuses a stale revision. With review on, a
// role the policy requires for the batch scale and the request marks N/A
// needs a decision record of a batch member; any other combination passes.
// The decisions recorded are those such a role used.
func (p *project) checkNewReviewBatches(revision uint64, batches []newReviewBatch) (board.BatchAttestations, error) {
	if len(batches) == 0 {
		return nil, nil
	}
	if err := p.requireCurrentPolicy(revision); err != nil {
		return nil, err
	}
	current, values, set, err := p.currentPolicyValues()
	if err != nil {
		return nil, err
	}
	attest := board.BatchAttestations{}
	var unmet []string
	for _, b := range batches {
		value := board.BatchAttestation{PolicyRevision: current.Revision}
		if set && values.Rules[config.RuleReview] {
			used, problems, err := p.checkBatchRoles(b, values.ReviewStages)
			if err != nil {
				return nil, err
			}
			value.RequirementDecisions = used
			unmet = append(unmet, problems...)
		}
		attest[b.id] = value
	}
	if len(unmet) > 0 {
		return nil, apiError(api.CodePolicyRequirementUnmet, strings.Join(unmet, "; "))
	}
	return attest, nil
}

// checkBatchRoles checks the roles of one batch. It returns the decisions the
// batch uses and a description of each role that fails.
func (p *project) checkBatchRoles(b newReviewBatch, stages map[string]map[string]string) (map[string]string, []string, error) {
	scale, known, err := p.batchScaleAndDecisions(b.taskIDs)
	if err != nil {
		return nil, nil, err
	}
	var used map[string]string
	var problems []string
	for _, role := range reviewRoles {
		requirement, ok := b.requirements[role]
		if !ok || stages[scale][role] != "required" || !strings.HasPrefix(requirement, "N/A") {
			continue
		}
		decision := b.decisions[role]
		switch {
		case decision == "":
			problems = append(problems, "batch "+b.id+": the project policy requires "+role+" for "+scale+" tasks; marking it N/A needs a decision record of a batch member")
		case !known[decision]:
			problems = append(problems, "batch "+b.id+": decision record "+strconv.Quote(decision)+" for "+role+" does not exist on any batch member")
		default:
			if used == nil {
				used = map[string]string{}
			}
			used[role] = decision
		}
	}
	return used, problems, nil
}

// batchScaleAndDecisions is the review scale of a batch, large when any member
// is large, and the IDs of the decision records of its members.
func (p *project) batchScaleAndDecisions(ids []string) (string, map[string]bool, error) {
	scale := "small"
	known := map[string]bool{}
	for _, id := range ids {
		s, err := board.ReadSnapshot(p.board, id)
		if err != nil {
			return "", nil, err
		}
		size, err := board.TaskSize(s.Entry, s.Text)
		if err != nil {
			return "", nil, err
		}
		if size == "large" {
			scale = "large"
		}
		records, err := board.Decisions(p.board, s.Entry.TaskID)
		if err != nil {
			return "", nil, err
		}
		for _, record := range records {
			known[record.ID] = true
		}
	}
	return scale, known, nil
}

// reviewBatchExists reports whether a batch record already exists.
func (p *project) reviewBatchExists(id string) (bool, error) {
	_, found, err := board.LookupReviewBatch(p.board, id)
	return found, err
}

// planNewBatches lists the batches of a plan request that do not exist yet,
// in plan order.
func (p *project) planNewBatches(batches []board.ReviewPlanBatch) ([]newReviewBatch, error) {
	var created []newReviewBatch
	for _, b := range batches {
		exists, err := p.reviewBatchExists(b.BatchID)
		if err != nil {
			return nil, err
		}
		if !exists {
			created = append(created, newReviewBatch{id: b.BatchID, taskIDs: b.TaskIDs, requirements: b.Requirements, decisions: b.RequirementDecisions})
		}
	}
	return created, nil
}
