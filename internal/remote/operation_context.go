package remote

import (
	"context"
	"errors"
)

// OperationContext gives a command a fresh operation budget when an interactive
// question consumed the budget passed to Open. Callers that reuse that context
// for subsequent REST operations must use the returned context and cancel it.
// Values and explicit cancellation are preserved; elapsed deadlines are not.
func (s *Session) OperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	if !s.afterQuestion || s.operationBudget <= 0 || errors.Is(parent.Err(), context.Canceled) {
		return parent, func() {}
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), s.operationBudget)
	stop := context.AfterFunc(parent, func() {
		if errors.Is(parent.Err(), context.Canceled) {
			cancel()
		}
	})
	return ctx, func() { stop(); cancel() }
}
