package review

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/fs"
)

// processEvidenceMaxMembers caps the member records of one run. Leftover members sort first, so the cap
// never hides the records that explain a rejection.
const processEvidenceMaxMembers = 64

const (
	memberLeftover     = "leftover"
	memberExited       = "exited"
	memberAttributed   = "attributed"
	memberNotRechecked = "not_rechecked"
	memberUnknown      = "unknown"

	sigkillPending = "pending"
	sigkillSent    = "sent"
	sigkillNotSent = "not_sent"
	sigkillFailed  = "failed"
)

// processCollection is the result of collecting a reviewer process tree. evidence is nil when no group member
// was observed, and always on Windows, where the job object reports only a count.
type processCollection struct {
	lingering bool
	evidence  *processEvidence
}

// processEvidence holds process identity only. It never carries argv or a path, because both can hold
// credentials or private locations.
type processEvidence struct {
	Schema         int                `json:"schema"`
	LeaderPID      int                `json:"leader_pid"`
	Recheck        processRecheck     `json:"recheck"`
	Observation    processObservation `json:"observation"`
	Members        []processMember    `json:"members"`
	OmittedMembers int                `json:"omitted_members"`
	Cleanup        processCleanup     `json:"cleanup"`
}

type processRecheck struct {
	Performed  bool  `json:"performed"`
	DurationMS int64 `json:"duration_ms"`
}

type processObservation struct {
	Failed bool   `json:"failed"`
	Reason string `json:"reason,omitempty"`
}

type processMember struct {
	PID        int    `json:"pid"`
	PPID       int    `json:"ppid"`
	PGID       int    `json:"pgid"`
	StartTime  string `json:"start_time"`
	Executable string `json:"executable"`
	ObservedAt string `json:"observed_at"`
	Outcome    string `json:"outcome"`
}

type processCleanup struct {
	Sigkill   string `json:"sigkill"`
	Collected bool   `json:"collected"`
	Error     string `json:"error,omitempty"`
}

// retainProcessEvidence stores the evidence with the run, replacing an earlier version. It prints the evidence
// when no run exists or the write fails, so the record survives either way.
func retainProcessEvidence(ctx reviewContext, evidence *processEvidence) error {
	var stored error
	if ctx.archive != nil {
		data, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			return err
		}
		path := filepath.Join(ctx.archive.staging, board.ReviewProcessEvidence)
		stored = fs.WriteTextAtomic(ctx.archive.staging, path, string(data)+"\n", true)
		if stored == nil {
			ctx.archive.processEvidence = true
			return nil
		}
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, config.Text("review.process_evidence", string(data)))
	return stored
}

// processEvidenceNote names the stored evidence of a run, relative to its card.
func processEvidenceNote(runID string) string {
	return config.Text("review.process_evidence", "reviews/"+runID+"/"+board.ReviewProcessEvidence)
}

// processRejection is the failure of a collection that rejects a reviewer result.
func processRejection(name string, collection processCollection) *gateError {
	key := "review.review_left_background_child_processes_the_result_was_rejected"
	if collection.evidence != nil && collection.evidence.Observation.Failed {
		key = "review.process_observation_failed_the_result_was_rejected"
	}
	return newGate(2, key, name)
}
