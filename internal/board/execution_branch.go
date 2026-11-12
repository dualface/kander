package board

import (
	"context"
	"strings"
)

// RecordExecutionBranchWithTransaction records the branch prepared by a
// launcher before the agent runs. It advances the launcher's own cursor, so
// later rollback cannot mistake this setup write for executor-authored work.
func RecordExecutionBranchWithTransaction(root string, entry Entry, branch string, extensions ...TransactionExtension) (ExecutionResult, error) {
	if strings.TrimSpace(branch) == "" || strings.ContainsAny(branch, "\r\n\x00") {
		return ExecutionResult{}, coordinatorError("execution branch binding")
	}
	return executionWrite(context.Background(), root, entry, func(tx *Transaction, snapshot Snapshot, ledger *executionLedger) (string, error) {
		if !ledger.current.StartUnfinished() {
			return "", coordinatorError("execution branch needs unfinished start")
		}
		return setMetadata(snapshot.Text, FieldTaskBranch, branch)
	}, extensions...)
}
