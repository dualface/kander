package tui

import (
	"errors"
	"regexp"
	"strings"

	"github.com/dualface/kander/internal/board"
)

// handoffValidationError names the field that has to be fixed, so the editor
// can focus it instead of only printing a message.
type handoffValidationError struct {
	field   handoffFieldID
	message string
}

func (e *handoffValidationError) Error() string { return e.message }

func handoffError(field handoffFieldID, message string) error {
	return &handoffValidationError{field: field, message: message}
}

func handoffFieldForError(err error) handoffFieldID {
	var validation *handoffValidationError
	if errors.As(err, &validation) {
		return validation.field
	}
	return handoffType
}

var (
	handoffHeadingRe  = regexp.MustCompile(`(?m)^## `)
	handoffCheckboxRe = regexp.MustCompile(`(?m)^\s*-\s*\[[ xX]\]\s*\S`)
	handoffFieldLine  = handoffFieldLineRe()
)

// handoffFieldLineRe matches any metadata line a section body must never carry.
// The names come from the board so the legacy Chinese spelling is covered too.
func handoffFieldLineRe() *regexp.Regexp {
	names := []string{
		board.FieldType, board.FieldSize, board.FieldTaskGroup, board.FieldLanguage,
		board.FieldCreatedAt, board.FieldOwner, board.FieldSession, board.FieldWindow,
		board.FieldStartedAt, board.FieldFinishedAt, board.FieldTaskBranch, board.FieldResult,
		"REVISION", "OPERATION_ID", "EXECUTION_EPOCH", "DISPATCH_ID", "REVIEWS",
	}
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, board.TokenPattern(name))
	}
	return regexp.MustCompile(`(?m)^- (?:` + strings.Join(parts, "|") + `):`)
}

func handoffKnownType(value string) (string, bool) {
	for _, name := range board.TaskTypes() {
		if strings.EqualFold(strings.TrimSpace(value), name) {
			return name, true
		}
	}
	return "", false
}

// parseHandoffValues reads the editable contract out of a committed card. A
// missing part is a load error, never a silently empty draft.
func parseHandoffValues(text string) (map[handoffFieldID]string, error) {
	values := make(map[handoffFieldID]string, handoffFieldCount)
	for id, spec := range handoffFieldSpecs {
		field := handoffFieldID(id)
		if spec.Field != "" {
			value := strings.TrimSpace(board.MetadataFrom(text, spec.Field))
			if spec.Field == board.FieldType {
				if canonical, ok := handoffKnownType(value); ok {
					value = canonical
				}
			}
			values[field] = value
			continue
		}
		body, ok := board.SectionBody(text, spec.Section)
		if !ok {
			return nil, handoffError(field, t("tui.handoff_missing_section", spec.Section))
		}
		values[field] = body
	}
	return values, nil
}

// validateHandoffDraft is the mechanical check before the self-review step. It
// only proves the contract is complete and structurally safe; it never claims
// the creator's statements are true.
func validateHandoffDraft(values map[handoffFieldID]string) error {
	for id, spec := range handoffFieldSpecs {
		field := handoffFieldID(id)
		label := handoffFieldLabel(field)
		value := strings.TrimSpace(values[field])
		switch field {
		case handoffType:
			if _, ok := handoffKnownType(value); !ok {
				return handoffError(field, t("tui.handoff_type_invalid"))
			}
			continue
		case handoffSize:
			if value != "small" && value != "large" {
				return handoffError(field, t("tui.handoff_size_invalid"))
			}
			continue
		}
		if value == "" {
			return handoffError(field, t("tui.handoff_empty_field", label))
		}
		if board.ContainsPlaceholder(value) {
			return handoffError(field, t("tui.handoff_placeholder_field", label))
		}
		if handoffHeadingRe.MatchString(value) || handoffFieldLine.MatchString(value) {
			return handoffError(field, t("tui.handoff_structure_field", label))
		}
		// DISCUSSION legitimately carries the records the card already has, so
		// this check only rejects records the draft invented; see
		// validateHandoffRecords, which compares the draft with the loaded card.
		if spec.Section != board.SectionDiscussion && handoffRecordMarker(value) {
			return handoffError(field, t("tui.handoff_structure_field", label))
		}
	}
	if !handoffCheckboxRe.MatchString(values[handoffAcceptanceCriteria]) {
		return handoffError(handoffAcceptanceCriteria, t("tui.handoff_acceptance_items"))
	}
	if !strings.Contains(values[handoffGoal], "source/github-issue.md") {
		return handoffError(handoffGoal, t("tui.handoff_source_required"))
	}
	return nil
}

// handoffRecordLines lists the record lines a body carries. Records belong to
// their own producers, so the editor never mints one. The trimming matches the
// board's import validator, which uses the same marker helper.
func handoffRecordLines(body string) []string {
	lines := []string{}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		for _, marker := range []string{board.MarkerSelfReview, board.MarkerCardReview, board.MarkerPrerequisites} {
			if board.HasMarkerPrefix(value, marker) {
				lines = append(lines, value)
				break
			}
		}
	}
	return lines
}

