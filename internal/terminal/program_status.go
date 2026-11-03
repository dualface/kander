package terminal

import (
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/dualface/kander/internal/config"
)

// programStatusError is the OSC 7501 state Kander reports as blocked: the
// program stopped on an error and waits for a person, like a blocked dialog.
const programStatusError = "error"

// programStatusFacts marks facts of a program status backend and maps the
// reported OSC 7501 state onto Kander's agent states. The other reported
// states (idle, working, blocked, done) are already Kander's words and pass
// through unchanged; an unknown word stays unclassified for the caller.
func programStatusFacts(facts PaneFacts, app string) PaneFacts {
	facts.ProgramStatus = true
	facts.AgentApp = app
	if facts.AgentStatus == programStatusError {
		facts.AgentStatus, facts.AgentStatusReason = "blocked", programStatusError
	}
	return facts
}

// AgentMatch is the verdict of comparing the agent a pane reports with the
// agent a card expects.
type AgentMatch int

const (
	// AgentMatches: the pane runs the expected agent.
	AgentMatches AgentMatch = iota
	// AgentDiffers: the pane runs another agent, or none.
	AgentDiffers
	// AgentUndecided: a program status pane has not reported yet, or the
	// expected agent's declaration cannot be read. It is evidence of neither
	// a match nor another agent.
	AgentUndecided
)

// MatchAgent compares the pane's agent with the expected agent name. A program
// status pane is compared by the app the agent declares for OSC 7501; an
// agent without a reporting declaration never matches such a pane.
func (f PaneFacts) MatchAgent(agent string) AgentMatch {
	if !f.ProgramStatus {
		if f.Agent == agent {
			return AgentMatches
		}
		return AgentDiffers
	}
	if f.AgentApp == "" {
		return AgentUndecided
	}
	definition, err := config.LoadAgent(agent)
	if err != nil {
		return AgentUndecided
	}
	if app, ok := config.ProgramStatusApp(definition); ok && app == f.AgentApp {
		return AgentMatches
	}
	return AgentDiffers
}

// ReportedAgent names what the pane reports for diagnostics: the agent name,
// or the program status app.
func (f PaneFacts) ReportedAgent() string {
	if f.Agent != "" {
		return f.Agent
	}
	return f.AgentApp
}

// labelNameLimit leaves room for the random suffix inside the 32 characters
// a terminal name such as an hc terminal name allows.
const labelNameLimit = 23

// LabelName reduces a container label to a terminal name: the lowercase ASCII
// letters, digits and hyphens of the label, at most 23 characters, then a
// hyphen and eight random hexadecimal digits, so two launches with the same
// label (a takeover opens its new container before the old one closes) never
// share a name.
func LabelName(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		if b.Len() >= labelNameLimit {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == ' ':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		}
	}
	var suffix [4]byte
	// crypto/rand.Read never fails since Go 1.24.
	_, _ = rand.Read(suffix[:])
	name := strings.Trim(b.String(), "-")
	if name == "" {
		name = "kander"
	}
	return name + "-" + hex.EncodeToString(suffix[:])
}
