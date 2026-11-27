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
