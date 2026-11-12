package api

import (
	"github.com/dualface/kander/internal/board"
	"time"
)

// SnapshotQuery selects a coordinated read. Nil tasks captures the entire
// board for group expansion; an empty array captures no cards.
type SnapshotQuery struct {
	Tasks  []string `json:"tasks"`
	Groups []string `json:"groups,omitempty"`
}
type BoardSnapshot struct {
	ObservedAt time.Time             `json:"observed_at"`
	View       board.CapturedView    `json:"view"`
	Claims     map[string]*ClaimView `json:"claims"`
	Blocked    map[string]string     `json:"blocked"`
	Cursor     string                `json:"cursor"`
}

type CheckQuery struct {
	Tasks []string `json:"tasks,omitempty"`
	All   bool     `json:"all,omitzero"`
}
type CheckResult struct {
	Code        int      `json:"code"`
	Output      string   `json:"output"`
	Diagnostics []string `json:"diagnostics"`
}
