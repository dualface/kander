package liveness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func prepareSubscriptionDispatch(t *testing.T, root, id, kind string, deadline time.Duration) board.Dispatch {
	t.Helper()
	now := time.Now().UTC()
	in := board.DispatchInput{ID: "subscription-round", TaskID: id,
		Kind: kind, Message: "private dispatch payload", Base: strings.Repeat("a", 40),
		CreatedAt: now.Add(-time.Minute), ConfirmBy: now.Add(deadline)}
	if kind == "wrap-up" {
		in.Base = strings.Repeat("b", 40)
		in.Evidence.WrapUp = &board.DispatchWrapUpBinding{Artifact: board.ArtifactReference{TaskID: id, Path: "dispatches/" + in.ID + "/integration.json"}, Git: board.DispatchIntegration{DispatchID: in.ID, TaskID: id, CWD: t.TempDir(), SourceCommit: in.Base, ReviewTarget: in.Base, ReviewBase: strings.Repeat("a", 40), TargetCommit: in.Base, TargetRef: "refs/heads/develop", Author: "fixture", Basis: "structural subscription fixture; no Git verification claimed", VerifiedAt: now}}
	}
	dispatch, err := board.PrepareDispatch(root, in)
	if err != nil {
		t.Fatal(err)
	}
	return dispatch
}

func completeSubscriptionDispatch(t *testing.T, root, id string, dispatch board.Dispatch, target string) {
	t.Helper()
	if _, err := board.MoveWithOptions(currentEntry(t, root, id), root, "working", board.MoveOptions{Authorization: dispatch.Authorization}); err != nil {
		t.Fatal(err)
	}
	options := board.MoveOptions{Authorization: dispatch.Authorization, DeliveryCommit: strings.Repeat("b", 40)}
	if target == "done" {
		options.Result = "completed"
	}
	if _, err := board.MoveWithOptions(currentEntry(t, root, id), root, target, options); err != nil {
		t.Fatal(err)
	}
}

// This fixture closes only a lifecycle test plan, with no claim of Git review.
func closeSubscriptionFixture(t *testing.T, root, id string) {
	t.Helper()
	p := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "subscription-plan", Author: "fixture", Basis: "lifecycle-only test",
		CWD: "/repo", ReportLanguage: "en", TaskIDs: []string{id}, Batches: []board.ReviewPlanBatch{{BatchID: "subscription-batch", TaskIDs: []string{id},
			Base: strings.Repeat("a", 40), TargetCommit: strings.Repeat("b", 40), Requirements: map[string]string{
				"PM": "N/A: lifecycle fixture", "QA": "N/A: lifecycle fixture", "CSA": "N/A: lifecycle fixture", "Hacker": "N/A: lifecycle fixture"}}}}
	if err := board.CreateReviewPlan(root, p); err != nil {
		t.Fatal(err)
	}
	v, err := board.ReadReviewBatchView(root, "subscription-batch")
	if err != nil {
		t.Fatal(err)
	}
	r := board.ReviewCloseRequest{BatchID: v.Batch.BatchID, ExpectedRevision: v.Batch.Revision, ViewHash: board.ReviewViewDigest(v), Author: "fixture", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(v, r)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.CloseReviewBatch(root, r, board.ReviewGitEvidence{CWD: "/repo", Head: v.Batch.TargetCommit, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano), Edges: edges}); err != nil {
		t.Fatal(err)
	}
}

