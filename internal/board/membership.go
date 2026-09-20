package board

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Membership is a deterministic expansion of committed task-group references.
// Problems prevent a consumer from treating any partial expansion as complete.
type Membership struct {
	Groups   map[string][]string
	Versions map[string]string
	Problems []Problem
	// declared holds, per Problems index, every task group the problem's card
	// could still belong to. A missing index means the ownership could not be
	// read at all, so that problem can never be ruled out for any group.
	declared map[int][]string
}

// GroupMembership reuses the dependency parser and preserves scan/read failures.
// It never probes agent processes or tries to repair unknown entries.
func (b Board) GroupMembership() Membership {
	texts := map[string]string{}
	result := Membership{Problems: append([]Problem(nil), b.Problems...), declared: map[int][]string{}}
	ids := make([]string, 0, len(b.Entries))
	for id := range b.Entries {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		text, err := b.Document(id)
		if err != nil {
			result.Problems = append(result.Problems, Problem{Path: b.Entries[id].Document, Message: err.Error()})
			continue
		}
		group := TaskGroupFrom(text)
		if len(fieldLines(text, FieldTaskGroup)) > 1 || (group != "" && !taskGroupRe.MatchString(group)) {
			result.declared[len(result.Problems)] = declaredGroups(text)
			result.Problems = append(result.Problems, Problem{Path: b.Entries[id].Document, Message: t("board.membership_invalid", id)})
			continue
		}
		texts[id] = text
	}
	result.Groups = taskGroupMembers(texts)
	result.Versions = map[string]string{}
	for group, members := range result.Groups {
		result.Versions[group] = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(members, "\n"))))
	}
	return result
}

// declaredGroups lists every task group value TaskGroupFrom could resolve this
// card to. The metadata field wins when its first line has a value, so an empty
// or absent metadata line keeps the legacy discussion value in play; a card with
// several metadata lines contributes all of them, because which one a later
// reader honors depends on how the ambiguity is repaired.
func declaredGroups(text string) []string {
	lines := fieldLines(text, FieldTaskGroup)
	values := make([]string, 0, len(lines)+1)
	incomplete := len(lines) == 0
	for _, line := range lines {
		_, value, _ := strings.Cut(line, ":")
		value = strings.TrimSpace(value)
		if value == "" {
			incomplete = true
			continue
		}
		values = append(values, value)
	}
	if incomplete {
		if legacy := legacyTaskGroupFrom(text); legacy != "" {
			values = append(values, legacy)
		}
	}
	return values
}

// ErrFor rejects a membership view that may be incomplete for the named groups,
// while retaining the diagnostics of every problem it reports. A problem is
// ruled out only when its card's declared groups were all readable and none of
// them names a requested group; unknown ownership is never ruled out. Without
// groups it reports every problem, so a caller that cannot name its references
// still fails closed.
func (m Membership) ErrFor(groups ...string) error {
	requested := make(map[string]bool, len(groups))
	for _, group := range groups {
		requested[group] = true
	}
	var failures []error
	for i, problem := range m.Problems {
		if len(requested) > 0 && !m.mayBelongTo(i, requested) {
			continue
		}
		failures = append(failures, errors.New(problem.Message))
	}
	return errors.Join(failures...)
}

// mayBelongTo reports whether the problem at index i could still conceal a
// member of one of the requested groups.
func (m Membership) mayBelongTo(i int, requested map[string]bool) bool {
	declared, known := m.declared[i]
	if !known {
		return true
	}
	for _, group := range declared {
		if requested[group] {
			return true
		}
	}
	return false
}
