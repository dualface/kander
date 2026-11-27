package tui

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/menu"
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
	// read and write reach the server project; both are nil without one. Each
	// returns a failed cache write as cacheErr beside the answer.
	read  func(context.Context) (snapshot config.PolicySnapshot, cacheErr, err error)
	write func(context.Context, uint64, json.RawMessage) (snapshot config.PolicySnapshot, cacheErr, err error)
	// known is the policy this process works with.
	known config.PolicySnapshot
	// cacheStale is set while the policy cache may be older than known: the
	// last answer could not be stored. The next event page reads again, whatever
	// its version, so the cache catches up without a second write.
	cacheStale bool
	// readQueued asks for a read. resetFor is the Options session whose policy
	// edits that read drops, after the server refused its write.
	readQueued bool
	resetFor   *menu.Session
	// writeFor is the Options session whose policy edits are to be sent. The
	// write is built when it starts, from the edits and the base of that moment,
	// so a second save never sends a revision the first one already used.
	writeFor      *menu.Session
	seq, inFlight uint64
	// inFlightFor is the session of the write in flight.
	inFlightFor *menu.Session
	cancel      context.CancelFunc
}

// policyResult is the answer of one policy read or write.
type policyResult struct {
	seq      uint64
	write    bool
	snapshot config.PolicySnapshot
	cacheErr error
	err      error
}

// attachPolicy binds the policy requests to the server project of session.
func (a *App) attachPolicy(session *remote.Session) {
	a.cancelPolicy()
	a.policy = policyWatch{seq: a.policy.seq, known: remote.PolicySnapshot(session.CurrentPolicy())}
	// The refresh function writes the cache and the policy of this process; the
	// TUI owns the terminal, so a failed cache write comes back as a notice.
	a.policy.read = func(ctx context.Context) (config.PolicySnapshot, error, error) {
		policy, cacheErr, err := session.FetchPolicy(ctx)
		if err != nil {
			return config.PolicySnapshot{}, nil, err
		}
		session.AdoptPolicy(policy)
		return remote.PolicySnapshot(policy), cacheErr, nil
	}
	a.policy.write = func(ctx context.Context, expected uint64, values json.RawMessage) (config.PolicySnapshot, error, error) {
		written, cacheErr, err := session.WritePolicy(ctx, api.PolicyWrite{ExpectedRevision: expected, Values: values})
		if err != nil {
			return config.PolicySnapshot{}, nil, err
		}
		session.AdoptPolicy(written)
		return remote.PolicySnapshot(written), cacheErr, nil
	}
}

// attachKnownPolicy gives a newly loaded Options session the policy this
// process knows at the moment the session is installed.
func (a *App) attachKnownPolicy(session *menu.Session) {
	if a.policy.read != nil && session != nil {
		session.AttachPolicy(a.policy.known)
	}
}

