package server

import (
	"crypto/rand"
	"encoding/hex"
	"maps"
	"net/http"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/fs"
	"github.com/dualface/kander/v2/internal/statemachine/startorder"
)

// Start orders live in the service group of the project (start-orders/<id>.json),
// so a claim, a start confirmation, a rollback, a takeover, or a release stages
// the change of its order in its own board transaction. Every decision goes
// through the start order kernel (start_order_kernel.go).

// startOrderSweepInterval is how often the server expires orders and fails the
// orders of lost hosts; tests shorten it.
var startOrderSweepInterval = 10 * time.Second

// maxOrderReason bounds the message of a failure the target host reports.
const maxOrderReason = 2000

func startOrderName(id string) string { return "start-orders/" + id + ".json" }

func (p *project) readOrder(id string) (api.StartOrder, bool, error) {
	var o api.StartOrder
	if !api.ValidIdentity(id) {
		return o, false, nil
	}
	found, err := readJSON(p.root, servicePath(filepath.FromSlash(startOrderName(id))), &o)
	return o, found, err
}

// openOrdersLocked returns the cached open orders of the project, reading the
// stored orders when the cache is empty. The caller holds the board lock.
func (p *project) openOrdersLocked() (map[string]api.StartOrder, error) {
	if p.openOrders != nil {
		return p.openOrders, nil
	}
	names, err := p.serviceNames("start-orders", ".json", fs.KindFile)
	if err != nil {
		return nil, err
	}
	open := map[string]api.StartOrder{}
	for _, id := range names {
		o, found, err := p.readOrder(id)
		if err != nil {
			return nil, err
		}
		if found && orderOpen(o) {
			open[o.ID] = o
		}
	}
	p.openOrders = open
	return open, nil
}

// openOrdersOf lists the open orders of the project that target a client.
func (p *project) openOrdersOf(clientID string) ([]api.StartOrder, error) {
	if err := p.lockBoard(); err != nil {
		return nil, err
	}
	defer p.unlockBoard()
	open, err := p.openOrdersLocked()
	if err != nil {
		return nil, err
	}
	var out []api.StartOrder
	for _, o := range open {
		if o.TargetClientID == clientID {
			out = append(out, o)
		}
	}
	return out, nil
}

// orderChanges are the orders one board transaction writes.
type orderChanges []api.StartOrder

func (c orderChanges) stage(tx *board.Transaction) error {
	for _, o := range c {
		if err := stageJSON(tx, startOrderName(o.ID), o); err != nil {
			return err
		}
	}
	return nil
}

// orderPublished updates the open order cache, appends one event per changed
// order, and wakes the waiting heartbeats. The caller holds the board lock and
// the transaction that wrote the changes committed.
func (p *project) orderPublished(changes orderChanges) {
	for _, o := range changes {
		if p.openOrders != nil {
			if orderOpen(o) {
				p.openOrders[o.ID] = o
			} else {
				delete(p.openOrders, o.ID)
			}
		}
		reason := ""
		if o.Reason != nil {
			reason = o.Reason.Code
		}
		p.log().Info("start order changed", "order", o.ID, "task", o.TaskID, "state", o.State, "host", o.TargetHost, "generation", o.Generation, "reason", reason)
		_ = p.events.append(api.Event{Kind: api.EventStartOrderChanged, TaskID: o.TaskID, OrderID: o.ID, State: o.State, Generation: o.Generation})
	}
	if len(changes) > 0 {
		p.server.hosts.notify()
	}
}

// commitOrders writes changes in a transaction of their own.
func (p *project) commitOrders(changes orderChanges, after func(*board.Transaction) error) error {
	err := p.boardTransaction(board.LockScope{Groups: []string{serviceGroup}}, func(tx *board.Transaction) error {
		if err := changes.stage(tx); err != nil {
			return err
		}
		if after != nil {
			return after(tx)
		}
		return nil
	})
	if err == nil {
		p.orderPublished(changes)
	}
	return err
}

