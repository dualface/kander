package review

import (
	"encoding/json"
	"errors"
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

// retainProcessEvidence stores the evidence with the run. It prints the evidence when no run exists or the
// write fails, so the record survives either way. It returns the sentence that names the stored location, which
// is empty when nothing was stored.
func retainProcessEvidence(ctx reviewContext, evidence *processEvidence) (string, error) {
	if evidence == nil {
		return "", nil
	}
	var stored error
	if ctx.archive != nil {
		data, err := json.MarshalIndent(evidence, "", "  ")
		if err != nil {
			return "", err
		}
		path := filepath.Join(ctx.archive.staging, board.ReviewProcessEvidence)
		stored = fs.WriteTextAtomic(ctx.archive.staging, path, string(data)+"\n", false)
		if stored == nil {
			return config.Text("review.process_evidence", "reviews/"+ctx.archive.run.RunID+"/"+board.ReviewProcessEvidence), nil
		}
	}
	data, err := json.Marshal(evidence)
	if err != nil {
		return "", err
	}
	fmt.Fprintln(os.Stderr, config.Text("review.process_evidence", string(data)))
	return "", stored
}

// withProcessNote appends the evidence location to a process failure.
func withProcessNote(message, note string) string {
	if note == "" {
		return message
	}
	return message + "; " + note
}

// processRejection is the failure message of a collection that rejects a reviewer result.
func processRejection(name, note string, collection processCollection) *gateError {
	key := "review.review_left_background_child_processes_the_result_was_rejected"
	if collection.evidence != nil && collection.evidence.Observation.Failed {
		key = "review.process_observation_failed_the_result_was_rejected"
	}
	return newGateMsg(2, withProcessNote(config.Text(key, name), note))
}

// processStopFailure adds the evidence location to a failed collection.
func processStopFailure(err error, note string) error {
	var ge *gateError
	if errors.As(err, &ge) {
		return newGateMsg(ge.code, withProcessNote(ge.message, note))
	}
	return err
}
