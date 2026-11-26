package launch

import (
	"time"
	"unicode/utf8"

	"github.com/dualface/kander/v2/internal/issue"
	"github.com/dualface/kander/v2/internal/probe"
)

const (
	// unresolvedPaneStatusTimeout bounds the one pane read of an unresolved
	// start. It stays below probe.DefaultCommandTimeout: the read only adds a
	// diagnostic and must not hold up the started-unresolved result.
	unresolvedPaneStatusTimeout = 3 * time.Second
	// unresolvedPaneStatusMaxBytes bounds the terminal-reported status value
	// quoted in the reason; the value is untrusted terminal data.
	unresolvedPaneStatusMaxBytes = 32
	// unresolvedReasonMaxBytes matches the bound issue.Sanitize applies, so a
	// reason with the suffix is no longer than one without it.
	unresolvedReasonMaxBytes = 300
	unresolvedReasonEllipsis = "..."
)

// unresolvedPaneStatus reads, once and within a bound, the agent status a
// program status terminal reports for pane and renders it as a suffix of the
// unresolved reason. A blocked status adds a hint that the agent may wait at
// a confirmation dialog. Every failure to read a status returns "", leaving
// the reason as it was.
func unresolvedPaneStatus(plan LaunchPlan, pane string) string {
	if pane == "" || !plan.capabilities().ProgramStatus {
		return ""
	}
	ctx, cancel := probe.TimeoutContext(unresolvedPaneStatusTimeout)
	defer cancel()
	facts, err := plan.backend().PaneFacts(ctx, probeConn(plan), pane)
	if err != nil || facts.Gone {
		return ""
	}
	status := truncateUTF8(issue.Sanitize(facts.AgentStatus), unresolvedPaneStatusMaxBytes)
	if status == "" {
		return ""
	}
	suffix := t("launch.unresolved_pane_status", status)
	if status == "blocked" {
		suffix += t("launch.unresolved_pane_blocked_hint")
	}
	return suffix
}

// unresolvedReason sanitizes the reason of an unresolved start and appends
// suffix, shortening the reason instead of the suffix when both together
// exceed the sanitized length bound.
func unresolvedReason(reason, suffix string) string {
	reason = issue.Sanitize(reason)
	if suffix == "" {
		return reason
	}
	if len(reason)+len(suffix) > unresolvedReasonMaxBytes+len(unresolvedReasonEllipsis) {
		reason = truncateUTF8(reason, max(unresolvedReasonMaxBytes-len(suffix), 0)) + unresolvedReasonEllipsis
	}
	return reason + suffix
}

// truncateUTF8 cuts text to at most limit bytes without splitting a rune.
func truncateUTF8(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut]
}
