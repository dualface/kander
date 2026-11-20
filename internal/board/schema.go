package board

import (
	"regexp"
	"strings"
	"sync"
)

// The card schema tokens. Cards use these English names only; the Chinese names
// of cards written before the rename are converted when such a card is read, in
// legacy_card.go.
const (
	FieldType       = "TYPE"
	FieldSize       = "SIZE"
	FieldTaskGroup  = "TASK_GROUP"
	FieldLanguage   = "LANGUAGE"
	FieldCreatedAt  = "CREATED_AT"
	FieldOwner      = "OWNER"
	FieldSession    = "SESSION"
	FieldWindow     = "WINDOW"
	FieldStartedAt  = "STARTED_AT"
	FieldFinishedAt = "FINISHED_AT"
	FieldTaskBranch = "TASK_BRANCH"
	FieldResult     = "RESULT"
	// FieldIntegrationTarget is optional: it names the non-mainline branch a
	// card integrates into. `kander new` never writes it.
	FieldIntegrationTarget = "INTEGRATION_TARGET"
)

const (
	SectionGoal               = "GOAL"
	SectionUserDecisions      = "USER_DECISIONS"
	SectionExpectedOutcome    = "EXPECTED_OUTCOME"
	SectionAcceptanceCriteria = "ACCEPTANCE_CRITERIA"
	SectionThreatModel        = "THREAT_MODEL"
	SectionOutOfScope         = "OUT_OF_SCOPE"
	SectionDiscussion         = "DISCUSSION"
	SectionImplementation     = "IMPLEMENTATION"
	SectionSummary            = "SUMMARY"
)

const (
	MarkerSelfReview    = "SELF_REVIEW"
	MarkerCardReview    = "CARD_REVIEW"
	MarkerPrerequisites = "PREREQUISITES"
	Placeholder         = "<FILL_IN>"
)

var fieldLineCache sync.Map

// FieldLineRe matches the whole `- NAME: value` line of a metadata field. It
// knows nothing about where the line is: callers must apply it to CardHeader,
// never to a whole card, so that a body line with the same spelling stays text.
func FieldLineRe(name string) *regexp.Regexp {
	if cached, ok := fieldLineCache.Load(name); ok {
		return cached.(*regexp.Regexp)
	}
	re := regexp.MustCompile(`(?m)^- ` + regexp.QuoteMeta(name) + `:.*$`)
	fieldLineCache.Store(name, re)
	return re
}

// editHeader applies edit to the card header and keeps every other byte of the
// card as it is.
func editHeader(text string, edit func(header string) string) string {
	header := CardHeader(text)
	return edit(header) + text[len(header):]
}

// RenderField renders one metadata line.
func RenderField(name, value string) string {
	line := "- " + name + ": " + value
	return strings.TrimRight(line, " ")
}

// ContainsPlaceholder reports whether a section body still holds an unfilled
// placeholder.
func ContainsPlaceholder(body string) bool {
	return strings.Contains(body, Placeholder)
}

// HasMarkerPrefix reports whether a line starts with a record marker such as
// PREREQUISITES.
func HasMarkerPrefix(line, marker string) bool {
	return strings.HasPrefix(strings.TrimSpace(line), marker)
}
