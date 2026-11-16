package issue

import (
	"context"

	rk "github.com/dualface/kander/internal/statemachine/issueresult"
)

// ValidateResultStore is shared by local recovery and the service's controlled
// CAS producer. It preserves the existing result-version identity contract. The
// identities, outcomes, statuses, and close decisions of a record are decided by
// the issue result kernels (result_kernel.go); the schema, the source, the
// version identity, and the field shapes stay text checks.
func ValidateResultStore(store ResultStore, sourceKey, cardID string) error {
	if store.Schema != 1 || store.SourceKey != sourceKey || store.TaskID != cardID || store.Records == nil || store.IssueID < 0 {
		return resultError("inconsistent result recovery record")
	}
	if _, ok := resultStep(rk.Initial(resultKernelParams), rk.IssueNeeded{HasRecords: len(store.Records) > 0, IdPos: store.IssueID > 0}); !ok {
		return resultError("result records need the confirmed numeric issue identity")
	}
	for version, record := range store.Records {
		if record == nil || record.Version != version || !recordTaskKeyValid(record, cardID) || resultVersion(record.taskKey(cardID), record.Commits, record.Outcomes, record.FullyResolved) != version {
			return resultError("invalid result record")
		}
		if err := validRecord(record); err != nil {
			return err
		}
		for _, decision := range record.Decisions {
			if err := validDecision(decision); err != nil {
				return err
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
	if _, ok := resultStep(rk.State{Iid: previous.IssueID > 0}, rk.IdKeep{Same: next.IssueID == previous.IssueID}); !ok {
		return resultError("result issue identity changed")
	}
	for version, old := range previous.Records {
		current := next.Records[version]
		if err := validRecordUpdate(previous, version, old, current); err != nil {
			return err
		}
		for i, decision := range old.Decisions {
			if err := validDecisionUpdate(decision, current.Decisions[i]); err != nil {
				return err
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
		if err := validRecordWork(previous, version, current, work); err != nil {
			return err
		}
	}
	return nil
}