// validateHandoffRecords refuses a draft that adds a record line the card did
// not already carry. The form writes the SELF_REVIEW line itself from the
// creator's typed conclusion; CARD_REVIEW and PREREQUISITES belong to their own
// producers, so the form must not be able to mint the independent review record
// the todo gate looks for.
func validateHandoffRecords(discussion, source string) error {
	existing := map[string]bool{}
	for _, line := range handoffRecordLines(source) {
		existing[line] = true
	}
	for _, line := range handoffRecordLines(discussion) {
		if !existing[line] {
			return handoffError(handoffDiscussion, t("tui.handoff_record_injected", line))
		}
	}
	return nil
}

// validateHandoffSelfReview keeps the conclusion a single line of prose: the
// record is written as one line, and a multi-line or structured conclusion
// could smuggle a second record into the card.
func validateHandoffSelfReview(conclusion string) error {
	text := strings.TrimSpace(conclusion)
	if text == "" {
		return handoffError(handoffConclusion, t("tui.handoff_conclusion_required"))
	}
	if strings.ContainsAny(text, "\r\n") {
		return handoffError(handoffConclusion, t("tui.handoff_conclusion_single_line"))
	}
	if handoffHeadingRe.MatchString(text) || handoffFieldLine.MatchString(text) || handoffRecordMarker(text) {
		return handoffError(handoffConclusion, t("tui.handoff_conclusion_structure"))
	}
	return nil
}

// handoffRecordMarker reports whether a body carries a review or dependency
// record outside DISCUSSION.
func handoffRecordMarker(value string) bool {
	return len(handoffRecordLines(value)) > 0
}

// buildHandoffText writes the draft back over the committed card. Only the two
// metadata lines and the seven section bodies change; every other byte,
// including managed fields and the review records, is preserved.
func buildHandoffText(state *handoffState) (string, error) {
	values := make(map[handoffFieldID]string, len(state.values)+1)
	for id, value := range state.values {
		values[id] = value
	}
	values[handoffDiscussion] = appendHandoffSelfReview(values[handoffDiscussion], state.conclusion)
	text := state.source
	for id, spec := range handoffFieldSpecs {
		value := values[handoffFieldID(id)]
		var err error
		if spec.Field != "" {
			text, err = replaceHandoffField(text, spec.Field, value)
		} else {
			text, err = replaceHandoffSection(text, spec.Section, value)
		}
		if err != nil {
			return "", err
		}
	}
	return text, nil
}

func replaceHandoffField(text, name, value string) (string, error) {
	loc := board.FieldLineRe(name).FindStringIndex(text)
	if loc == nil {
		return "", errors.New(t("tui.handoff_missing_field", name))
	}
	return text[:loc[0]] + board.RenderField(name, value) + text[loc[1]:], nil
}

var handoffSectionHeadingRe = regexp.MustCompile(`(?m)^## `)

func replaceHandoffSection(text, name, body string) (string, error) {
	heading := regexp.MustCompile(`(?m)^## ` + board.TokenPattern(name) + `[ \t\r]*$`)
	loc := heading.FindStringIndex(text)
	if loc == nil {
		return "", errors.New(t("tui.handoff_missing_section", name))
	}
	end := len(text)
	if next := handoffSectionHeadingRe.FindStringIndex(text[loc[1]:]); next != nil {
		end = loc[1] + next[0]
	}
	return text[:loc[1]] + "\n\n" + strings.TrimSpace(body) + "\n\n" + text[end:], nil
}

// appendHandoffSelfReview replaces an older record with the conclusion the
// creator typed now. The editor never invents a conclusion and never writes a
// CARD_REVIEW line: the review gate stays with an independent reviewer.
func appendHandoffSelfReview(discussion, conclusion string) string {
	lines := strings.Split(strings.ReplaceAll(discussion, "\r\n", "\n"), "\n")
	kept := make([]string, 0, len(lines)+2)
	for _, line := range lines {
		if board.HasMarkerPrefix(line, board.MarkerSelfReview) {
			continue
		}
		kept = append(kept, line)
	}
	text := strings.TrimRight(strings.Join(kept, "\n"), " \t\n")
	record := "- " + board.MarkerSelfReview + ": " + strings.TrimSpace(conclusion)
	if text == "" {
		return record
	}
	return text + "\n\n" + record
}

// cycleHandoffValue moves the two inferred selectors. TYPE is normalized to the
// canonical token when the card carried the display spelling.
func cycleHandoffValue(state *handoffState, delta int) {
	switch state.focus {
	case handoffType:
		state.values[handoffType] = cycleOption(board.TaskTypes(), state.values[handoffType], delta)
	case handoffSize:
		state.values[handoffSize] = cycleOption([]string{"small", "large"}, state.values[handoffSize], delta)
	}
}

func cycleOption(options []string, current string, delta int) string {
	index := 0
	for i, option := range options {
		if strings.EqualFold(strings.TrimSpace(current), option) {
			index = i
			break
		}
	}
	index = (index + delta) % len(options)
	if index < 0 {
		index += len(options)
	}
	return options[index]
}
