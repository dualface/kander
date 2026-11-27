package api

import (
	"encoding/json"
	"time"
)

// CodePolicyRevisionConflict refuses a project policy write whose expected
// revision is not the current one. Nothing was written.
const CodePolicyRevisionConflict = "policy_revision_conflict"

// ProjectPolicy is GET /api/v1/projects/{project}/policy, the answer of a
// policy write, and the policy field of a project. Revision 0 means the
// project has no policy yet: Epoch, Values, UpdatedAt and UpdatedBy are then
// absent. Every successful write adds 1 to Revision; the first write chooses a
// random Epoch that later writes keep. Values holds the five policy values as
// JSON; it stays raw so a client keeps the keys a newer server knows and this
// version does not.
type ProjectPolicy struct {
	Epoch     string          `json:"epoch,omitempty"`
	Revision  uint64          `json:"revision"`
	Values    json.RawMessage `json:"values,omitempty"`
	UpdatedAt time.Time       `json:"updated_at,omitzero"`
	UpdatedBy string          `json:"updated_by,omitempty"`
}

// PolicyWrite is PUT /api/v1/projects/{project}/policy. The write applies only
// while ExpectedRevision equals the current revision (0 initializes the
// policy). Values must hold all five policy values. Submitter is the name the
// client reports; an Attribution hook replaces it.
type PolicyWrite struct {
	ExpectedRevision uint64          `json:"expected_revision"`
	Values           json.RawMessage `json:"values"`
	Submitter        string          `json:"submitter,omitempty"`
}

// MarshalJSON keeps the answer of an unset policy to its revision and writes
// updated_by for every set policy, also when the submitter was empty.
func (p ProjectPolicy) MarshalJSON() ([]byte, error) {
	type plain ProjectPolicy
	if p.Revision == 0 {
		return json.Marshal(plain(p))
	}
	return json.Marshal(struct {
		plain
		UpdatedBy string `json:"updated_by"`
	}{plain(p), p.UpdatedBy})
}

// CodePolicyRevisionStale refuses a claim, a group claim, or a request that
// creates a review batch when the policy revision it carries is not the
// current one of a project whose policy is set. Nothing was written, not even
// an idempotency receipt: the client reads the policy again and resends the
// same key with the new revision.
const CodePolicyRevisionStale = "policy_revision_stale"

// CodePolicyRequirementUnmet refuses a new review batch that marks a role the
// project policy requires as N/A without a decision record of a batch member
// that justifies it. Nothing was written.
const CodePolicyRequirementUnmet = "policy_requirement_unmet"

// Scopes of a branch snapshot.
const (
	BranchSnapshotTask  = "task"
	BranchSnapshotGroup = "group"
)

// BranchSnapshot is the branch model the server fixed for a card at its first
// claim, or for a task group at its first group claim: the git.branch_mode of
// the project policy and its revision, or, while the project had no policy,
// the branch mode the claiming client reported with revision 0. Later claims
// of the same card or group keep it, so a change of the policy never moves
// the mainline branch of work in progress. Scope says whether it belongs to
// the card itself (task) or to its task group (group).
type BranchSnapshot struct {
	BranchMode     string `json:"branch_mode"`
	PolicyRevision uint64 `json:"policy_revision"`
	Scope          string `json:"scope"`
}