func newOrderID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "so-" + hex.EncodeToString(b), nil
}

func refused(o api.StartOrder, message string) *api.Error {
	return apiError(api.CodeStartOrderRefused, "start order "+o.ID+" is "+o.State+": "+message)
}

// createOrder checks a new order against the project, the card, and the
// target host, and records it pending.
func (p *project) createOrder(scope receiptScope, req api.NewStartOrder, requester string) outcome {
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	run := func() outcome {
		if !api.ValidHostName(req.Host) {
			return outcome{err: apiError(api.CodeInvalidRequest, "invalid host name")}
		}
		if (req.Agent != "" && !api.ValidIdentity(req.Agent)) || (req.Launcher != "" && !api.ValidIdentity(req.Launcher)) {
			return outcome{err: apiError(api.CodeInvalidRequest, "invalid agent or launcher name")}
		}
		if p.info.Remote == "" {
			return outcome{err: apiError(api.CodeRemoteRequired, "project "+p.info.Name+" has no Git remote; a start on another host needs one")}
		}
		task, err := p.show(req.TaskID)
		if err != nil {
			return outcome{err: err}
		}
		if task.State != "todo" {
			return outcome{err: apiError(api.CodeTaskNotTodo, "task "+task.TaskID+" is in "+task.State+", not in todo")}
		}
		host, found := p.server.hostByName(req.Host)
		if !found {
			return outcome{err: apiError(api.CodeHostUnknown, "no host named "+req.Host+" has sent a heartbeat to this server")}
		}
		now := p.server.now()
		if now.Sub(host.LastHeartbeat) >= api.HostOnlineWindow {
			return outcome{err: apiError(api.CodeHostOffline, "host "+host.Name+" sent its last heartbeat at "+host.LastHeartbeat.Format(time.RFC3339))}
		}
		if !host.Accepting || host.OrderProtocol < api.StartOrderProtocol {
			return outcome{err: apiError(api.CodeHostUnsupported, "host "+host.Name+" does not take start orders")}
		}
		index := slices.IndexFunc(host.Projects, func(hp api.HostProject) bool { return hp.Project == p.info.Name })
		if index < 0 {
			return outcome{err: apiError(api.CodeHostProjectMissing, "host "+host.Name+" did not report project "+p.info.Name)}
		}
		reported := host.Projects[index]
		if req.Agent != "" && !slices.Contains(reported.Agents, req.Agent) {
			return outcome{err: apiError(api.CodeHostAgentUnavailable, "host "+host.Name+" did not report agent "+req.Agent)}
		}
		if req.Launcher != "" && !slices.Contains(reported.Launchers, req.Launcher) {
			return outcome{err: apiError(api.CodeHostLauncherUnavailable, "host "+host.Name+" did not report launcher "+req.Launcher)}
		}
		open, err := p.openOrdersLocked()
		if err != nil {
			return outcome{err: err}
		}
		for _, o := range open {
			if o.TaskID == task.TaskID {
				return outcome{err: apiError(api.CodeStartOrderOpen, "task "+task.TaskID+" already has open start order "+o.ID)}
			}
		}
		id, err := newOrderID()
		if err != nil {
			return outcome{err: err}
		}
		o := api.StartOrder{
			ID: id, ProjectID: p.info.ID, TaskID: task.TaskID, TargetClientID: host.ClientID, TargetHost: host.Name,
			Agent: req.Agent, Launcher: req.Launcher, RequesterClientID: requester, CreatedAt: now, UpdatedAt: now,
		}
		if !stepOrder(&o, startorder.Create{}, 0) {
			return outcome{err: apiError(api.CodeInternal, "the start order kernel refused a new order")}
		}
		err = p.commitOrders(orderChanges{o}, func(tx *board.Transaction) error {
			return p.stageOperationReceiptStatus(tx, scope, "start-order", req, http.StatusCreated, o)
		})
		return result(http.StatusCreated, o, err)
	}
	return p.idempotent(scope, "start-order", req, run, func() (outcome, bool) { return outcome{}, false })
}