func (a *App) cancelPolicy() {
	if a.policy.cancel != nil {
		a.policy.cancel()
		a.policy.cancel = nil
	}
	a.policy.seq++
	a.policy.inFlight = 0
	a.policy.inFlightFor = nil
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
	if page.Reset || watch.cacheStale {
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

// queuePolicyWrite asks to send the policy edits of session.
func (a *App) queuePolicyWrite(session *menu.Session) {
	a.policy.writeFor = session
}

// takePolicy starts the next policy request when none is in flight. A queued
// write is built from its session now; one with nothing left to send is
// dropped.
func (a *App) takePolicy() tea.Cmd {
	watch := &a.policy
	if watch.read == nil || !a.Running || watch.inFlight != 0 {
		return nil
	}
	var expected uint64
	var values json.RawMessage
	session := watch.writeFor
	watch.writeFor = nil
	if session != nil && session.PolicyDirty() {
		var err error
		if expected, values, err = session.PolicyWrite(); err != nil {
			if panel := a.Options; panel != nil && panel.session == session {
				panel.policyNotice = t("tui.policy_save_failed", err.Error())
			}
			session = nil
		}
	} else {
		session = nil
	}
	if session == nil && !watch.readQueued {
		return nil
	}
	watch.seq++
	seq := watch.seq
	watch.inFlight = seq
	ctx, cancel := context.WithTimeout(context.Background(), policyRequestTimeout)
	watch.cancel = cancel
	if session != nil {
		watch.inFlightFor = session
		write := watch.write
		return func() tea.Msg {
			defer cancel()
			snapshot, cacheErr, err := write(ctx, expected, values)
			return workMsg{payload: policyResult{seq: seq, write: true, snapshot: snapshot, cacheErr: cacheErr, err: err}}
		}
	}
	watch.readQueued = false
	read := watch.read
	return func() tea.Msg {
		defer cancel()
		snapshot, cacheErr, err := read(ctx)
		return workMsg{payload: policyResult{seq: seq, snapshot: snapshot, cacheErr: cacheErr, err: err}}
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
	owner := watch.inFlightFor
	watch.inFlight, watch.inFlightFor, watch.cancel = 0, nil, nil
	panel := a.Options
	if panel != nil && panel.session == nil {
		panel = nil
	}
	if result.write {
		return a.applyPolicyWrite(panel, owner, result)
	}
	reset := watch.resetFor
	watch.resetFor = nil
	if result.err != nil {
		message := t("tui.policy_refresh_failed", result.err.Error())
		a.showFocusNotice(message)
		if panel != nil && (reset == nil || reset != panel.session) {
			panel.policyNotice = message
		}
		return nil
	}
	a.acceptPolicy(panel, result)
	if panel == nil {
		return nil
	}
	switch {
	case reset != nil && reset == panel.session:
		panel.session.ResetPolicy(result.snapshot)
		panel.dirty = panel.session.HasUnsaved()
	case panel.session.AttachPolicy(result.snapshot):
		panel.policyNotice = t("tui.policy_changed_on_server", strconv.FormatUint(result.snapshot.Revision, 10))
		return nil
	}
	return panel.refreshPolicyView()
}

// acceptPolicy records a policy the server answered as the policy of this
// process. When its cache write failed, a notice says so and the next event
// page reads again to store it.
func (a *App) acceptPolicy(panel *optionsPanel, result policyResult) {
	a.policy.known = result.snapshot
	a.policy.cacheStale = result.cacheErr != nil
	if result.cacheErr != nil {
		message := t("tui.policy_cache_failed", result.cacheErr.Error())
		a.showFocusNotice(message)
		if panel != nil {
			panel.policyNotice = message
		}
	}
}

// applyPolicyWrite takes the answer of a write of owner, the session whose
// edits it sent. Only that session takes the written policy as the base of its
// edits; an edit made after the save stays pending. Another panel, opened
// since, only learns the new policy like any refresh.
func (a *App) applyPolicyWrite(panel *optionsPanel, owner *menu.Session, result policyResult) tea.Cmd {
	own := panel != nil && panel.session == owner
	switch {
	case api.IsCode(result.err, api.CodePolicyRevisionConflict):
		// Never overwrite the server: read its policy and show it instead.
		a.policy.readQueued = true
		if own {
			a.policy.resetFor = owner
			panel.policyNotice = t("tui.policy_conflict")
		}
		return nil
	case result.err != nil:
		if own {
			panel.policyNotice = t("tui.policy_save_failed", result.err.Error())
		}
		return nil
	}
	a.acceptPolicy(panel, result)
	if panel == nil {
		return nil
	}
	if !own {
		if panel.session.AttachPolicy(result.snapshot) {
			panel.policyNotice = t("tui.policy_changed_on_server", strconv.FormatUint(result.snapshot.Revision, 10))
			return nil
		}
		return panel.refreshPolicyView()
	}
	panel.session.AcceptPolicyWrite(result.snapshot)
	panel.dirty = panel.session.HasUnsaved()
	if result.cacheErr == nil {
		panel.policyNotice = t("tui.policy_saved", strconv.FormatUint(result.snapshot.Revision, 10))
	}
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
	if p.session != nil && p.session.PolicyDirty() {
		p.app.queuePolicyWrite(p.session)
	}
}
