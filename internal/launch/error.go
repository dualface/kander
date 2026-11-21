package launch

import (
	"github.com/dualface/kander/internal/config"
)

// Error is a displayable failure of start/resume.
type Error struct {
	Message string
}

func (e *Error) Error() string { return e.Message }

func launchError(id string, args ...any) *Error {
	return &Error{Message: config.Text(id, args...)}
}

// unavailableAgentError reports an agent the configuration names but this version
// cannot use: an unusable agents entry, or a name only a newer version knows.
func unavailableAgentError(cfg *config.Config, name string) *Error {
	return &Error{Message: config.UnavailableAgentError(cfg, name).Error()}
}

func t(id string, args ...any) string {
	return config.Text(id, args...)
}