// orderTransition applies one kernel event of a request to a stored order.
// applied reports an order that already is in the state the request asks for,
// which answers a retry without a change.
func (p *project) orderTransition(scope receiptScope, operation, id string, request any, decide func(o *api.StartOrder) (event startorder.Event, applied bool, err error)) outcome {
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	run := func() outcome {
		o, found, err := p.readOrder(id)
		if err != nil {
			return outcome{err: err}
		}
		if !found {
			return outcome{err: apiError(api.CodeNotFound, "start order "+id+" does not exist")}
		}
		event, applied, err := decide(&o)
		if err != nil {
			return outcome{err: err}
		}
		if applied {
			return outcome{status: http.StatusOK, body: o}
		}
		before := o
		if !stepOrder(&o, event, 0) {
			return outcome{err: refused(before, "the request does not apply")}
		}
		o.UpdatedAt = p.server.now()
		err = p.commitOrders(orderChanges{o}, func(tx *board.Transaction) error {
			return p.stageOperationReceipt(tx, scope, operation, request, o)
		})
		return result(http.StatusOK, o, err)
	}
	return p.idempotent(scope, operation, request, run, func() (outcome, bool) { return outcome{}, false })
}

func (p *project) acceptOrder(scope receiptScope, id, clientID string) outcome {
	return p.orderTransition(scope, "start-order-accept:"+id, id, clientID, func(o *api.StartOrder) (startorder.Event, bool, error) {
		target := o.TargetClientID == clientID
		if o.State == api.StartOrderAccepted && target {
			return nil, true, nil
		}
		if !target {
			return nil, false, refused(*o, "only its target host takes it")
		}
		return startorder.Accept{Target: true}, false, nil
	})
}

func (p *project) failOrder(scope receiptScope, id, clientID string, failure api.StartOrderFailure) outcome {
	return p.orderTransition(scope, "start-order-fail:"+id, id, failure, func(o *api.StartOrder) (startorder.Event, bool, error) {
		if !api.ValidIdentity(failure.Code) || len(failure.Message) > maxOrderReason {
			return nil, false, apiError(api.CodeInvalidRequest, "a failure needs an identity code and a message of at most 2000 bytes")
		}
		if o.TargetClientID != clientID {
			return nil, false, refused(*o, "only its target host reports its failure")
		}
		o.Reason = &api.OrderReason{Code: failure.Code, Message: singleLine(failure.Message)}
		return startorder.Fail{Target: true}, false, nil
	})
}

func (p *project) cancelOrder(scope receiptScope, id, clientID string) outcome {
	return p.orderTransition(scope, "start-order-cancel:"+id, id, clientID, func(o *api.StartOrder) (startorder.Event, bool, error) {
		if o.State == api.StartOrderCancelled {
			return nil, true, nil
		}
		if !orderCancellable(*o) {
			return nil, false, refused(*o, "only a pending order or an accepted order no claim took yet can be cancelled")
		}
		o.Reason = &api.OrderReason{Code: api.OrderReasonCancelled, Message: "cancelled by client " + clientID}
		return startorder.Cancel{}, false, nil
	})
}

// singleLine keeps a reported message on one line without control characters.
func singleLine(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, text)
}

