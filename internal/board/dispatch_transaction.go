package board

func PrepareDispatch(root string, in DispatchInput) (Dispatch, error) {
	return PrepareDispatchWithTransaction(root, in)
}
func BeginDispatchAttempt(root, task, id string, expected uint64, cardRevision ...uint64) (Dispatch, error) {
	return BeginDispatchAttemptWithTransaction(root, task, id, expected, cardRevision)
}
func ReauthorizeDispatch(root, task, id string, expected uint64, stopped ...WrapUpExitEvidence) (Dispatch, error) {
	return ReauthorizeDispatchWithTransaction(root, task, id, expected, stopped)
}
func EndDispatch(root, task, id string, expected uint64, state DispatchState, reason string, decisions ...string) error {
	return EndDispatchWithTransaction(root, task, id, expected, state, reason, decisions)
}
func SettleDispatch(root, task, id string, expected uint64) error {
	return SettleDispatchWithTransaction(root, task, id, expected)
}
func AuthorizeDispatchWrapUp(root, task, id string, expected uint64, author, reason string, exit WrapUpExitEvidence) (Dispatch, error) {
	return AuthorizeDispatchWrapUpWithTransaction(root, task, id, expected, author, reason, exit)
}
