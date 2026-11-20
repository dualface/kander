package board

import (
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

// integrationTargetMaxBytes bounds the branch name a card can declare.
const integrationTargetMaxBytes = 100

var (
	// integrationTargetRe is a strict subset of what `git check-ref-format
	// --branch` accepts: path segments that start with a letter or digit and
	// hold only letters, digits, '.', '_' and '-'.
	integrationTargetRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)*$`)
	// integrationTargetFieldRe reads the target field from a card header.
	integrationTargetFieldRe = regexp.MustCompile(`(?m)^- ` + FieldIntegrationTarget + `:[ \t]*(.*?)[ \t\r]*$`)
)

// ValidIntegrationTarget reports whether value is a branch name a card may
// declare as its integration target. It never calls Git.
func ValidIntegrationTarget(value string) bool {
	if value == "" || len(value) > integrationTargetMaxBytes || !integrationTargetRe.MatchString(value) {
		return false
	}
	if value == "HEAD" || strings.Contains(value, "..") || strings.HasPrefix(value, "refs/") || strings.HasPrefix(value, "group/") {
		return false
	}
	for _, segment := range strings.Split(value, "/") {
		if strings.HasSuffix(segment, ".lock") || strings.HasSuffix(segment, ".") {
			return false
		}
	}
	return true
}

// integrationTargetValues returns the value of every target field line in the
// card header. A line with the same spelling in a body section is ordinary text.
func integrationTargetValues(text string) []string {
	matches := integrationTargetFieldRe.FindAllStringSubmatch(CardHeader(text), -1)
	values := make([]string, len(matches))
	for i, match := range matches {
		values[i] = match[1]
	}
	return values
}

// IntegrationTargetOf reads the integration target field of a card. A missing
// or empty field returns "": the card integrates into the mainline branch. More
// than one field line, or a value that is not a valid branch name, is an error.
func IntegrationTargetOf(text string) (string, error) {
	values := integrationTargetValues(text)
	switch {
	case len(values) == 0:
		return "", nil
	case len(values) > 1:
		return "", kanbanError("board.pin_duplicate_field", FieldIntegrationTarget)
	case values[0] != "" && !ValidIntegrationTarget(values[0]):
		return "", kanbanError("board.integration_target_invalid", values[0])
	}
	return values[0], nil
}

// integrationTargetMemberStates are the states in which a group member is
// compared with its siblings. Backlog cards are drafts and closed cards no
// longer take part in an open integration.
var integrationTargetMemberStates = []string{"todo", "working", "review"}

// integrationTargetProblems reports a duplicated or invalid target field of
// every selected card and one inconsistency per task group. Members are read
// from full, the whole board, so a check of one member still sees its siblings.
func integrationTargetProblems(selected, full Board) []Problem {
	var problems []Problem
	reported := map[string]struct{}{}
	ids := make([]string, 0, len(selected.Entries))
	for id := range selected.Entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		entry := selected.Entries[id]
		if !slices.Contains(integrationTargetMemberStates, entry.State) {
			continue
		}
		text, err := selected.Document(id)
		if err != nil {
			continue
		}
		if _, err := IntegrationTargetOf(text); err != nil {
			problems = append(problems, Problem{Path: entry.Document, Message: t("board.contract_defect", id, err.Error())})
		}
		group := TaskGroupFrom(text)
		if group == "" {
			continue
		}
		if _, done := reported[group]; done {
			continue
		}
		reported[group] = struct{}{}
		if message := integrationTargetMismatch(full, group); message != "" {
			problems = append(problems, Problem{Path: entry.Document, Message: message})
		}
	}
	return problems
}

// integrationTargetMismatch names the members of one task group whose target
// fields differ; a member with no field counts as a value of its own. A member
// whose field cannot be read is reported by its own check, not compared here.
func integrationTargetMismatch(board Board, group string) string {
	ids := make([]string, 0, len(board.Entries))
	for id := range board.Entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	values := map[string]struct{}{}
	var members []string
	for _, id := range ids {
		if !slices.Contains(integrationTargetMemberStates, board.Entries[id].State) {
			continue
		}
		text, err := board.Document(id)
		if err != nil || TaskGroupFrom(text) != group {
			continue
		}
		value, err := IntegrationTargetOf(text)
		if err != nil {
			continue
		}
		if value == "" {
			value = t("board.integration_target_none")
		}
		values[value] = struct{}{}
		members = append(members, fmt.Sprintf("%s=%s", id, value))
	}
	if len(values) < 2 {
		return ""
	}
	return t("board.integration_target_group_mismatch", group, strings.Join(members, ", "))
}
