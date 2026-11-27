package api

import "github.com/dualface/kander/v2/internal/board"

const HeaderReviewWriter = "Kander-Review-Writer"

// ReviewPrepare freezes reviewer inputs and a hash of the private upload
// credential. Holder confirmations cover this material, never its secret.
type ReviewPrepare struct {
	Input        board.ReviewInput    `json:"input"`
	Requirements map[string]string    `json:"requirements"`
	Advance      *board.ReviewAdvance `json:"advance,omitempty"`
	Originals    map[string][]byte    `json:"originals"`
	Version      string               `json:"version"`
	WriterHash   string               `json:"writer_hash"`
}

// ReviewRunState is GET .../reviews/runs/{run}: the stored run and, when a
// compaction removed its large evidence after it settled, compacted set to
// true. A server before compaction never sends the field, which reads as not
// compacted.
type ReviewRunState struct {
	board.ReviewRun
	Compacted bool `json:"compacted,omitempty"`
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
