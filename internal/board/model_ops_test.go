package board

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	modelExecutor = "codex"
	modelTaker    = "claude"
	modelTailOp   = "F.done"
)

// modelOp is one supported command or launcher step. cli names the command a
// failure report points at. A shared op runs once per state; any other op runs
// once per member. guard reads the facts of the state; apply runs on a private
// copy, and an error means the command refused and the copy is discarded.
type modelOp struct {
	name     string
	cli      string
	shared   bool
	abandons bool     // an archive or a trash gives the card up; it is never a way out
	narrows  []string // the only operations the search tries after this one
	guard    func(f *modelFacts, i int) bool
	apply    func(w *modelWorld, m *modelMember, f *modelFacts) error
}

// modelAfterAbandon limits the search behind an archive or a trash. Every dead
// end of a terminal member shows at the next tail or reconcile.
var modelAfterAbandon = []string{modelTailOp, "C.reconcile", "T.archive", "T.trash"}

func modelInState(states ...string) func(*modelFacts, int) bool {
	return func(f *modelFacts, i int) bool {
		for _, state := range states {
			if f.cards[i].Entry.State == state {
				return true
			}
		}
		return false
	}
}

func modelLive(f *modelFacts, i int) bool {
	m := f.world.members[i]
	return m.executor || m.self
}

// modelLauncherFail is launchAgent's fail(): the container closes first, then
// rollbackLaunch restores the todo text through the launcher's cursor.
func modelLauncherFail(w *modelWorld, m *modelMember, cause string) error {
	s, err := w.snapshot(m)
	if err != nil {
		return err
	}
	m.executor = false
	entry := w.launcherEntry(m, s)
	m.launcher = nil
	if err := RollbackDocument(w.root, entry, m.original, "todo"); err != nil {
		m.stranded = cause
	} else {
		m.stranded = ""
	}
	return nil
}

