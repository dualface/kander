package issue

import (
	"context"
	"reflect"
	"slices"
)

// ValidateResultStore is shared by local recovery and the service's controlled
// CAS producer. It preserves the existing result-version identity contract.
func ValidateResultStore(store ResultStore, sourceKey, cardID string) error {
	if store.Schema != 1 || store.SourceKey != sourceKey || store.TaskID != cardID || store.Records == nil || store.IssueID < 0 {
		return resultError("inconsistent result recovery record")
	}
	if len(store.Records) > 0 && store.IssueID <= 0 {
		return resultError("result records need the confirmed numeric issue identity")
	}
	for version, record := range store.Records {
		if record == nil || record.Version != version || !recordTaskKeyValid(record, cardID) || resultVersion(record.taskKey(cardID), record.Commits, record.Outcomes, record.FullyResolved) != version {
			return resultError("invalid result record")
		}
		if record.ActorID <= 0 || (record.Status == "published" || record.Status == "equivalent") && record.CommentID <= 0 {
			return resultError("result records need confirmed numeric remote identities")
		}
		for _, outcome := range record.Outcomes {
			if outcome != "met" && outcome != "unmet" && outcome != "unknown" || record.FullyResolved && outcome != "met" {
				return resultError("invalid stored criterion outcome")
			}
		}
		switch record.Status {
		case "uncertain", "published", "equivalent", "rejected":
		default:
			return resultError("invalid result status")
		}
		for _, decision := range record.Decisions {
			if decision.StateVersion == "" || decision.UserReference == "" || decision.Decision != "yes" && decision.Decision != "no" {
				return resultError("invalid close decision")
			}
			switch decision.Status {
			case "declined", "uncertain", "closed", "observed-closed", "superseded", "rejected":
			default:
				return resultError("invalid close decision status")
			}
			if decision.Decision == "yes" && decision.Status == "declined" {
				return resultError("a close consent cannot become a refusal")
			}
			if decision.Decision == "no" && decision.Status != "declined" {
				return resultError("close refusal cannot authorize a mutation")
			}
		}
	}
	return nil
}

// ValidateResultStoreUpdate refuses deletion or regression of durable remote
// effects. An uncertain result can only be reconciled or definitely rejected.
func ValidateResultStoreUpdate(previous, next ResultStore) error {
	if err := ValidateResultStore(next, previous.SourceKey, previous.TaskID); err != nil {
		return err
	}
	if previous.IssueID > 0 && next.IssueID != previous.IssueID {
		return resultError("result issue identity changed")
	}
	for version, old := range previous.Records {
		current := next.Records[version]
		if current == nil {
			return resultError("result history cannot be removed")
		}
		if old.Status != "rejected" {
			if old.Status != "uncertain" && current.Status != old.Status {
				return resultError("published result status cannot regress")
			}
			if current.Body != old.Body || current.ActorID != old.ActorID || old.CommentID > 0 && current.CommentID != old.CommentID {
				return resultError("published result identity changed")
			}
		}
		if len(current.Failures) < len(old.Failures) || !slices.Equal(current.Failures[:len(old.Failures)], old.Failures) {
			return resultError("result failures cannot be removed")
		}
		if len(current.Decisions) < len(old.Decisions) {
			return resultError("close history cannot be removed")
		}
		for i, decision := range old.Decisions {
			updated := current.Decisions[i]
			if updated.StateVersion != decision.StateVersion || updated.Decision != decision.Decision || updated.UserReference != decision.UserReference {
				return resultError("close decision identity changed")
			}
			if decision.Status != "uncertain" && updated.Status != decision.Status {
				return resultError("settled close decision cannot regress")
			}
		}
	}
	return nil
}

// ReadResultStore supplies a validated copy of migrated local history. A service
// adopts these records on its first central CAS without rewriting the originals.
func ReadResultStore(ctx context.Context, root string, repository Repository, number int, cardID string) (out ResultStore, err error) {
	err = withResultStore(ctx, root, repository, number, cardID, func(store *resultStore, _ func() error) error { out = *store; return nil })
	return out, err
}

// HasPublishedEvidence is a display predicate, never permission to close.
func (s ResultStore) HasPublishedEvidence(version string) bool {
	for _, record := range s.Records {
		if record != nil && (record.Status == "published" || record.Status == "equivalent") && record.EvidenceVersion == version {
			return true
		}
	}
	return false
}

// ValidateResultStoreWork binds newly assessed records to the exact completed
// work. Historical reconciliation retains its original assessment context.
func ValidateResultStoreWork(previous, next ResultStore, work ResultWork) error {
	for version, current := range next.Records {
		old := previous.Records[version]
		if old != nil && old.Status == "rejected" && reflect.DeepEqual(old, current) {
			continue
		}
		fresh := old == nil || old.Status == "rejected"
		if !fresh {
			if !reflect.DeepEqual(current.Checks, old.Checks) {
				return resultError("published verification evidence changed")
			}
			continue
		}
		if current.Status != "uncertain" && current.Status != "equivalent" {
			return resultError("publication needs its preceding uncertain intent")
		}
		for oldVersion, record := range previous.Records {
			if oldVersion != version && record.Status == "uncertain" {
				return resultError("another publication outcome is still uncertain")
			}
		}
		if current.CardDigest != work.Digest || current.EvidenceVersion != work.EvidenceVersion || !slices.Equal(current.TaskIDs, work.taskIDs()) || !slices.Equal(current.Commits, work.Commits) {
			return resultError("result assessment differs from the current work set")
		}
		calculated, err := proposalVersion(work, ResultProposal{Outcomes: current.Outcomes, Checks: current.Checks, FullyResolved: current.FullyResolved, ResolutionEvidence: current.ResolutionEvidence})
		if err != nil {
			return err
		}
		if calculated != version {
			return resultError("result version differs from the current assessment")
		}
	}
	return nil
}
