package tui

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
)

// policyRequestTimeout bounds one policy read or write of the TUI.
const policyRequestTimeout = 15 * time.Second

// policyWatch keeps the project policy of this process current and sends the
// policy edits of Options. It has its own scheduler beside the board events:
// one request at a time, a write before a read, and none blocks Update. Event
// pages only ask for a read; the read itself updates the policy cache and the
// configuration of the process, and its result updates what Options shows.
type policyWatch struct {
	// read and write reach the server project; both are nil without one.
	read  func(context.Context) (config.PolicySnapshot, error)
	write func(context.Context, uint64, json.RawMessage) (config.PolicySnapshot, error)
	// known is the policy this process works with.
	known config.PolicySnapshot
	// readQueued asks for a read; resetAfterRead makes it drop the policy
	// edits of Options, after the server refused a write.
	readQueued, resetAfterRead bool
	writeQueued                *policyWriteRequest
	seq, inFlight              uint64
	cancel                     context.CancelFunc
}

type policyWriteRequest struct {
	expected uint64
	values   json.RawMessage
}

// policyResult is the answer of one policy read or write.
type policyResult struct {
	seq      uint64
	write    bool
	snapshot config.PolicySnapshot
	err      error
}

// attachPolicy binds the policy requests to the server project of session.
func (a *App) attachPolicy(session *remote.Session) {
	a.cancelPolicy()
	a.policy = policyWatch{seq: a.policy.seq, known: remote.PolicySnapshot(session.CurrentPolicy())}
	a.policy.read = func(ctx context.Context) (config.PolicySnapshot, error) {
		// The refresh function writes the cache and the policy of this process; the
		// TUI owns the terminal, so a failed cache write is not printed.
		policy, _, err := session.FetchPolicy(ctx)
		if err != nil {
			return config.PolicySnapshot{}, err
		}
		session.AdoptPolicy(policy)
		return remote.PolicySnapshot(policy), nil
	}
	a.policy.write = func(ctx context.Context, expected uint64, values json.RawMessage) (config.PolicySnapshot, error) {
		written, _, err := session.WritePolicy(ctx, api.PolicyWrite{ExpectedRevision: expected, Values: values})
		if err != nil {
			return config.PolicySnapshot{}, err
		}
		session.AdoptPolicy(written)
		return remote.PolicySnapshot(written), nil
	}
}

func (a *App) cancelPolicy() {
	if a.policy.cancel != nil {
		a.policy.cancel()
		a.policy.cancel = nil
	}
	a.policy.seq++
	a.policy.inFlight = 0
}

// notePolicyPage asks for a policy read when an event page says the policy may
// have changed: a reset, which may hide a change, a change event above the
// known revision, or a page version newer than the known one. A repeated or
// late event at or below the known revision asks for nothing.
func (a *App) notePolicyPage(page api.EventPage) {
	watch := &a.policy
	if watch.read == nil {
		return
	}
	if page.Reset {
		watch.readQueued = true
		return
	}
	if page.PolicyRevision > 0 && (page.PolicyEpoch != watch.known.Epoch || page.PolicyRevision > watch.known.Revision) {
		watch.readQueued = true
		return
	}
	for _, e := range page.Events {
		if e.Kind == api.EventProjectPolicyChanged && e.Revision > watch.known.Revision {
			watch.readQueued = true
			return
		}
	}
}

// queuePolicyWrite sends the policy edits of Options with the revision they
// started from.
func (a *App) queuePolicyWrite(expected uint64, values json.RawMessage) {
	a.policy.writeQueued = &policyWriteRequest{expected: expected, values: values}
}

// takePolicy starts the next policy request when none is in flight.
func (a *App) takePolicy() tea.Cmd {
	watch := &a.policy
	if watch.read == nil || !a.Running || watch.inFlight != 0 || (watch.writeQueued == nil && !watch.readQueued) {
		return nil
	}
	watch.seq++
	seq := watch.seq
	watch.inFlight = seq
	ctx, cancel := context.WithTimeout(context.Background(), policyRequestTimeout)
	watch.cancel = cancel
	if request := watch.writeQueued; request != nil {
		watch.writeQueued = nil
		write := watch.write
		return func() tea.Msg {
			defer cancel()
			snapshot, err := write(ctx, request.expected, request.values)
			return workMsg{payload: policyResult{seq: seq, write: true, snapshot: snapshot, err: err}}
		}
	}
	watch.readQueued = false
	read := watch.read
	return func() tea.Msg {
		defer cancel()
		snapshot, err := read(ctx)
		return workMsg{payload: policyResult{seq: seq, snapshot: snapshot, err: err}}
	}
}

// applyPolicyResult takes the answer of a policy request on the Update
// goroutine. A failed read keeps the previous values; the next event or reset
// reads again.
func (a *App) applyPolicyResult(result policyResult) tea.Cmd {
	watch := &a.policy
	if result.seq != watch.inFlight {
		return nil
	}
	watch.inFlight, watch.cancel = 0, nil
	panel := a.Options
	if panel != nil && panel.session == nil {
		panel = nil
	}
	if result.write {
		return a.applyPolicyWrite(panel, result)
	}
	reset := watch.resetAfterRead
	watch.resetAfterRead = false
	if result.err != nil {
		message := t("tui.policy_refresh_failed", result.err.Error())
		a.showFocusNotice(message)
		if panel != nil && !reset {
			panel.policyNotice = message
		}
		return nil
	}
	watch.known = result.snapshot
	if panel == nil {
		return nil
	}
	switch {
	case reset:
		panel.session.ResetPolicy(result.snapshot)
		panel.dirty = panel.session.HasUnsaved()
	case panel.session.AttachPolicy(result.snapshot):
		panel.policyNotice = t("tui.policy_changed_on_server", strconv.FormatUint(result.snapshot.Revision, 10))
		return nil
	}
	return panel.refreshPolicyView()
}

func (a *App) applyPolicyWrite(panel *optionsPanel, result policyResult) tea.Cmd {
	switch {
	case api.IsCode(result.err, api.CodePolicyRevisionConflict):
		// Never overwrite the server: read its policy and show it instead.
		a.policy.readQueued, a.policy.resetAfterRead = true, true
		if panel != nil {
			panel.policyNotice = t("tui.policy_conflict")
		}
		return nil
	case result.err != nil:
		if panel != nil {
			panel.policyNotice = t("tui.policy_save_failed", result.err.Error())
		}
		return nil
	}
	a.policy.known = result.snapshot
	if panel == nil {
		return nil
	}
	panel.session.AcceptPolicyWrite(result.snapshot)
	panel.dirty = panel.session.HasUnsaved()
	panel.policyNotice = t("tui.policy_saved", strconv.FormatUint(result.snapshot.Revision, 10))
	return panel.refreshPolicyView()
}

// refreshPolicyView shows the current policy values on the open page of
// Options: the workflow chart, or the form, rebuilt with its focus kept.
func (p *optionsPanel) refreshPolicyView() tea.Cmd {
	if p.flowScale != "" {
		p.refreshFlowReport()
		return nil
	}
	return p.resizeForm()
}

// queuePolicySave sends the policy edits of the session, independently of the
// configuration files the same save writes: each reports its own result.
func (p *optionsPanel) queuePolicySave() {
	if p.session == nil || !p.session.PolicyDirty() {
		return
	}
	expected, values, err := p.session.PolicyWrite()
	if err != nil {
		p.policyNotice = t("tui.policy_save_failed", err.Error())
		return
	}
	p.app.queuePolicyWrite(expected, values)
}