func modelLifecycleOps() []modelOp {
	return []modelOp{
		{name: "L.begin", cli: "kander start <id>: claim and start metadata, before the agent runs",
			guard: func(f *modelFacts, i int) bool {
				return f.cards[i].Entry.State == "todo" && f.world.members[i].launcher == nil
			},
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				s, err := w.snapshot(m)
				if err != nil {
					return err
				}
				moved, err := BeginTaskStart(s.Entry, w.root, TaskStartInput{Owner: modelExecutor, Session: modelExecutor})
				if err != nil {
					return err
				}
				m.launcher = &modelCursor{state: moved.State, revision: moved.Version.revision, auth: moved.Version.authorization, fence: moved.Version.fence}
				m.executor, m.self, m.original, m.stranded = true, false, s.Text, ""
				return nil
			}},
		{name: "L.confirm", cli: "kander start: ConfirmTaskStart after launch",
			guard: func(f *modelFacts, i int) bool { return f.world.members[i].launcher != nil },
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				s, err := w.snapshot(m)
				if err != nil {
					return err
				}
				entry := w.launcherEntry(m, s)
				m.launcher = nil
				if err := ConfirmTaskStart(w.root, entry); err != nil {
					m.stranded = "confirm-binding"
				}
				return nil
			}},
		{name: "L.confirmSession", cli: "kander start: ConfirmTaskStartSession after discovery; failure closes the container and rolls back",
			guard: func(f *modelFacts, i int) bool { return f.world.members[i].launcher != nil },
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				s, err := w.snapshot(m)
				if err != nil {
					return err
				}
				if err := ConfirmTaskStartSession(w.root, w.launcherEntry(m, s), modelExecutor, modelExecutor+" exact-session"); err != nil {
					cause := "confirm-session"
					if authFrom(s.Text).DispatchID != "" {
						cause = "confirm-dispatch"
					}
					return modelLauncherFail(w, m, cause)
				}
				m.launcher = nil
				return nil
			}},
		{name: "L.fail", cli: "kander start: late launch failure; closes the container and rolls back",
			guard: func(f *modelFacts, i int) bool { return f.world.members[i].launcher != nil },
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				return modelLauncherFail(w, m, "rollback")
			}},
		{name: "X.write", cli: "executor: kander update <id> --document spec.md --expect-revision <rev>",
			guard: func(f *modelFacts, i int) bool { return modelLive(f, i) && f.cards[i].Entry.State == "working" },
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				s, err := w.snapshot(m)
				if err != nil {
					return err
				}
				return UpdateDocument(w.root, m.id, UpdateOptions{Document: "spec.md", Text: strings.TrimRight(s.Text, "\n") + "\n\n- progress\n", ExpectedRevision: s.Revision, Authorization: authFrom(s.Text)})
			}},
		{name: "X.done", cli: "kander move <id> done --result completed, with no dispatch",
			guard: func(f *modelFacts, i int) bool {
				return f.world.group && modelLive(f, i) && f.cards[i].Entry.State == "working" && f.bound[i] == nil
			},
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error { return w.plainDone(m) }},
		{name: "X.review", cli: "executor: kander move <id> review",
			guard: func(f *modelFacts, i int) bool {
				return modelLive(f, i) && f.cards[i].Entry.State == "working" && f.bound[i] == nil
			},
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				s, err := w.snapshot(m)
				if err != nil {
					return err
				}
				_, err = MoveEntry(s.Entry, w.root, "review")
				return err
			}},
		{name: "X.exit", cli: "executor process stops",
			guard: func(f *modelFacts, i int) bool { return f.world.members[i].executor },
			apply: func(_ *modelWorld, m *modelMember, _ *modelFacts) error { m.executor = false; return nil }},
		{name: "H.takeover", cli: "kander resume --agent <agent> --message ... <id>",
			guard: modelInState("working"),
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error { return modelTakeover(w, m) }},
		{name: "H.takeoverBound", cli: "kander resume --agent <agent> --dispatch-id <id>: ReauthorizeDispatch, then the takeover metadata",
			guard: func(f *modelFacts, i int) bool {
				return f.cards[i].Entry.State == "working" && f.bound[i] != nil && !f.world.members[i].executor
			},
			apply: func(w *modelWorld, m *modelMember, f *modelFacts) error {
				s, err := w.snapshot(m)
				if err != nil {
					return err
				}
				d := f.bound[f.index(m)]
				if d.State == DispatchAccepted {
					_, err = ReauthorizeDispatch(w.root, m.id, d.Input.ID, d.Revision, modelStopped(s))
				} else {
					_, err = ReauthorizeDispatch(w.root, m.id, d.Input.ID, d.Revision)
				}
				if err != nil {
					return err
				}
				return modelTakeover(w, m)
			}},
		{name: "H.claim", cli: "kander move <id> working --owner <agent>",
			guard: modelInState("todo"),
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				return modelClaim(w, m, MoveOptions{Owner: modelTaker})
			}},
		{name: "H.plainWorking", cli: "kander move <id> working",
			guard: modelInState("todo"),
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error { return modelClaim(w, m, MoveOptions{}) }},
		{name: "H.reclaim", cli: "kander move <id> working --owner <agent>, from review",
			guard: modelInState("review"),
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				return modelClaim(w, m, MoveOptions{Owner: modelTaker})
			}},
		{name: "H.reclaimDecision", cli: "kander move <id> working --owner <agent> --decision <ref>",
			guard: modelInState("review"),
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				return modelClaim(w, m, MoveOptions{Owner: modelTaker, Decision: "model-decision"})
			}},
		{name: "H.return", cli: "self-executed group: kander move <id> working",
			guard: func(f *modelFacts, i int) bool {
				return f.world.members[i].self && f.cards[i].Entry.State == "review" && f.bound[i] == nil
			},
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				s, err := w.snapshot(m)
				if err != nil {
					return err
				}
				_, err = MoveEntry(s.Entry, w.root, "working")
				return err
			}},
		{name: "T.archive", cli: "kander move <id> archived --result ... --reason ... --decision ...", abandons: true, narrows: modelAfterAbandon,
			guard: modelInState("backlog", "todo", "working", "review", "done"),
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				s, err := w.snapshot(m)
				if err != nil {
					return err
				}
				result := "cancelled"
				if s.Entry.State == "done" {
					result = "completed"
				}
				_, err = MoveWithOptions(s.Entry, w.root, "archived", MoveOptions{Result: result, Reason: "model", Decision: "model-decision"})
				if err == nil {
					m.executor, m.self = false, false
				}
				return err
			}},
		{name: "T.trash", cli: "kander move <id> trash --result trashed --reason ... --decision ...", abandons: true, narrows: modelAfterAbandon,
			guard: modelInState("backlog", "todo", "working", "review", "done"),
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				s, err := w.snapshot(m)
				if err != nil {
					return err
				}
				_, err = MoveWithOptions(s.Entry, w.root, "trash", MoveOptions{Result: "trashed", Reason: "model", Decision: "model-decision"})
				if err == nil {
					m.executor, m.self = false, false
				}
				return err
			}},
		{name: "U.regroup", cli: "kander update <id> --document spec.md --contract-decision-file <f>: rewrite TASK_GROUP; a checkpoint refuses it",
			guard: func(f *modelFacts, i int) bool {
				return f.world.group && !f.review.planned() && f.bound[i] == nil && f.cards[i].Entry.State != "archived"
			},
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				s, err := w.snapshot(m)
				if err != nil {
					return err
				}
				return modelRegroup(w, m, s)
			}},
		{name: modelTailOp, cli: "the completion tail of one card, ending in kander move <id> done",
			guard: func(f *modelFacts, i int) bool {
				return !modelTerminal(f.cards[i].Entry.State)
			},
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error { return w.finishMember(m) }},
		{name: "C.reconcile", cli: "kander coordinator reconcile <observations.json>", shared: true,
			guard: func(f *modelFacts, _ int) bool { return f.ckpt != nil },
			apply: func(w *modelWorld, _ *modelMember, _ *modelFacts) error { return w.reconcile() }},
		{name: "C.claim", cli: "kander coordinator claim <claim.json>: a new coordinator session fences the old one", shared: true,
			guard: func(f *modelFacts, _ int) bool { return f.ckpt != nil },
			apply: func(w *modelWorld, _ *modelMember, f *modelFacts) error {
				_, err := ClaimCoordinator(context.Background(), w.root, CoordinatorClaim{GroupID: coordinatorGroup, ExpectedRevision: f.ckpt.Revision, ExpectedEpoch: f.ckpt.Authority.Epoch, Owner: "coordinator", Token: w.nextID("session"), Basis: "model recovery", Members: checkpointIDs(*f.ckpt)})
				return err
			}},
	}
}

