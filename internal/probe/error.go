package probe

import "github.com/dualface/kander/internal/config"

// Error 是 pane 事实采集的可展示失败, 不含投递或存活策略.
type Error struct {
	Message string
}

func (e *Error) Error() string { return e.Message }

func probeError(id string, args ...any) *Error {
	return &Error{Message: config.Text(id, args...)}
}