// claimOrders decides the orders a claim of task for generation gen changes:
// the order the claim names binds gen, and every other open order of the task
// fails as already claimed. A claim the named order does not allow is refused.
// The caller holds the board lock and stages the changes in the claim's
// transaction.
func (p *project) claimOrders(task string, req api.ClaimRequest, gen uint64) (orderChanges, error) {
	now := p.server.now()
	var changes orderChanges
	if req.StartOrderID != "" {
		o, found, err := p.readOrder(req.StartOrderID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, apiError(api.CodeStartOrderRefused, "start order "+req.StartOrderID+" does not exist")
		}
		before := o
		if !stepOrder(&o, startorder.Claim{Target: o.TargetClientID == req.ClientID, Task: o.TaskID == task, Gen: 1}, gen) {
			return nil, refused(before, "it does not allow this claim")
		}
		o.UpdatedAt = now
		changes = append(changes, o)
	}
	open, err := p.openOrdersLocked()
	if err != nil {
		return nil, err
	}
	for _, id := range slices.Sorted(maps.Keys(open)) {
		o := open[id]
		if o.TaskID != task || o.ID == req.StartOrderID {
			continue
		}
		if stepOrder(&o, startorder.Preempt{}, 0) {
			o.Reason = &api.OrderReason{Code: api.OrderReasonAlreadyClaimed, Message: "task " + task + " was claimed by execution " + req.ExecutionID + " of client " + req.ClientID}
			o.UpdatedAt = now
			changes = append(changes, o)
		}
	}
	return changes, nil
}

// generationOrders decides how the end of a start of generation gen changes
// the order bound to it: a confirmation starts it, a rollback fails it, and a
// takeover, a release, or a termination supersedes it. An order the event does
// not apply to (none, another generation, already final) is left as it is.
func (p *project) generationOrders(orderID string, gen uint64, event startorder.Event, reason *api.OrderReason) (orderChanges, error) {
	if orderID == "" {
		return nil, nil
	}
	o, found, err := p.readOrder(orderID)
	if err != nil || !found {
		return nil, err
	}
	if !stepOrder(&o, event, gen) {
		return nil, nil
	}
	o.Reason, o.UpdatedAt = reason, p.server.now()
	return orderChanges{o}, nil
}

func (p *project) confirmOrders(claim claimRecord) (orderChanges, error) {
	return p.generationOrders(claim.StartOrderID, claim.Generation, startorder.Confirm{Gen: 1}, nil)
}

func (p *project) rollbackOrders(claim claimRecord) (orderChanges, error) {
	return p.generationOrders(claim.StartOrderID, claim.Generation, startorder.Rollback{Gen: 1}, &api.OrderReason{Code: api.OrderReasonRolledBack, Message: "the start of generation " + strconv.FormatUint(claim.Generation, 10) + " failed and returned the card to todo"})
}

// supersedeOrders ends the order of a claim whose generation ended before its
// start was confirmed.
func (p *project) supersedeOrders(claim claimRecord, how string) (orderChanges, error) {
	return p.generationOrders(claim.StartOrderID, claim.Generation, startorder.Supersede{Gen: 1}, &api.OrderReason{Code: api.OrderReasonSuperseded, Message: "the claim of generation " + strconv.FormatUint(claim.Generation, 10) + " ended by " + how + " before its start was confirmed"})
}

// orderDecorator stages changes in a task transaction; a confirmation records
// the WINDOW the start wrote.
func orderDecorator(changes orderChanges) func(*board.Transaction, *api.Task) error {
	return func(tx *board.Transaction, task *api.Task) error {
		for i := range changes {
			if changes[i].State == api.StartOrderStarted {
				changes[i].Window = board.MetadataFrom(task.Text, board.FieldWindow)
			}
		}
		return changes.stage(tx)
	}
}