// modelRegroup moves the card to another group.
func modelRegroup(w *modelWorld, m *modelMember, s Snapshot) error {
	text, err := setMetadata(s.Text, FieldTaskGroup, "20261030-model-other-group")
	if err != nil {
		return modelInfra(err)
	}
	return UpdateDocument(w.root, m.id, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision, ContractDecision: "model group change", Authorization: authFrom(s.Text)})
}

func modelTakeover(w *modelWorld, m *modelMember) error {
	s, err := w.snapshot(m)
	if err != nil {
		return err
	}
	if _, err = BindExecutionCursor(w.root, s.Entry); err != nil {
		return err
	}
	if _, err = BeginTakeover(w.root, s.Entry, TakeoverInput{Owner: modelTaker, Session: modelTaker + " takeover-session", Decision: "model-decision"}); err != nil {
		return err
	}
	if err = ConfirmTaskStart(w.root, s.Entry); err != nil {
		return err
	}
	m.executor, m.self = true, false
	return nil
}

func modelClaim(w *modelWorld, m *modelMember, o MoveOptions) error {
	s, err := w.snapshot(m)
	if err != nil {
		return err
	}
	if _, err = MoveWithOptions(s.Entry, w.root, "working", o); err != nil {
		return err
	}
	m.self, m.executor = true, false
	return nil
}

func modelStopped(s Snapshot) WrapUpExitEvidence {
	return WrapUpExitEvidence{Outcome: "stopped", CardRevision: s.Revision, Session: MetadataFrom(s.Text, FieldSession), Window: MetadataFrom(s.Text, FieldWindow), Owner: MetadataFrom(s.Text, FieldOwner), StartedAt: MetadataFrom(s.Text, FieldStartedAt), ObservedAt: time.Now().UTC()}
}

func (f *modelFacts) index(m *modelMember) int {
	for i, member := range f.world.members {
		if member.id == m.id {
			return i
		}
	}
	return -1
}

