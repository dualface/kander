package liveness

import (
	"context"
	"os/exec"
	"regexp"
	"time"

	"github.com/dualface/kander/internal/config"
	_ "github.com/dualface/kander/internal/terminal/builtin"
)

const (
	Alive   = "alive"
	Stopped = "stopped"
	Drifted = "drifted"
	Unknown = "unknown"

	// The results below only the process identity of a card without a pane gives.
	AliveOrphaned    = "alive-orphaned"
	IdentityMismatch = "identity-mismatch"
	Unprobeable      = "unprobeable"
)

var (
	sessionReferenceRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	taskGroupRe        = regexp.MustCompile(`^\d{8}-[a-z0-9]+(?:-[a-z0-9]+)*-group$`)

	lookPath = exec.LookPath
)

// TaskSession is the parsed session field of a card.
type TaskSession struct {
	Agent     string
	Reference string
}

// ExecutorEvidence says why an unknown observation still shows an executor: the
// probe finished and saw one, but could not prove its identity or its state. A
// probe that failed has none.
type ExecutorEvidence struct {
	// Window is the address the executor shows at; empty means the recorded WINDOW.
	Window string
	// Matches counts the panes of a reverse lookup that matched several.
	Matches int
	// Unmarked: the pane runs the agent but carries no session marker.
	Unmarked bool
}

// ReferenceResolver finds the session of a card that records none, within the
// deadline of the classification.
type ReferenceResolver func(context.Context) (string, error)

// Report is the read-only liveness classification of one card; it never writes to the card.
type Report struct {
	TaskID           string
	Agent            string
	Status           string
	Channel          string
	Container        string
	Detail           string
	NewWindow        string
	ObservedAt       time.Time           `json:"observed_at"`
	Identity         ObservationIdentity `json:"identity"`
	RuntimeState     string              `json:"runtime_state"`
	ObservationValid bool                `json:"observation_valid"`
	// Evidence separates an unknown that saw an executor from a failed probe.
	Evidence *ExecutorEvidence `json:"-"`
	// Reference is the session a drift was found by, when the card records none.
	Reference string `json:"-"`
}

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
