package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/statemachine/serverclaim"
)

// project is one registered project: its board below the data directory and
// the control records the server keeps beside it. mu serializes every write,
// so ownership checks, board transactions, receipts, and events of one
// project never interleave.
type project struct {
	mu     sync.Mutex
	server *Server
	info   api.Project
	root   string
	board  string
	events *eventLog
	starts map[string]startCursor
}

func (p *project) log() *slog.Logger { return p.server.log.With("project", p.info.ID) }

// open prepares the board, finishes interrupted board transactions, and
// settles claims an interrupted operation left behind.
func (p *project) open() error {
	if err := ensureDir(p.root, "control"); err != nil {
		return err
	}
	if err := p.recoverImport(); err != nil {
		return err
	}
	if err := board.InitStorage(p.board); err != nil {
		return err
	}
	if request := p.server.maintenance; request != nil && request.Project == p.info.Name {
		count, err := board.MigrateCards(p.board, board.InitOptions{Maintenance: request.Acknowledged})
		if err != nil {
			return err
		}
		p.server.maintenanceChanged = count
	} else if err := board.RecoverTransactions(p.board); err != nil {
		return err
	}
	events, err := openEventLog(p.root)
	if err != nil {
		return err
	}
	p.events = events
	p.starts = map[string]startCursor{}
	return p.reconcileClaims()
}