func modelDispatchOps() []modelOp {
	prepare := func(name, cli string, build func(w *modelWorld, m *modelMember) (DispatchInput, error), guard func(*modelFacts, int) bool) modelOp {
		return modelOp{name: name, cli: cli,
			guard: func(f *modelFacts, i int) bool {
				if state := f.cards[i].Entry.State; state != "working" && state != "review" {
					return false
				}
				return (f.bound[i] == nil || coordinatorDispatchTerminal(f.bound[i].State)) && guard(f, i)
			},
			apply: func(w *modelWorld, m *modelMember, _ *modelFacts) error {
				in, err := build(w, m)
				if err != nil {
					return err
				}
				_, err = PrepareDispatch(w.root, in)
				return err
			}}
	}
	active := func(f *modelFacts, i int) bool {
		return f.bound[i] != nil && !coordinatorDispatchTerminal(f.bound[i].State)
	}
	end := func(state DispatchState) func(*modelWorld, *modelMember, *modelFacts) error {
		return func(w *modelWorld, m *modelMember, f *modelFacts) error {
			d := f.bound[f.index(m)]
			return EndDispatch(w.root, m.id, d.Input.ID, d.Revision, state, "model", "model-decision")
		}
	}
	return []modelOp{
		prepare("D.prepareSync", "kander dispatch prepare (sync)", func(w *modelWorld, m *modelMember) (DispatchInput, error) {
			return w.dispatchInput(m, "sync", modelCommit(1)), nil
		}, func(*modelFacts, int) bool { return true }),
		prepare("D.prepareFix", "kander dispatch prepare (fix)", (*modelWorld).fixInput, func(f *modelFacts, i int) bool {
			_, _, ok := f.review.fixTarget(f.world.members[i].id)
			return ok
		}),
		prepare("D.prepareWrapUp", "kander dispatch prepare (wrap-up)", (*modelWorld).wrapUpInput, func(f *modelFacts, i int) bool {
			return f.review.closedFor(f.world.members[i].id)
		}),
		{name: "D.accept", cli: "executor: kander move <id> working --dispatch-id <id> --execution-epoch <n>",
			guard: func(f *modelFacts, i int) bool {
				return active(f, i) && f.bound[i].State == DispatchPrepared
			},
			apply: func(w *modelWorld, m *modelMember, f *modelFacts) error {
				return w.acceptDispatch(m, f.bound[f.index(m)])
			}},
		{name: "D.complete", cli: "executor: kander move <id> review|done --dispatch-id <id> --execution-epoch <n> --delivery-commit <sha>",
			guard: func(f *modelFacts, i int) bool {
				return active(f, i) && f.bound[i].State == DispatchAccepted && modelLive(f, i)
			},
			apply: func(w *modelWorld, m *modelMember, f *modelFacts) error {
				return w.completeDispatch(m, f.bound[f.index(m)])
			}},
		{name: "D.fail", cli: "kander dispatch fail <id> <dispatch> <revision> <reason>", guard: active, apply: end(DispatchFailed)},
		{name: "D.cancel", cli: "kander dispatch cancel <id> <dispatch> <revision> <reason>", guard: active, apply: end(DispatchCancelled)},
		{name: "D.settle", cli: "kander dispatch settle <id> <dispatch> <revision>",
			guard: func(f *modelFacts, i int) bool {
				state := f.cards[i].Entry.State
				return f.bound[i] != nil && f.bound[i].State == DispatchCompleted && (state == "working" || state == "review")
			},
			apply: func(w *modelWorld, m *modelMember, f *modelFacts) error {
				d := f.bound[f.index(m)]
				return SettleDispatch(w.root, m.id, d.Input.ID, d.Revision)
			}},
		{name: "D.authorizeWrapUp", cli: "kander dispatch authorize-wrap-up <request.json>",
			guard: func(f *modelFacts, i int) bool {
				d := f.bound[i]
				return active(f, i) && d.Input.Kind == "wrap-up" && d.WrapUpAuthority == nil && !f.world.members[i].executor
			},
			apply: func(w *modelWorld, m *modelMember, f *modelFacts) error {
				s, err := w.snapshot(m)
				if err != nil {
					return err
				}
				d := f.bound[f.index(m)]
				if _, err = AuthorizeDispatchWrapUp(w.root, m.id, d.Input.ID, d.Revision, "coordinator", "executor stopped", modelStopped(s)); err != nil {
					return err
				}
				m.self = true
				return nil
			}},
	}
}

