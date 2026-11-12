package board

import (
	"context"
	"slices"
)

func ValidTaskGroup(id string) bool { return taskGroupRe.MatchString(id) }
func ClaimCoordinator(ctx context.Context, root string, r CoordinatorClaim) (CoordinatorCheckpoint, error) {
	return ClaimCoordinatorWithTransaction(ctx, root, r)
}
func ReconcileCoordinator(ctx context.Context, root string, r CoordinatorReconcile, verify func(context.Context, CoordinatorGitFacts) error) (CoordinatorCheckpoint, error) {
	return ReconcileCoordinatorWithTransaction(ctx, root, r, verify)
}
func RecoverCoordinator(ctx context.Context, root string, r CoordinatorRecover, observed uint64) (CoordinatorCheckpoint, error) {
	return RecoverCoordinatorWithTransaction(ctx, root, r, observed)
}

func LookupCoordinatorCheckpoint(ctx context.Context, root, group string) (checkpoint CoordinatorCheckpoint, found bool, err error) {
	err = WithTransactionContext(ctx, root, LockScope{Groups: []string{group}, ReadOnly: true}, func(tx *Transaction) error {
		checkpoint, found, err = readCheckpoint(tx, group)
		return err
	})
	return
}

// CollectCoordinatorGitFacts captures the producer facts a Git-capable client
// must verify. Reconciliation repeats the pass before accepting those verdicts.
// It writes no checkpoint and executes no Git or terminal command.
func CollectCoordinatorGitFacts(ctx context.Context, root string, r CoordinatorReconcile) (facts []CoordinatorGitFacts, err error) {
	ids := make([]string, 0, len(r.Members))
	for id := range r.Members {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	if len(ids) == 0 {
		return nil, coordinatorError("members required")
	}
	scope := LockScope{Groups: []string{r.GroupID, reviewControlGroup, dispatchRegistry, taskStartGroup}, Tasks: ids, ReadOnly: true, validateMutable: true}
	err = WithTransactionContext(ctx, root, scope, func(tx *Transaction) error {
		_, _, err := reconcileCheckpoint(ctx, tx, r, ids, &coordinatorPass{group: r.GroupID, verify: func(_ context.Context, value CoordinatorGitFacts) error { facts = append(facts, value); return nil }})
		return err
	})
	return
}

// CurrentExecutionInTransaction reads the producer's ledger while its start
// group and task locks are held. Service gates never call a nested board API.
func (tx *Transaction) CurrentExecutionInTransaction(id string) (ExecutionView, error) {
	s, err := tx.Snapshot(id)
	if err != nil {
		return ExecutionView{}, err
	}
	ledger, err := readExecutionLedger(tx, s)
	if err != nil {
		return ExecutionView{}, err
	}
	view := ExecutionView{Stored: ledger.synthesized < len(ledger.chain)}
	if ledger.current != nil {
		view.Record, view.Exists = *ledger.current, true
	}
	return view, nil
}