func assertSubscriptionReceipt(t *testing.T, event groupEvent, id string, dispatch board.Dispatch, target string) {
	t.Helper()
	fact := event.Dispatches[id]
	if fact.ID != dispatch.Input.ID || fact.TaskID != id || fact.Epoch != dispatch.Authorization.Epoch || fact.State != board.DispatchCompleted || fact.Accepted == nil || fact.Completed == nil {
		t.Fatalf("missing durable completion: %+v", fact)
	}
	if fact.Completed.State != target || fact.Completed.DeliveryCommit != strings.Repeat("b", 40) || fact.Accepted.CardRevision >= fact.Completed.CardRevision || fact.Completed.CardRevision != event.TaskRevisions[id] || fact.Revision != 3 || fact.ConfirmationPending || fact.ConfirmationOverdue {
		t.Fatalf("receipt binding: %+v / %+v", fact, event)
	}
	raw, err := json.Marshal(event)
	if err != nil || strings.Contains(string(raw), dispatch.Input.Message) {
		t.Fatalf("summary exposed payload: %s %v", raw, err)
	}
}

func TestSubscriptionFastDispatchWrapUp(t *testing.T) {
	root, opts, id := factsMember(t)
	if err := factsUpdate(root, id, func(text string) string {
		body, _ := board.SectionBody(text, board.SectionSummary)
		return strings.Replace(text, "## SUMMARY\n\n"+body, "## SUMMARY\n\nLifecycle fixture completed.", 1)
	}); err != nil {
		t.Fatal(err)
	}
	closeSubscriptionFixture(t, root, id)
	opts.Refresh, opts.Heartbeat = 1, 1.5
	dispatch := prepareSubscriptionDispatch(t, root, id, "wrap-up", time.Minute)
	events, err := factsEvents(t, root, opts, func(event groupEvent) error {
		if event.Event == "snapshot" {
			completeSubscriptionDispatch(t, root, id, dispatch, "done")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 || events[1].Event != "state-change" {
		t.Fatalf("unexpected events: %+v", events)
	}
	assertSubscriptionReceipt(t, events[1], id, dispatch, "done")
	// First snapshot after completion needs no historical working edge.
	restarted, err := factsEvents(t, root, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	assertSubscriptionReceipt(t, restarted[0], id, dispatch, "done")
}

// Reverse ReviewHasNoLiveness: pending review is monitored, legacy review is not.
func TestAuditReviewHasNoLiveness(t *testing.T) {
	for _, status := range []string{Alive, Stopped, Unknown} {
		t.Run(status, func(t *testing.T) {
			root, opts, id := factsMember(t)
			setLocation(t, currentEntry(t, root, id).Document, "codex wanted", "herdr:w1:t1:w1:p0")
			installPOSIXFakes(t, true)
			script := "#!/bin/sh\necho '{\"result\":{\"pane\":{\"pane_id\":\"w1:p0\",\"agent\":\"codex\",\"agent_status\":\"working\",\"agent_session\":{\"value\":\"wanted\"}}}}'\n"
			if status == Stopped {
				script = "#!/bin/sh\nif [ \"$2\" = list ]; then echo '{\"result\":{\"panes\":[]}}'; else echo '{\"id\":\"cli:pane:get\",\"error\":{\"code\":\"pane_not_found\",\"message\":\"gone\"}}' >&2; exit 1; fi\n"
			} else if status == Unknown {
				script = "#!/bin/sh\necho invalid-json\n"
			}
			if err := os.WriteFile(filepath.Join(os.Getenv("PATH"), "herdr"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			dispatch := prepareSubscriptionDispatch(t, root, id, "sync", 80*time.Millisecond)
			opts.Refresh, opts.Heartbeat = 3600, 3600
			events := make(runtimeEvents, 32)
			cancel, done := runRuntimeSubscription(t, root, opts, events)
			attention := nextRuntimeEvent(t, events, func(e groupEvent) bool {
				live := e.Liveness[id]
				return e.Event == "dispatch-attention" && live.Status == status && live.ObservedAt != nil
			})
			if !attention.Dispatches[id].ConfirmationOverdue || !attention.Dispatches[id].ConfirmationPending || attention.Dispatches[id].AgeSeconds < 60 || !attention.ReconciliationRequired || attention.Tasks[id] != "review" || len(attention.Attention) != 1 {
				t.Fatalf("attention: %+v", attention)
			}
			if !attention.Dispatches[id].ConfirmBy.Equal(dispatch.Input.ConfirmBy) || attention.Liveness[id].Revision != attention.TaskRevisions[id] {
				t.Fatal("deadline or observation binding changed")
			}
			cancel()
			<-done
			current, err := board.ReadDispatch(root, id, dispatch.Input.ID)
			if err != nil || current.State != board.DispatchPrepared || current.Accepted != nil || current.Attempts != 0 {
				t.Fatalf("subscription performed recovery/acceptance: %+v %v", current, err)
			}
			// Restart after expiry must immediately diagnose the same deadline.
			restarted := make(runtimeEvents, 16)
			cancel, done = runRuntimeSubscription(t, root, opts, restarted)
			initial := nextRuntimeEvent(t, restarted, func(e groupEvent) bool { return e.Event == "snapshot" })
			nextRuntimeEvent(t, restarted, func(e groupEvent) bool { return e.Event == "dispatch-attention" })
			if !initial.Dispatches[id].ConfirmationOverdue || !initial.Dispatches[id].ConfirmBy.Equal(dispatch.Input.ConfirmBy) {
				t.Fatal("restart reset confirmation")
			}
			cancel()
			<-done
		})
	}
}

func TestSubscriptionDispatchDeadlineSurvivesOtherEvents(t *testing.T) {
	root, opts, id := factsMember(t)
	external := addFactsExternal(t, root, "dispatch-changing", "20260908-external-group")
	opts.Watch = []string{external}
	opts.Refresh, opts.Heartbeat = .005, 3600
	dispatch := prepareSubscriptionDispatch(t, root, id, "sync", 200*time.Millisecond)
	if _, err := board.BeginDispatchAttempt(root, id, dispatch.Input.ID, dispatch.Revision); err != nil {
		t.Fatal(err)
	}
	var ticks atomic.Int64
	oldNow := nowFn
	nowFn = func() time.Time {
		return dispatch.Input.ConfirmBy.Add(-200*time.Millisecond + time.Duration(ticks.Load())*40*time.Millisecond)
	}
	t.Cleanup(func() { nowFn = oldNow })
	events := make(runtimeEvents, 64)
	cancel, done := runRuntimeSubscription(t, root, opts, events)
	changes := 0
	for {
		event := nextRuntimeEvent(t, events, func(e groupEvent) bool { return true })
		if event.Event == "dispatch-attention" {
			if event.Dispatches[id].State != board.DispatchUnknown || event.Dispatches[id].Accepted != nil || changes < 3 || !event.Dispatches[id].ConfirmBy.Equal(dispatch.Input.ConfirmBy) || event.ObservedAt.Sub(dispatch.Input.ConfirmBy) >= 40*time.Millisecond {
				t.Fatalf("deadline postponed: changes=%d event=%+v", changes, event)
			}
			break
		}
		if event.Event == "task-update" {
			changes++
			ticks.Add(1)
		}
		if err := factsUpdate(root, external, func(s string) string { return s + "\nOther update.\n" }); err != nil {
			t.Fatal(err)
		}
	}
	cancel()
	<-done
}

func TestSubscriptionDispatchCorruptionFailsClosed(t *testing.T) {
	root, opts, id := factsMember(t)
	dispatch := prepareSubscriptionDispatch(t, root, id, "sync", time.Minute)
	path := filepath.Join(currentEntry(t, root, id).Path, "dispatches", dispatch.Input.ID, "state.json")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	events := make(runtimeEvents, 8)
	err := SubscribeContext(context.Background(), root, opts, events)
	if err == nil {
		t.Fatal("missing dispatch read as no dispatch")
	}
	event := <-events
	if event.ReadStatus != "invalid" || !event.ReconciliationRequired || event.MembershipComplete || len(event.Dispatches) != 0 {
		t.Fatalf("corrupt facts accepted: %+v", event)
	}
}