func modelCommit(n int) string { return strings.Repeat(fmt.Sprintf("%x", 0xa+n), 40) }

func (w *modelWorld) dispatchInput(m *modelMember, kind, base string) DispatchInput {
	created, confirm := modelDispatchWindow()
	return DispatchInput{ID: w.nextID("dispatch"), TaskID: m.id, Kind: kind, Message: "model dispatch", Base: base, CreatedAt: created, ConfirmBy: confirm}
}

// fixTarget finds the newest finding assigned to task in an open batch.
func (r modelReviewFacts) fixTarget(task string) (modelBatchFacts, modelRunFacts, bool) {
	for _, b := range r.batches {
		if b.closed {
			continue
		}
		for _, run := range b.runs {
			for _, finding := range run.findings {
				if containsID(run.assigned[finding], task) && !run.recorded(finding, task) {
					return b, run, true
				}
			}
		}
	}
	return modelBatchFacts{}, modelRunFacts{}, false
}

func (r modelReviewFacts) closedFor(task string) bool {
	seen := false
	for _, b := range r.batches {
		if containsID(b.members, task) {
			if !b.closed {
				return false
			}
			seen = true
		}
	}
	return seen
}

func (w *modelWorld) fixInput(m *modelMember) (DispatchInput, error) {
	review, err := w.readReview()
	if err != nil {
		return DispatchInput{}, err
	}
	b, run, ok := review.fixTarget(m.id)
	if !ok {
		return DispatchInput{}, reviewError("no assigned finding")
	}
	in := w.dispatchInput(m, "fix", b.target)
	binding := &DispatchFixBinding{BatchID: b.id}
	for _, finding := range run.findings {
		if containsID(run.assigned[finding], m.id) {
			binding.Findings = append(binding.Findings, DispatchFindingReference{FindingRef: FindingRef{run.id, finding}, PreviousRunID: run.previous})
		}
	}
	for _, r := range run.records {
		if r.TaskID == m.id {
			binding.Authors = append(binding.Authors, DispatchAuthorReference{Finding: FindingRef{run.id, r.FindingID}, RecordID: r.RecordID, Author: r.Author, Artifact: ArtifactReference{m.id, dispositionPath(r)}})
		}
	}
	in.Evidence.Fix = binding
	return in, nil
}

func (w *modelWorld) wrapUpInput(m *modelMember) (DispatchInput, error) {
	reviewRange, err := DispatchReviewRange(w.root, m.id)
	if err != nil {
		return DispatchInput{}, err
	}
	in := w.dispatchInput(m, "wrap-up", reviewRange.Descendant)
	in.Evidence.WrapUp = &DispatchWrapUpBinding{Artifact: ArtifactReference{m.id, dispatchPath(in.ID, "integration")}, Git: DispatchIntegration{DispatchID: in.ID, TaskID: m.id, CWD: w.root, SourceCommit: in.Base, ReviewTarget: in.Base, ReviewBase: reviewRange.Ancestor, TargetCommit: in.Base, TargetRef: "refs/heads/develop", Author: "coordinator", Basis: "structural model; no Git verification claimed", VerifiedAt: time.Now().UTC()}}
	return in, nil
}

func (w *modelWorld) acceptDispatch(m *modelMember, d *Dispatch) error {
	s, err := w.snapshot(m)
	if err != nil {
		return err
	}
	_, err = MoveWithOptions(s.Entry, w.root, "working", MoveOptions{Authorization: d.Authorization})
	return err
}

func (w *modelWorld) completeDispatch(m *modelMember, d *Dispatch) error {
	if d.Input.Kind == "wrap-up" {
		if err := w.writeSummary(m); err != nil {
			return err
		}
		s, err := w.snapshot(m)
		if err != nil {
			return err
		}
		_, err = MoveWithOptions(s.Entry, w.root, "done", MoveOptions{Authorization: d.Authorization, DeliveryCommit: d.Input.Base, Result: "completed"})
		return err
	}
	s, err := w.snapshot(m)
	if err != nil {
		return err
	}
	_, err = MoveWithOptions(s.Entry, w.root, "review", MoveOptions{Authorization: d.Authorization, DeliveryCommit: w.nextCommit()})
	return err
}

