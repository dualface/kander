package api

import "github.com/dualface/kander/v2/internal/board"

const HeaderReviewWriter = "Kander-Review-Writer"

// ReviewPrepare freezes reviewer inputs and a hash of the private upload
// credential. Holder confirmations cover this material, never its secret.
//
// A request that creates its batch carries PolicyRevision, the project policy
// revision the client works with, and RequirementDecisions, the decision
// record that justifies each role the policy requires but Requirements marks
// N/A. PolicyRevision is not part of the run identity, so a retry of the same
// run after a policy change still matches it.
type ReviewPrepare struct {
	Input                board.ReviewInput    `json:"input"`
	Requirements         map[string]string    `json:"requirements"`
	RequirementDecisions map[string]string    `json:"requirement_decisions,omitempty"`
	PolicyRevision       uint64               `json:"policy_revision,omitzero"`
	Advance              *board.ReviewAdvance `json:"advance,omitempty"`
	Originals            map[string][]byte    `json:"originals"`
	Version              string               `json:"version"`
	WriterHash           string               `json:"writer_hash"`
}

// ReviewRunState is GET .../reviews/runs/{run}: the stored run and, when a
// compaction removed its large evidence after it settled, compacted set to
// true. A server before compaction never sends the field, which reads as not
// compacted. CompactionError names a compaction receipt that cannot be
// validated; the run is still returned, and its compaction state is unknown.
type ReviewRunState struct {
	board.ReviewRun
	Compacted       bool   `json:"compacted,omitempty"`
	CompactionError string `json:"compaction_error,omitempty"`
}

type ReviewArtifact struct {
	Name   string `json:"name"`
	Data   []byte `json:"data"`
	Digest string `json:"digest"`
}

type ReviewFinalize struct {
	Run    board.ReviewRun  `json:"run"`
	Report []byte           `json:"report"`
	Files  []ReviewArtifact `json:"files"`
}
