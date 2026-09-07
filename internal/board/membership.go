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
}

// GroupMembership reuses the dependency parser and preserves scan/read failures.
// It never probes agent processes or tries to repair unknown entries.
func (b Board) GroupMembership() Membership {
	texts := map[string]string{}
	result := Membership{Problems: append([]Problem(nil), b.Problems...)}
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

// Err rejects an incomplete membership view while retaining all diagnostics.
func (m Membership) Err() error {
	var failures []error
	for _, problem := range m.Problems {
		failures = append(failures, errors.New(problem.Message))
	}
	return errors.Join(failures...)
}
