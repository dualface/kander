package notify

import (
	"errors"
	"time"

	"github.com/dualface/kander/internal/config"
)

const pollInterval = 100 * time.Millisecond

// Error 是 notify 策略层的可展示失败.
type Error struct{ Message string }

func (e *Error) Error() string { return e.Message }

// BusyError 表示目标忙且 timeout 内未空闲; 不走恢复, 不改卡片.
type BusyError struct{ Message string }

func (e *BusyError) Error() string { return e.Message }

func notifyError(id string, args ...any) *Error {
	return &Error{Message: config.Text(id, args...)}
}

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

func isBusy(err error) bool {
	var busy *BusyError
	return errors.As(err, &busy)
}