// plainDone completes an unbound card its owner holds: a single card, or a member
// of a self-executed group.
func (w *modelWorld) plainDone(m *modelMember) error {
	if err := w.settleReview(m); err != nil {
		return err
	}
	if err := w.writeSummary(m); err != nil {
		return err
	}
	s, err := w.snapshot(m)
	if err != nil {
		return err
	}
	_, err = MoveWithOptions(s.Entry, w.root, "done", MoveOptions{Result: "completed"})
	return err
}

// nextRoundInput is the orchestrator's next dispatch for a card in review: a fix
// round while the card owes a finding, otherwise wrap-up over a settled plan.
func (w *modelWorld) nextRoundInput(m *modelMember) (DispatchInput, error) {
	review, err := w.readReview()
	if err != nil {
		return DispatchInput{}, err
	}
	if b, ok := review.openFor(m.id); ok {
		for _, r := range b.runs {
			if r.assigned != nil {
				continue
			}
			if err := w.assign(r, b.id, m.id); err != nil {
				return DispatchInput{}, err
			}
		}
		if review, err = w.readReview(); err != nil {
			return DispatchInput{}, err
		}
	}
	if _, _, owes := review.fixTarget(m.id); owes {
		return w.fixInput(m)
	}
	if err := w.settleReview(m); err != nil {
		return DispatchInput{}, err
	}
	return w.wrapUpInput(m)
}

func (w *modelWorld) writeSummary(m *modelMember) error {
	s, err := w.snapshot(m)
	if err != nil {
		return err
	}
	text := strings.Replace(s.Text, "## SUMMARY\n\nfixture", "## SUMMARY\n\nDone.", 1)
	if text == s.Text {
		return nil
	}
	return UpdateDocument(w.root, m.id, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision, Authorization: authFrom(s.Text)})
}

// finishMember is the completion tail of one card: claim an unstarted card, let
// a running launcher confirm, take over a card with no live owner, then deliver.
// A single card ends with a plain move done; a member goes through review and a
// wrap-up dispatch. The tail uses ordinary commands only. A state it cannot
// finish is left to the exit search, which puts other operations in front of it.
func (w *modelWorld) finishMember(m *modelMember) error {
	for range 10 {
		s, err := w.snapshot(m)
		if err != nil {
			return err
		}
		if m.launcher != nil && !modelTerminal(s.Entry.State) {
			entry := w.launcherEntry(m, s)
			m.launcher = nil
			// A launcher a takeover overtook ends here; the new execution goes on.
			if err := ConfirmTaskStart(w.root, entry); err != nil {
				m.stranded = "confirm-binding"
			}
			continue
		}
		if s.Entry.State == "todo" {
			if err := modelClaim(w, m, MoveOptions{Owner: modelTaker}); err != nil {
				return err
			}
			continue
		}
		if s.Entry.State == "working" && !m.self && !m.executor {
			if err := modelTakeover(w, m); err != nil {
				return err
			}
			continue
		}
		var d *Dispatch
		if a := authFrom(s.Text); a.DispatchID != "" {
			read, err := ReadDispatch(w.root, m.id, a.DispatchID)
			if err != nil {
				return modelInfra(err)
			}
			d = &read
		}
		if modelTerminal(s.Entry.State) {
			return nil
		}
		switch {
		case d != nil && d.State == DispatchPrepared:
			err = w.acceptDispatch(m, d)
		case d != nil && d.State == DispatchAccepted:
			if d.Input.Kind == "wrap-up" {
				return w.completeDispatch(m, d)
			}
			if err = w.settleReview(m); err == nil {
				err = w.completeDispatch(m, d)
			}
		case s.Entry.State == "working" && !w.group:
			return w.plainDone(m)
		case s.Entry.State == "working":
			_, err = MoveEntry(s.Entry, w.root, "review")
		case d == nil && m.self && !w.group:
			_, err = MoveEntry(s.Entry, w.root, "working")
		default:
			var in DispatchInput
			if in, err = w.nextRoundInput(m); err == nil {
				_, err = PrepareDispatch(w.root, in)
			}
		}
		if err != nil {
			return err
		}
	}
	return errors.New("completion tail did not converge")
}