// reconcileClaims derives every claim from the committed card state: a
// pending claim whose move committed becomes active, one whose move did not
// is released, and a claim of a card that left working/review is released.
func (p *project) reconcileClaims() error {
	if err := ensureDir(p.root, servicePath("claims")); err != nil {
		return err
	}
	view, err := board.LoadBoard(p.board)
	if err != nil {
		return err
	}
	for id := range view.Entries {
		r, found, err := p.readClaim(id)
		if err != nil || !found {
			if err != nil {
				return err
			}
			continue
		}
		state := view.Entries[id].State
		k, err := p.projectClaim(r, true, claimedState(state))
		if err != nil {
			return err
		}
		before := k.state.Status
		owner := r.Status == claimPending && claimedState(state) && p.ownerMatches(view.Entries[id], r)
		if !k.step(serverclaim.Reconcile{Owner: owner}) {
			return apiError(api.CodeLeaseInvalid, "claim reconciliation refused for task "+id)
		}
		changed := k.state.Status != before
		switch k.state.Status {
		case serverclaim.StatusActive:
			if changed {
				r.Status, r.RenewedAt, r.ExpiresAt = api.ClaimActive, p.server.now(), p.server.now().Add(p.server.ttl)
			}
		case serverclaim.StatusReleased:
			r.Status = api.ClaimReleased
		}
		if changed {
			p.log().Info("claim reconciled", "task", id, "generation", r.Generation, "status", r.Status)
			if err := p.writeClaim(r); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *project) ownerMatches(entry board.Entry, r claimRecord) bool {
	text, err := board.ReadDocument(entry)
	return err == nil && board.MetadataFrom(text, board.FieldOwner) == r.Owner
}

func (p *project) list(ctx context.Context, state string) (api.TaskList, error) {
	cursor := p.events.cursor()
	view, err := board.BoardPayloadContext(ctx, p.board)
	if err != nil {
		return api.TaskList{}, err
	}
	now := p.server.now()
	out := api.TaskList{ObservedAt: now, Tasks: []api.TaskSummary{}, Cursor: cursor, Warnings: view.Warnings}
	for _, t := range view.Tasks {
		if state != "" && t.State != state {
			continue
		}
		s := api.TaskSummary{
			TaskID: t.TaskID, Title: t.Title, State: t.State, Kind: t.Kind, Type: t.Type, Session: t.Session, Window: t.Window,
			TaskGroup: t.TaskGroup, Assignee: t.Assignee, CreatedAt: t.CreatedAt,
			StartedAt: t.StartedAt, CompletedAt: t.CompletedAt, Time: t.Time, Result: t.Result,
		}
		snapshot, err := board.ReadSnapshot(p.board, t.TaskID)
		if err != nil {
			return api.TaskList{}, err
		}
		s.Revision = snapshot.Revision
		if r, found, err := p.readClaim(t.TaskID); err != nil {
			return api.TaskList{}, err
		} else if found && r.Status != claimPending {
			s.Claim = r.view(now)
		}
		out.Tasks = append(out.Tasks, s)
	}
	return out, nil
}

func (p *project) show(id string) (api.Task, error) {
	s, err := board.ReadSnapshot(p.board, id)
	if err != nil {
		return api.Task{}, err
	}
	t := api.Task{ObservedAt: p.server.now(), TaskID: s.Entry.TaskID, State: s.Entry.State, Revision: s.Revision, OperationID: s.OperationID, Text: s.Text}
	t.Delivery, err = p.latestDelivery(s.Entry.TaskID)
	if err != nil {
		return api.Task{}, err
	}
	r, found, err := p.readClaim(s.Entry.TaskID)
	if err != nil {
		return api.Task{}, err
	}
	if found && r.Status != claimPending {
		t.Claim = r.view(p.server.now())
	}
	return t, nil
}

func (p *project) document(id, path string) (api.Document, error) {
	s, err := board.ReadSnapshot(p.board, id)
	if err != nil {
		return api.Document{}, err
	}
	if path == "spec.md" {
		return api.Document{Path: path, Text: s.Text, SHA256: digest(s.Text), ExpectedRevision: s.Revision}, nil
	}
	data, ok, err := board.ReadCardFile(s.Entry, path)
	if err != nil {
		return api.Document{}, err
	}
	if !ok {
		return api.Document{}, apiError(api.CodeNotFound, "document "+path+" does not exist")
	}
	return api.Document{Path: path, Text: string(data), SHA256: digest(string(data)), ExpectedRevision: s.Revision}, nil
}

func digest(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// changed publishes a task event after a committed board write.
func (p *project) changed(id string, generation uint64) {
	s, err := board.ReadSnapshot(p.board, id)
	e := api.Event{Kind: api.EventTaskChanged, TaskID: id, Generation: generation}
	if err == nil {
		e.State, e.Revision = s.Entry.State, s.Revision
	}
	if err := p.events.append(e); err != nil {
		p.log().Error("event append failed", "task", id, "error", err)
	}
}

func (p *project) newTask(key string, req api.NewTask) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	run := func() outcome {
		var created api.Task
		extension := board.TransactionExtension{Groups: []string{serviceGroup}, After: func(tx *board.Transaction) error {
			snapshot, ok := tx.OperationResult().(board.Snapshot)
			if !ok {
				return apiError(api.CodeInternal, "creation did not stage its initial snapshot")
			}
			created = api.Task{ObservedAt: p.server.now(), TaskID: snapshot.Entry.TaskID, State: snapshot.Entry.State, Revision: snapshot.Revision, OperationID: snapshot.OperationID, Text: snapshot.Text}
			return p.stageOperationReceiptStatus(tx, key, "new", req, http.StatusCreated, created)
		}}
		_, err := board.NewTaskWithTransaction(p.board, req.Type, req.Slug, req.Title, req.Language, req.Large, extension)
		if err != nil {
			return outcome{err: err}
		}
		p.changed(created.TaskID, 0)
		if p.server.createFault != nil {
			p.server.createFault(created)
		}
		return outcome{status: http.StatusCreated, body: created}
	}
	// Recovery restores a committed creation receipt before the service is
	// ready. A pending current-schema intent has not committed a creation;
	// mutable titles, dates and card states are never operation identities.
	return p.idempotent(key, "new", req, run, func() (outcome, bool) { return outcome{}, false })
}

func (p *project) putDocument(key, id string, doc api.Document, h holder) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	if doc.SHA256 != digest(doc.Text) {
		return outcome{err: apiError(api.CodeEvidenceIncomplete, "document digest does not match the received bytes")}
	}
	run := func() outcome {
		s, err := board.ReadSnapshot(p.board, id)
		if err != nil {
			return outcome{err: err}
		}
		var generation uint64
		if claimedState(s.Entry.State) {
			r, err := p.requireHolder(s.Entry.TaskID, h)
			if err != nil {
				return outcome{err: err}
			}
			generation = r.Generation
		}
		err = board.UpdateDocument(p.board, s.Entry.TaskID, board.UpdateOptions{
			Document: doc.Path, Text: doc.Text, ExpectedRevision: doc.ExpectedRevision, ContractDecision: doc.ContractDecision, Authorization: doc.Authorization,
			Transaction: p.taskTransaction(key, "put:"+id+":"+doc.Path, doc, id, p.mutationGuard(id, h, false), nil, false),
		})
		if err != nil {
			return outcome{err: err}
		}
		p.changed(s.Entry.TaskID, generation)
		t, err := p.show(s.Entry.TaskID)
		if err != nil {
			return outcome{err: err}
		}
		return outcome{status: http.StatusOK, body: t}
	}
	applied := func() (outcome, bool) {
		current, err := p.document(id, doc.Path)
		if err != nil || current.ExpectedRevision <= doc.ExpectedRevision || current.SHA256 != doc.SHA256 {
			return outcome{}, false
		}
		t, err := p.show(id)
		return outcome{status: http.StatusOK, body: t}, err == nil
	}
	return p.idempotent(key, "put:"+id+":"+doc.Path, doc, run, applied)
}

func (p *project) move(key, id string, req api.Move, h holder) outcome {
	p.mu.Lock()
	defer p.mu.Unlock()
	run := func() outcome {
		view, err := board.LoadBoard(p.board)
		if err != nil {
			return outcome{err: err}
		}
		entry, err := board.Locate(view, id)
		if err != nil {
			return outcome{err: err}
		}
		from := entry.State
		if req.State == "working" && from != "review" && !(from == "working" && req.Authorization.DispatchID != "") {
			return outcome{err: apiError(api.CodeClaimRequired, "a move into working is a claim")}
		}
		var claim claimRecord
		claimed := false
		if claimedState(from) {
			terminate := req.State == "archived" || req.State == "trash"
			// A user-authorized termination may end a task whose lease was
			// lost; a live lease of another execution still refuses it.
			event := writeEvent(false)
			if !claimedState(req.State) {
				event = leaveEvent(terminate)
			}
			r, found, err := p.holderDecision(entry.TaskID, h, true, event)
			if err != nil {
				return outcome{err: err}
			}
			claim, claimed = r, found
		}
		var nextClaim *claimRecord
		if claimed && !claimedState(req.State) {
			claim.Status = api.ClaimReleased
			nextClaim = &claim
		}
		validate := p.mutationGuard(id, h, req.State == "archived" || req.State == "trash")
		// An exact completed dispatch replay is a read-only receipt. The board
		// producer still validates the entire authorization and completion payload;
		// it cannot turn this exception into a new mutation of a released task.
		if from == "done" && req.State == "done" && req.Authorization.DispatchID != "" {
			validate = func(tx *board.Transaction) error {
				snapshot, err := tx.Snapshot(id)
				if err != nil {
					return err
				}
				if snapshot.Entry.State != "done" {
					return apiError(api.CodeRevisionConflict, "completed dispatch moved")
				}
				return nil
			}
		}
		if p.info.Remote != "" && from == "working" && req.State == "review" {
			ownership := validate
			validate = func(tx *board.Transaction) error {
				if err := ownership(tx); err != nil {
					return err
				}
				return p.deliveryGuard(entry.TaskID, req.DeliveryCommit, h)(tx)
			}
		}
		if p.info.Remote != "" && req.State == "done" && from != "done" {
			ownership := validate
			validate = func(tx *board.Transaction) error {
				if err := ownership(tx); err != nil {
					return err
				}
				_, err := p.completionIntegration(tx, entry.TaskID, h.generation)
				return err
			}
		}
		businessCommit := req.DeliveryCommit
		if req.Authorization.DispatchID == "" {
			businessCommit = ""
		}
		replayed := false
		_, err = board.MoveWithOptions(entry, p.board, req.State, board.MoveOptions{
			Transaction: p.taskTransaction(key, "move:"+id, req, id, validate, nextClaim, false, func(tx *board.Transaction, task *api.Task) error {
				task.MoveReceipt = &api.MoveReceipt{Replayed: replayed}
				if req.Authorization.DispatchID != "" {
					d, err := tx.StagedDispatch(id, req.Authorization.DispatchID)
					if err != nil {
						return err
					}
					task.MoveReceipt.Dispatch = &d
					if req.State == "done" {
						if err := p.stageCompletedProposal(tx, id, d); err != nil {
							return err
						}
					}
				}
				return nil
			}),
			Replayed: &replayed, ServiceCompletion: true,
			Result: req.Result, Reason: req.Reason, Decision: req.Decision, DuplicateOf: req.DuplicateOf, ExpectedRevision: req.ExpectedRevision, Authorization: req.Authorization, DeliveryCommit: businessCommit, Disposition: req.Disposition,
		})
		if err != nil {
			return outcome{err: err}
		}
		if claimed && !claimedState(req.State) {
			_ = p.events.append(api.Event{Kind: api.EventClaimChanged, TaskID: entry.TaskID, Generation: claim.Generation})
		}
		p.changed(entry.TaskID, claim.Generation)
		t, err := p.show(entry.TaskID)
		if err != nil {
			return outcome{err: err}
		}
		return outcome{status: http.StatusOK, body: t}
	}
	applied := func() (outcome, bool) {
		t, err := p.show(id)
		if err != nil || t.State != req.State || req.ExpectedRevision == nil || t.Revision <= *req.ExpectedRevision {
			return outcome{}, false
		}
		return outcome{status: http.StatusOK, body: t}, true
	}
	return p.idempotent(key, "move:"+id, req, run, applied)
}

// grant renders a claim answer; the token is derived, never stored.
func (p *project) grant(r claimRecord) (api.ClaimGrant, error) {
	t, err := p.show(r.TaskID)
	if err != nil {
		return api.ClaimGrant{}, err
	}
	return api.ClaimGrant{
		Claim: *r.view(p.server.now()), Token: p.server.token(p.info.ID, r.TaskID, r.ExecutionID, r.Generation), Task: t,
		RenewInterval: api.DurationOf(p.server.renew), TTL: api.DurationOf(p.server.ttl),
	}, nil
}

// withToken restores the token a stored receipt omits.
func (p *project) withToken(out outcome) outcome {
	if out.err != nil || out.status != http.StatusOK {
		return out
	}
	var g api.ClaimGrant
	raw, ok := out.body.(json.RawMessage)
	if !ok {
		data, err := json.Marshal(out.body)
		if err != nil {
			return outcome{err: err}
		}
		raw = data
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return outcome{err: err}
	}
	g.Token = p.server.token(p.info.ID, g.Task.TaskID, g.Claim.ExecutionID, g.Claim.Generation)
	return outcome{status: out.status, body: g}
}

func validClaimRequest(req api.ClaimRequest) error {
	if !api.ValidIdentity(req.ClientID) || !api.ValidIdentity(req.ExecutionID) || !api.ValidIdentity(req.Owner) {
		return apiError(api.CodeInvalidRequest, "client_id, execution_id and owner must be identities")
	}
	return nil
}

// claim atomically moves a todo task into working and grants a new lease.
func (p *project) claim(key, id string, req api.ClaimRequest) outcome {
	if err := validClaimRequest(req); err != nil {
		return outcome{err: err}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	run := func() outcome {
		view, err := board.LoadBoard(p.board)
		if err != nil {
			return outcome{err: err}
		}
		entry, err := board.Locate(view, id)
		if err != nil {
			return outcome{err: err}
		}
		previous, found, err := p.readClaim(entry.TaskID)
		if err != nil {
			return outcome{err: err}
		}
		k, err := p.projectClaim(previous, found, claimedState(entry.State))
		if err != nil {
			return outcome{err: err}
		}
		fresh := !(found && (previous.ExecutionID == req.ExecutionID || previous.Generation == ^uint64(0)))
		if !k.step(serverclaim.Claim{Fresh: fresh}) {
			if claimedState(entry.State) {
				return outcome{err: apiError(api.CodeHeldByOther, "task "+entry.TaskID+" is already claimed")}
			}
			return outcome{err: apiError(api.CodeLeaseInvalid, "a new claim requires a fresh execution identity and generation")}
		}

		r := claimRecord{TaskID: entry.TaskID, ClientID: req.ClientID, ExecutionID: req.ExecutionID, Owner: req.Owner, Generation: 1, Status: claimPending}
		if found {
			r.Generation, r.Handoffs = previous.Generation+1, previous.Handoffs
		}
		r.TokenHash = tokenHash(p.server.token(p.info.ID, r.TaskID, r.ExecutionID, r.Generation))
		if req.Start != nil {
			if r.OriginalText, err = view.Document(entry.TaskID); err != nil {
				return outcome{err: err}
			}
		}
		now := p.server.now()
		r.Status, r.RenewedAt, r.ExpiresAt = api.ClaimActive, now, now.Add(p.server.ttl)
		extension := p.taskTransaction(key, "claim:"+id, req, r.TaskID, func(tx *board.Transaction) error {
			snapshot, err := tx.Snapshot(r.TaskID)
			if err == nil && req.ExpectedRevision != 0 && req.ExpectedRevision != snapshot.Revision {
				return apiError(api.CodeRevisionConflict, "start input names an earlier card version")
			}
			if err == nil && snapshot.Entry.State != "todo" {
				return apiError(api.CodeRevisionConflict, "task is no longer todo")
			}
			return err
		}, &r, true)

		var moved board.Entry
		if req.Start != nil {
			moved, err = board.BeginTaskStartWithTransaction(entry, p.board, board.TaskStartInput{Owner: req.Owner, Session: req.Start.Session, Window: req.Start.Window, ExecResolved: req.Start.ExecResolved}, extension)
		} else {
			moved, err = board.MoveWithOptions(entry, p.board, "working", board.MoveOptions{Owner: req.Owner, Transaction: extension})
		}
		if err == nil && req.Start != nil {
			p.starts[entry.TaskID] = startCursor{entry: moved, generation: r.Generation}
		}
		if err != nil {
			return outcome{err: err}
		}
		_ = p.events.append(api.Event{Kind: api.EventClaimChanged, TaskID: r.TaskID, Generation: r.Generation})
		p.changed(r.TaskID, r.Generation)
		return p.activate(r)
	}
	applied := func() (outcome, bool) {
		r, found, err := p.readClaim(id)
		if err != nil || !found || r.ExecutionID != req.ExecutionID || r.Status == api.ClaimReleased {
			return outcome{}, false
		}
		t, err := p.show(id)
		if err != nil || !claimedState(t.State) {
			return outcome{}, false
		}
		return p.activate(r), true
	}
	return p.withToken(p.idempotent(key, "claim:"+id, req, run, applied))
}

func (p *project) activate(r claimRecord) outcome {
	if r.Status == claimPending {
		now := p.server.now()
		r.Status, r.RenewedAt, r.ExpiresAt = api.ClaimActive, now, now.Add(p.server.ttl)
		if err := p.writeClaim(r); err != nil {
			return outcome{err: err}
		}
		p.log().Info("claim granted", "task", r.TaskID, "generation", r.Generation, "client", r.ClientID)
		_ = p.events.append(api.Event{Kind: api.EventClaimChanged, TaskID: r.TaskID, Generation: r.Generation})
		p.changed(r.TaskID, r.Generation)
	}
	g, err := p.grant(r)
	if err != nil {
		return outcome{err: err}
	}
	g.Token = ""
	return outcome{status: http.StatusOK, body: g}
}

// renew extends the live lease of h. It writes no card and no receipt; a
// repeated renewal is naturally idempotent.
func (p *project) renew(id string, h holder) (api.Renewal, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	id, err := board.NormalizeTaskID(id)
	if err != nil {
		return api.Renewal{}, err
	}
	var r claimRecord
	err = board.WithTransaction(p.board, board.LockScope{Groups: []string{serviceGroup}}, func(tx *board.Transaction) error {
		var err error
		r, _, err = p.holderDecision(id, h, true, renewEvent)
		if err != nil {
			return err
		}
		now := p.server.now()
		r.RenewedAt, r.ExpiresAt = now, now.Add(p.server.ttl)
		return stageJSON(tx, "claims/"+id+".json", r)
	})
	if err != nil {
		return api.Renewal{}, err
	}
	return api.Renewal{Claim: *r.view(p.server.now())}, nil
}

// takeover hands a lost claim to a new execution with the next generation.
// Only an expired lease, or a claimed card without any claim record (a
// migrated board), can be taken over. A live lease is never preempted: its
// own holder may restart a freshly confirmed stopped executor on the same host.
func (p *project) takeover(key, id string, req api.ClaimRequest, h holder) outcome {
	if err := validClaimRequest(req); err != nil {
		return outcome{err: err}
	}
	if strings.TrimSpace(req.Decision) == "" {
		return outcome{err: apiError(api.CodeInvalidRequest, "a takeover needs a decision reference")}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	run := func() outcome {
		t, err := p.show(id)
		if err != nil {
			return outcome{err: err}
		}
		if !claimedState(t.State) {
			return outcome{err: apiError(api.CodeClaimRequired, "task "+t.TaskID+" is not claimed; claim it instead")}
		}
		if req.ExpectedRevision == 0 || req.ExpectedRevision != t.Revision {
			return outcome{err: apiError(api.CodeRevisionConflict, "takeover requires the inspected card revision")}
		}
		previous, found, err := p.readClaim(t.TaskID)
		if err != nil {
			return outcome{err: err}
		}
		now := p.server.now()
		k, err := p.projectClaim(previous, found, true)
		if err != nil {
			return outcome{err: err}
		}
		// A valid lease is preempted by nobody; only its holder restarts its
		// own stopped executor. The refusal is the first check that fails, in
		// the order the server has always checked.
		var refusal error
		restart := false
		ownedRestart := serverclaim.Valid(k.state, k.params)
		if ownedRestart {
			ht, herr := h.forTask(t.TaskID)
			hg, htg := k.headers(previous, found, ht)
			evidence := req.ClientID == previous.ClientID && req.ExpectedRevision == t.Revision && req.PreviousPresence != nil && req.PreviousPresence.Status == "stopped" && p.validObservationTicket(t.TaskID, req.ExpectedRevision, previous.Generation, req.ObservationTicket)
			restart = h.token != "" && h.clientID == previous.ClientID && evidence
			switch {
			case h.token == "":
				refusal = apiError(api.CodeLeaseActive, "the lease of task "+t.TaskID+" is still active")
			case h.clientID != previous.ClientID:
				refusal = apiError(api.CodeHeldByOther, "only the current client may restart its stopped executor")
			case herr != nil:
				refusal = herr
			case hg != k.state.Gen || htg != k.state.Gen:
				refusal = p.holderError(t.TaskID, previous, found, ht)
			case !evidence:
				refusal = apiError(api.CodeEvidenceIncomplete, "an owned restart requires a fresh stopped observation, version and private holder proof")
			}
		}
		if refusal == nil && previous.Generation != req.ExpectedGeneration {
			refusal = apiError(api.CodeLeaseInvalid, "the observed generation is not current")
		}
		fresh := !(previous.Generation == ^uint64(0) || previous.ExecutionID != "" && previous.ExecutionID == req.ExecutionID)
		if refusal == nil && !fresh {
			refusal = apiError(api.CodeLeaseInvalid, "takeover requires a fresh execution identity and generation")
		}
		expected := req.ExpectedGeneration
		if expected != previous.Generation {
			expected = k.state.Gen + 1
		}
		ht, _ := h.forTask(t.TaskID)
		hg, htg := k.headers(previous, found, ht)
		if !k.step(serverclaim.Takeover{Expected: expected, Fresh: fresh, Restart: restart, G: hg, Tg: htg}) {
			if refusal == nil {
				refusal = apiError(api.CodeLeaseInvalid, "the observed generation is not current")
			}
			return outcome{err: refusal}
		}

		r := claimRecord{
			TaskID: t.TaskID, ClientID: req.ClientID, ExecutionID: req.ExecutionID, Owner: req.Owner,
			Generation: previous.Generation + 1, Status: api.ClaimActive, RenewedAt: now, ExpiresAt: now.Add(p.server.ttl),
			Handoffs: append(previous.Handoffs, api.Handoff{
				At: now, FromGeneration: previous.Generation, ToGeneration: previous.Generation + 1,
				FromClient: previous.ClientID, ToClient: req.ClientID, Owner: req.Owner, Decision: req.Decision,
			}),
		}
		r.TokenHash = tokenHash(p.server.token(p.info.ID, r.TaskID, r.ExecutionID, r.Generation))
		// The card's execution ledger hands off in the same project lock: the
		// new owner, session, and window replace the old execution's, and the
		// new executor reports and confirms its start through .../execution.
		snapshot, err := board.ReadSnapshot(p.board, t.TaskID)
		if err != nil {
			return outcome{err: err}
		}
		if _, err := board.BindExecutionCursor(p.board, snapshot.Entry); err != nil {
			return outcome{err: err}
		}
		if board.MetadataFrom(snapshot.Text, "DISPATCH_ID") != "" && req.Dispatch == nil {
			return outcome{err: apiError(api.CodeEvidenceIncomplete, "takeover of a bound task needs the exact inspected dispatch context")}
		}
		in := board.TakeoverInput{Owner: req.Owner, Decision: req.Decision, Dispatch: req.Dispatch}
		if req.Start != nil {
			in.Session, in.Window, in.ExecResolved = req.Start.Session, req.Start.Window, req.Start.ExecResolved
		}
		if _, err := board.BeginTakeoverWithTransaction(p.board, snapshot.Entry, in, p.taskTransaction(key, "takeover:"+id, req, r.TaskID, func(tx *board.Transaction) error {
			_, err := tx.Expect(r.TaskID, snapshot.Entry.State, snapshot.Revision)
			if err != nil {
				return err
			}
			if ownedRestart {
				if _, err := p.requireHolder(r.TaskID, h); err != nil {
					return err
				}
				if !p.validObservationTicket(r.TaskID, req.ExpectedRevision, previous.Generation, req.ObservationTicket) {
					return apiError(api.CodeEvidenceIncomplete, "owned restart observation expired before commit")
				}
			}
			return nil
		}, &r, true), board.TransactionExtension{Before: func(tx *board.Transaction) error { return p.stageSupersedeRequests(tx, r.TaskID, r.Generation) }}); err != nil {
			return outcome{err: err}
		}
		p.starts[r.TaskID] = startCursor{entry: snapshot.Entry, generation: r.Generation}
		p.log().Info("claim taken over", "task", r.TaskID, "from_generation", previous.Generation, "generation", r.Generation, "client", r.ClientID)
		_ = p.events.append(api.Event{Kind: api.EventClaimChanged, TaskID: r.TaskID, Generation: r.Generation})
		g, err := p.grant(r)
		if err != nil {
			return outcome{err: err}
		}
		g.Token = ""
		return outcome{status: http.StatusOK, body: g}
	}
	applied := func() (outcome, bool) {
		r, found, err := p.readClaim(id)
		if err != nil || !found || r.ExecutionID != req.ExecutionID || r.Generation != req.ExpectedGeneration+1 {
			return outcome{}, false
		}
		g, err := p.grant(r)
		if err != nil {
			return outcome{}, false
		}
		g.Token = ""
		return outcome{status: http.StatusOK, body: g}, true
	}
	return p.withToken(p.idempotent(key, "takeover:"+id, req, run, applied))
}
