// Package herdrcore contains the layout protocol the herdr-core definition
// uses to give every new terminal its own named tab in the caller's space.
package herdrcore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/probe"
	"github.com/dualface/kander/internal/terminal"
)

// Budgets of one placement. They are fixed and independent of
// extended_launch_timeouts: a Herdr App that is not connected to the core
// never shows the tab, and every launch then waits the same bounded time.
var (
	totalBudget  = 15 * time.Second
	callBudget   = 3 * time.Second
	tabWait      = 5 * time.Second
	tabPoll      = 200 * time.Millisecond
	maxConflicts = 5
)

const (
	layoutDocument = "herdr-app"
	spaceLabel     = "herdr.space"
	tabLabel       = "herdr.tab"
)

var idPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// PlaceTab gives the new terminal the caller's Herdr App space and a new tab
// of its own, then names that tab. Without a caller space there is nothing to
// place into and the result is OK. Every problem degrades: the terminal keeps
// running, only the placement or the tab name is missing.
func PlaceTab(ctx context.Context, call terminal.HookCall) terminal.HookResult {
	callerID := strings.TrimSpace(call.Getenv("HC_TERMINAL_ID"))
	if callerID == "" {
		return terminal.HookResult{Status: terminal.HookOK}
	}
	ctx, cancel := context.WithTimeout(ctx, totalBudget)
	defer cancel()
	h := hc{conn: call.Conn}
	space, err := h.callerSpace(ctx, callerID)
	if err != nil {
		return degraded("herdr_core.place_failed", probe.FailureDetail(err))
	}
	if space == "" {
		return terminal.HookResult{Status: terminal.HookOK}
	}
	tab, err := newTabID()
	if err != nil {
		return degraded("herdr_core.place_failed", err.Error())
	}
	pane := call.Values["pane"]
	if _, err := h.run(ctx, "label", "--json", pane, spaceLabel+"="+space, tabLabel+"="+tab); err != nil {
		return degraded("herdr_core.place_failed", probe.FailureDetail(err))
	}
	title := tabTitle(call.Values["title"], call.Values["label"])
	if title == "" {
		return terminal.HookResult{Status: terminal.HookOK}
	}
	if err := h.nameTab(ctx, space, tab, title); err != nil {
		if errors.Is(err, errTabMissing) {
			return degraded("herdr_core.tab_missing", tabWait.String())
		}
		return degraded("herdr_core.tab_name_failed", probe.FailureDetail(err))
	}
	return terminal.HookResult{Status: terminal.HookOK}
}

func degraded(id string, args ...any) terminal.HookResult {
	return terminal.HookResult{Status: terminal.HookDegraded, Note: config.Text(id, args...)}
}

// newTabID returns a random tab ID in the form Herdr App itself generates.
func newTabID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "tb" + hex.EncodeToString(raw[:]), nil
}

// tabTitle is the title with control characters removed; it falls back to the
// container label.
func tabTitle(title, label string) string {
	for _, candidate := range []string{title, label} {
		cleaned := strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, candidate))
		if cleaned != "" {
			return cleaned
		}
	}
	return ""
}

// callerSpace reads the Herdr App space of the caller terminal. It is empty
// when the caller carries no valid space label.
func (h hc) callerSpace(ctx context.Context, callerID string) (string, error) {
	out, err := h.run(ctx, "get", "--json", callerID)
	if err != nil {
		return "", err
	}
	var caller struct {
		Labels map[string]any `json:"labels"`
	}
	if err := json.Unmarshal([]byte(out), &caller); err != nil {
		return "", fmt.Errorf("hc get: %w", err)
	}
	space, _ := caller.Labels[spaceLabel].(string)
	if !idPattern.MatchString(space) {
		return "", nil
	}
	return space, nil
}
