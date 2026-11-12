package board

import "context"

// BeginTaskStart preserves the standalone storage entrance.
func BeginTaskStart(entry Entry, root string, in TaskStartInput) (Entry, error) {
	return BeginTaskStartWithTransaction(entry, root, in)
}

// RecordExecutionWindow preserves the standalone storage entrance.
func RecordExecutionWindow(root string, entry Entry, window, resolved string) (ExecutionResult, error) {
	return RecordExecutionWindowWithTransaction(root, entry, window, resolved)
}

// RecordExecutionSession preserves the standalone storage entrance.
func RecordExecutionSession(ctx context.Context, root string, entry Entry, before, after string) (ExecutionResult, error) {
	return RecordExecutionSessionWithTransaction(ctx, root, entry, before, after)
}

// RecordExecutionProcess preserves the standalone storage entrance.
func RecordExecutionProcess(root string, entry Entry, process ExecutionProcess) (ExecutionResult, error) {
	return RecordExecutionProcessWithTransaction(root, entry, process)
}

// BeginTakeover preserves the standalone storage entrance.
func BeginTakeover(root string, entry Entry, in TakeoverInput) (ExecutionResult, error) {
	return BeginTakeoverWithTransaction(root, entry, in)
}

// ConfirmTaskStart preserves the standalone storage entrance.
func ConfirmTaskStart(root string, entry Entry) error {
	return ConfirmTaskStartWithTransaction(root, entry)
}

// ConfirmTaskStartSession preserves the standalone storage entrance.
func ConfirmTaskStartSession(root string, entry Entry, before, after string) error {
	return ConfirmTaskStartSessionWithTransaction(root, entry, before, after)
}

// RollbackDocument preserves the standalone storage entrance.
func RollbackDocument(root string, entry Entry, text, state string) error {
	return RollbackDocumentWithTransaction(root, entry, text, state)
}
