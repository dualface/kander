package liveness

import (
	"os/exec"
	"regexp"

	"github.com/dualface/kander/internal/config"
)

const (
	Alive   = "alive"
	Stopped = "stopped"
	Drifted = "drifted"
	Unknown = "unknown"
)

var (
	sessionReferenceRe = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)
	herdrWindowRe      = regexp.MustCompile(`^herdr:([^:\s]+:[^:\s]+):([^:\s]+:[^:\s]+)$`)
	tmuxWindowRe       = regexp.MustCompile(`^(tmux|tmux-session):([^:\s]+):([^:\s]+):([^:\s]+)$`)
	taskGroupRe        = regexp.MustCompile(`^\d{8}-[a-z0-9]+(?:-[a-z0-9]+)*-group$`)

	lookPath = exec.LookPath
)

// TaskSession is the parsed session field of a card.
type TaskSession struct {
	Agent     string
	Reference string
}

// TmuxPaneLocation is the pane coordinate hit by a tmux reverse lookup.
type TmuxPaneLocation struct {
	SessionID   string
	SessionName string
	WindowID    string
	PaneID      string
}

// Report is the read-only liveness classification of one card; it never writes to the card.
type Report struct {
	TaskID    string
	Agent     string
	Status    string
	Channel   string
	Container string
	Detail    string
	NewWindow string
}

func t(id string, args ...any) string {
	return config.Text(id, args...)
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