// sweepOrders expires the orders no host took in time and fails the accepted
// orders whose host went silent before any claim, on the server clock. The
// check of the claim transaction keeps a late claim of such an order refused.
func (p *project) sweepOrders() error {
	if err := p.lockBoard(); err != nil {
		return err
	}
	defer p.unlockBoard()
	open, err := p.openOrdersLocked()
	if err != nil {
		return err
	}
	now := p.server.now()
	var changes orderChanges
	for _, id := range slices.Sorted(maps.Keys(open)) {
		o := open[id]
		var reason *api.OrderReason
		switch {
		case stepOrder(&o, startorder.Expire{Due: now.Sub(o.CreatedAt) >= api.StartOrderAcceptWindow}, 0):
			reason = &api.OrderReason{Code: api.OrderReasonExpired, Message: "host " + o.TargetHost + " did not take the order within " + api.StartOrderAcceptWindow.String()}
		case stepOrder(&o, startorder.HostLost{Lost: p.hostLost(o.TargetClientID, now)}, 0):
			reason = &api.OrderReason{Code: api.OrderReasonHostLost, Message: "host " + o.TargetHost + " sent no heartbeat within " + api.HostOnlineWindow.String() + " before it claimed the card"}
		default:
			continue
		}
		o.Reason, o.UpdatedAt = reason, now
		changes = append(changes, o)
	}
	if len(changes) == 0 {
		return nil
	}
	return p.commitOrders(changes, nil)
}

func (p *project) hostLost(clientID string, now time.Time) bool {
	host, found := p.server.hostByClient(clientID)
	return !found || now.Sub(host.LastHeartbeat) >= api.HostOnlineWindow
}

// sweepStartOrders runs one sweep over every project.
func (s *Server) sweepStartOrders() {
	s.mu.Lock()
	projects := make([]*project, 0, len(s.projects))
	for _, p := range s.projects {
		projects = append(projects, p)
	}
	s.mu.Unlock()
	for _, p := range projects {
		if err := p.sweepOrders(); err != nil && !s.closing.Load() {
			s.log.Error("start order sweep failed", "project", p.info.ID, "error", err)
		}
	}
}

// runStartOrderSweeps sweeps until the server shuts down.
func (s *Server) runStartOrderSweeps() {
	defer s.sweepWorkers.Done()
	ticker := time.NewTicker(startOrderSweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.shutdown.Done():
			return
		case <-ticker.C:
			s.sweepStartOrders()
		}
	}
}

func (s *Server) startOrderRoutes(mux *routeTable) {
	s.projectRoute(mux, "POST "+api.StartOrdersRoute, func(p *project, r *http.Request) outcome {
		var req api.NewStartOrder
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		return p.createOrder(scopeOf(r), req, r.Header.Get(api.HeaderClientID))
	})
	s.projectRoute(mux, "GET "+api.StartOrderRoute, func(p *project, r *http.Request) outcome {
		o, found, err := p.readOrder(r.PathValue("order"))
		if err == nil && !found {
			err = apiError(api.CodeNotFound, "start order "+r.PathValue("order")+" does not exist")
		}
		return result(http.StatusOK, o, err)
	})
	client := func(r *http.Request) (string, error) {
		id := r.Header.Get(api.HeaderClientID)
		if !api.ValidIdentity(id) {
			return "", apiError(api.CodeInvalidRequest, "the client ID header is required")
		}
		return id, nil
	}
	s.projectRoute(mux, "POST "+api.StartOrderRoute+"/accept", func(p *project, r *http.Request) outcome {
		id, err := client(r)
		if err != nil {
			return outcome{err: err}
		}
		return p.acceptOrder(scopeOf(r), r.PathValue("order"), id)
	})
	s.projectRoute(mux, "POST "+api.StartOrderRoute+"/fail", func(p *project, r *http.Request) outcome {
		id, err := client(r)
		if err != nil {
			return outcome{err: err}
		}
		var failure api.StartOrderFailure
		if err := decode(r, &failure); err != nil {
			return outcome{err: err}
		}
		return p.failOrder(scopeOf(r), r.PathValue("order"), id, failure)
	})
	s.projectRoute(mux, "POST "+api.StartOrderRoute+"/cancel", func(p *project, r *http.Request) outcome {
		id, err := client(r)
		if err != nil {
			return outcome{err: err}
		}
		return p.cancelOrder(scopeOf(r), r.PathValue("order"), id)
	})
}
