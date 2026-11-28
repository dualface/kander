package server_test

import (
	"slices"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

// orderHost is a host of the start order tests: its client and its heartbeat.
type orderHost struct {
	t  *testing.T
	c  *client.Client
	hb api.HostHeartbeat
}

func (f *fixture) host(clientID, name string, projects ...api.HostProject) *orderHost {
	f.t.Helper()
	c, err := client.New(f.http.URL, clientID)
	if err != nil {
		f.t.Fatal(err)
	}
	c.Backoff = time.Millisecond
	if projects == nil {
		projects = []api.HostProject{{Project: "demo", Agents: []string{"claude", "codex"}, Launchers: []string{"tmux-session"}}}
	}
	return &orderHost{t: f.t, c: c, hb: api.HostHeartbeat{ClientID: clientID, Name: name, WorkerID: "w-" + clientID, Version: "test", Accepting: true, OrderProtocol: api.StartOrderProtocol, Projects: projects}}
}

func (h *orderHost) beat() api.HostHeartbeatResult {
	h.t.Helper()
	result, err := h.c.Heartbeat(h.t.Context(), h.hb, 0)
	if err != nil {
		h.t.Fatal(err)
	}
	return result
}

func (h *orderHost) project(id string) *client.Project { return h.c.Project(id) }

func newOrder(t *testing.T, p *client.Project, task, host string) api.StartOrder {
	t.Helper()
	o, err := p.CreateStartOrder(t.Context(), "", api.NewStartOrder{TaskID: task, Host: host})
	if err != nil {
		t.Fatal(err)
	}
	if o.State != api.StartOrderPending || o.TargetHost != host || o.TaskID != task {
		t.Fatalf("new order = %+v", o)
	}
	return o
}

func orderClaim(t *testing.T, p *client.Project, task, clientID, order string) (api.ClaimGrant, error) {
	t.Helper()
	return p.Claim(t.Context(), "", task, api.ClaimRequest{BranchMode: "main-develop", ClientID: clientID, ExecutionID: "exec-" + clientID + "-" + task, Owner: "claude", StartOrderID: order, Start: &api.StartInput{Session: "claude pending"}})
}

func orderState(t *testing.T, p *client.Project, id string) api.StartOrder {
	t.Helper()
	o, err := p.StartOrder(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	return o
}

func TestHostHeartbeatListsOnlineHostsAndConflicts(t *testing.T) {
	f := newFixture(t)
	b := f.host("c-000000000001-hostb", "hostb")
	result := b.beat()
	if !result.Host.Online || result.Host.Name != "hostb" || len(result.Orders) != 0 {
		t.Fatalf("heartbeat = %+v", result)
	}
	list, err := b.c.Hosts(t.Context())
	if err != nil || len(list.Hosts) != 1 || !list.Hosts[0].Online || list.Hosts[0].Projects[0].Project != "demo" {
		t.Fatalf("hosts = %+v %v", list, err)
	}

	// Another client ID may not take the name.
	other := f.host("c-000000000002-hostb", "hostb")
	_, err = other.c.Heartbeat(t.Context(), other.hb, 0)
	expectCode(t, err, api.CodeHostNameConflict)

	// A second worker of the same client ID within the online window is a conflict.
	second := *b
	second.hb.WorkerID = "w-second"
	second.beat()
	list, _ = b.c.Hosts(t.Context())
	if list.Hosts[0].Conflict != api.ConflictClientID {
		t.Fatalf("conflict not reported: %+v", list.Hosts[0])
	}

	// The server clock decides when a host is offline, and the conflict passes with it.
	f.clock.advance(api.HostOnlineWindow + time.Second)
	list, _ = b.c.Hosts(t.Context())
	if list.Hosts[0].Online || list.Hosts[0].Conflict != "" {
		t.Fatalf("host after the window = %+v", list.Hosts[0])
	}

	// The registry survives a restart.
	dir, clock := f.dir, f.clock
	f.stop()
	f = start(t, dir, clock)
	again := f.host("c-000000000001-hostb", "hostb")
	list, err = again.c.Hosts(t.Context())
	if err != nil || len(list.Hosts) != 1 || list.Hosts[0].Name != "hostb" {
		t.Fatalf("hosts after restart = %+v %v", list, err)
	}
}

func TestStartOrderCreationRefusals(t *testing.T) {
	f := newFixture(t)
	a := f.project("c-00000000000a-hosta")
	task := todoTask(t, a, "refusals")
	b := f.host("c-00000000000b-hostb", "hostb")
	b.beat()
	refuse := func(req api.NewStartOrder, code string) {
		t.Helper()
		_, err := a.CreateStartOrder(t.Context(), "", req)
		expectCode(t, err, code)
	}
	refuse(api.NewStartOrder{TaskID: task.TaskID, Host: "nohost"}, api.CodeHostUnknown)
	refuse(api.NewStartOrder{TaskID: task.TaskID, Host: "hostb", Agent: "grok"}, api.CodeHostAgentUnavailable)
	refuse(api.NewStartOrder{TaskID: task.TaskID, Host: "hostb", Launcher: "herdr"}, api.CodeHostLauncherUnavailable)

	missing := f.host("c-00000000000c-hostc", "hostc", api.HostProject{Project: "other", Agents: []string{}, Launchers: []string{}})
	missing.beat()
	refuse(api.NewStartOrder{TaskID: task.TaskID, Host: "hostc"}, api.CodeHostProjectMissing)

	closed := f.host("c-00000000000d-hostd", "hostd")
	closed.hb.Accepting = false
	closed.beat()
	refuse(api.NewStartOrder{TaskID: task.TaskID, Host: "hostd"}, api.CodeHostUnsupported)
	old := f.host("c-00000000000e-hoste", "hoste")
	old.hb.OrderProtocol = 0
	old.beat()
	refuse(api.NewStartOrder{TaskID: task.TaskID, Host: "hoste"}, api.CodeHostUnsupported)

	// One open order per card; a retry of the same request returns it.
	const key = "order-key-1"
	first, err := a.CreateStartOrder(t.Context(), key, api.NewStartOrder{TaskID: task.TaskID, Host: "hostb", Agent: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	again, err := a.CreateStartOrder(t.Context(), key, api.NewStartOrder{TaskID: task.TaskID, Host: "hostb", Agent: "codex"})
	if err != nil || again.ID != first.ID {
		t.Fatalf("retry = %+v %v, want %s", again, err, first.ID)
	}
	refuse(api.NewStartOrder{TaskID: task.TaskID, Host: "hostb"}, api.CodeStartOrderOpen)

	backlog, err := a.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "backlog", Title: "Backlog", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	refuse(api.NewStartOrder{TaskID: backlog.TaskID, Host: "hostb"}, api.CodeTaskNotTodo)

	f.clock.advance(api.HostOnlineWindow)
	refuse(api.NewStartOrder{TaskID: todoTask(t, a, "offline").TaskID, Host: "hostb"}, api.CodeHostOffline)

	// A project without a Git remote takes no order.
	c, _ := client.New(f.http.URL, "c-00000000000a-hosta")
	info, err := c.Register(t.Context(), "noremote", "")
	if err != nil {
		t.Fatal(err)
	}
	b.hb.Projects = append(b.hb.Projects, api.HostProject{Project: "noremote", Agents: []string{"claude"}, Launchers: []string{}})
	b.beat()
	noRemote := c.Project(info.ID)
	local := todoTask(t, noRemote, "local")
	_, err = noRemote.CreateStartOrder(t.Context(), "", api.NewStartOrder{TaskID: local.TaskID, Host: "hostb"})
	expectCode(t, err, api.CodeRemoteRequired)
}

func TestStartOrderStartsThroughItsClaimAndConfirmation(t *testing.T) {
	f := newFixture(t)
	a := f.project("c-00000000000a-hosta")
	task := todoTask(t, a, "lifecycle")
	b := f.host("c-00000000000b-hostb", "hostb")
	b.beat()
	cursor := ""
	if page, err := a.Events(t.Context(), "", 0); err == nil {
		cursor = page.Cursor
	}
	o := newOrder(t, a, task.TaskID, "hostb")
	orders := b.beat().Orders
	if len(orders) != 1 || orders[0].ID != o.ID || orders[0].State != api.StartOrderPending {
		t.Fatalf("heartbeat orders = %+v", orders)
	}

	// Only the target host takes the order, and a claim needs it taken.
	_, err := a.AcceptStartOrder(t.Context(), "", o.ID)
	expectCode(t, err, api.CodeStartOrderRefused)
	bp := b.project(a.ID())
	_, err = orderClaim(t, bp, task.TaskID, "c-00000000000b-hostb", o.ID)
	expectCode(t, err, api.CodeStartOrderRefused)
	if o, err = bp.AcceptStartOrder(t.Context(), "", o.ID); err != nil || o.State != api.StartOrderAccepted {
		t.Fatalf("accept = %+v %v", o, err)
	}
	if again, err := bp.AcceptStartOrder(t.Context(), "", o.ID); err != nil || again.State != api.StartOrderAccepted {
		t.Fatalf("repeated accept = %+v %v", again, err)
	}
	// Another client cannot claim with the order.
	_, err = orderClaim(t, a, task.TaskID, "c-00000000000a-hosta", o.ID)
	expectCode(t, err, api.CodeStartOrderRefused)

	grant, err := orderClaim(t, bp, task.TaskID, "c-00000000000b-hostb", o.ID)
	if err != nil {
		t.Fatal(err)
	}
	if o = orderState(t, a, o.ID); o.State != api.StartOrderAccepted || o.Generation != grant.Claim.Generation {
		t.Fatalf("bound order = %+v", o)
	}
	// A bound order cannot be cancelled.
	_, err = a.CancelStartOrder(t.Context(), "", o.ID)
	expectCode(t, err, api.CodeStartOrderRefused)

	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	if _, err := bp.Execution(t.Context(), "", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionWindow, Window: "tmux-session:$1:@1:%1"}, proof); err != nil {
		t.Fatal(err)
	}
	if _, err := bp.Execution(t.Context(), "", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionConfirm}, proof); err != nil {
		t.Fatal(err)
	}
	o = orderState(t, a, o.ID)
	if o.State != api.StartOrderStarted || o.Window != "tmux-session:$1:@1:%1" || o.Generation != grant.Claim.Generation {
		t.Fatalf("started order = %+v", o)
	}
	if len(b.beat().Orders) != 0 {
		t.Fatal("a started order is still listed for its host")
	}
	// A final order takes no further transition.
	_, err = a.CancelStartOrder(t.Context(), "", o.ID)
	expectCode(t, err, api.CodeStartOrderRefused)

	page, err := a.Events(t.Context(), cursor, 0)
	if err != nil {
		t.Fatal(err)
	}
	var states []string
	for _, e := range page.Events {
		if e.Kind == api.EventStartOrderChanged && e.OrderID == o.ID {
			states = append(states, e.State)
		}
	}
	if !slices.Equal(states, []string{api.StartOrderPending, api.StartOrderAccepted, api.StartOrderAccepted, api.StartOrderStarted}) {
		t.Fatalf("order events = %v", states)
	}
}

func TestStartOrderEndsWithItsGeneration(t *testing.T) {
	f := newFixture(t)
	a := f.project("c-00000000000a-hosta")
	b := f.host("c-00000000000b-hostb", "hostb")
	b.beat()
	bp := b.project(a.ID())
	bound := func(slug string) (api.Task, api.StartOrder, api.ClaimGrant) {
		task := todoTask(t, a, slug)
		o := newOrder(t, a, task.TaskID, "hostb")
		if _, err := bp.AcceptStartOrder(t.Context(), "", o.ID); err != nil {
			t.Fatal(err)
		}
		grant, err := orderClaim(t, bp, task.TaskID, "c-00000000000b-hostb", o.ID)
		if err != nil {
			t.Fatal(err)
		}
		return task, o, grant
	}

	// A rolled back launch fails its order.
	task, o, grant := bound("rollback")
	if _, err := bp.Execution(t.Context(), "", task.TaskID, api.ExecutionUpdate{Action: api.ExecutionRollback}, client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	if o = orderState(t, a, o.ID); o.State != api.StartOrderFailed || o.Reason == nil || o.Reason.Code != api.OrderReasonRolledBack {
		t.Fatalf("rolled back order = %+v", o)
	}
	// The card is back in todo and can take a new order.
	newOrder(t, a, task.TaskID, "hostb")

	// A takeover of the unconfirmed generation supersedes its order.
	task, o, grant = bound("takeover")
	f.clock.advance(91 * time.Second)
	b.beat()
	shown, err := a.Show(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Takeover(t.Context(), "", task.TaskID, api.ClaimRequest{BranchMode: "main-develop", ClientID: "c-00000000000a-hosta", ExecutionID: "exec-takeover", Owner: "claude", Decision: "user:takeover", ExpectedGeneration: grant.Claim.Generation, ExpectedRevision: shown.Revision}); err != nil {
		t.Fatal(err)
	}
	if o = orderState(t, a, o.ID); o.State != api.StartOrderFailed || o.Reason == nil || o.Reason.Code != api.OrderReasonSuperseded {
		t.Fatalf("superseded order = %+v", o)
	}
	// A late claim of a final order is refused.
	_, err = orderClaim(t, bp, todoTask(t, a, "late").TaskID, "c-00000000000b-hostb", o.ID)
	expectCode(t, err, api.CodeStartOrderRefused)
}

func TestStartOrderPreemptExpireHostLostAndCancel(t *testing.T) {
	f := newFixture(t)
	a := f.project("c-00000000000a-hosta")
	b := f.host("c-00000000000b-hostb", "hostb")
	b.beat()
	bp := b.project(a.ID())

	// A direct claim on any host takes the card: the open order fails and its
	// own claim is refused later.
	task := todoTask(t, a, "preempt")
	o := newOrder(t, a, task.TaskID, "hostb")
	if _, err := bp.AcceptStartOrder(t.Context(), "", o.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := claim(t, a, task.TaskID, "c-00000000000a-hosta"); err != nil {
		t.Fatal(err)
	}
	if o = orderState(t, a, o.ID); o.State != api.StartOrderFailed || o.Reason.Code != api.OrderReasonAlreadyClaimed {
		t.Fatalf("preempted order = %+v", o)
	}
	_, err := orderClaim(t, bp, task.TaskID, "c-00000000000b-hostb", o.ID)
	expectCode(t, err, api.CodeHeldByOther)

	// An order nobody takes expires on the server clock.
	pending := newOrder(t, a, todoTask(t, a, "expire").TaskID, "hostb")
	f.srv.SweepStartOrders()
	if orderState(t, a, pending.ID).State != api.StartOrderPending {
		t.Fatal("the sweep expired an order inside its window")
	}
	f.clock.advance(api.StartOrderAcceptWindow)
	b.beat()
	f.srv.SweepStartOrders()
	if o = orderState(t, a, pending.ID); o.State != api.StartOrderExpired || o.Reason.Code != api.OrderReasonExpired {
		t.Fatalf("expired order = %+v", o)
	}
	_, err = bp.AcceptStartOrder(t.Context(), "", pending.ID)
	expectCode(t, err, api.CodeStartOrderRefused)

	// An accepted order fails when its host goes silent before any claim.
	accepted := newOrder(t, a, todoTask(t, a, "lost").TaskID, "hostb")
	if _, err := bp.AcceptStartOrder(t.Context(), "", accepted.ID); err != nil {
		t.Fatal(err)
	}
	f.clock.advance(api.HostOnlineWindow)
	f.srv.SweepStartOrders()
	if o = orderState(t, a, accepted.ID); o.State != api.StartOrderFailed || o.Reason.Code != api.OrderReasonHostLost {
		t.Fatalf("host lost order = %+v", o)
	}

	// A pending order and an accepted one no claim took can be cancelled.
	b.beat()
	cancelled := newOrder(t, a, todoTask(t, a, "cancel").TaskID, "hostb")
	if o, err = a.CancelStartOrder(t.Context(), "", cancelled.ID); err != nil || o.State != api.StartOrderCancelled {
		t.Fatalf("cancel = %+v %v", o, err)
	}
	// The worker reports a failure before any claim.
	failing := newOrder(t, a, todoTask(t, a, "fail").TaskID, "hostb")
	if _, err := bp.AcceptStartOrder(t.Context(), "", failing.ID); err != nil {
		t.Fatal(err)
	}
	_, err = a.FailStartOrder(t.Context(), "", failing.ID, api.StartOrderFailure{Code: "registration_invalid", Message: "x"})
	expectCode(t, err, api.CodeStartOrderRefused)
	if o, err = bp.FailStartOrder(t.Context(), "", failing.ID, api.StartOrderFailure{Code: "registration_invalid", Message: "no\ncheckout"}); err != nil || o.State != api.StartOrderFailed || o.Reason.Message != "no checkout" {
		t.Fatalf("fail = %+v %v", o, err)
	}
}

func TestHeartbeatWakesOnANewOrder(t *testing.T) {
	f := newFixture(t)
	a := f.project("c-00000000000a-hosta")
	task := todoTask(t, a, "wake")
	b := f.host("c-00000000000b-hostb", "hostb")
	b.beat()
	done := make(chan api.HostHeartbeatResult, 1)
	go func() {
		result, err := b.c.Heartbeat(t.Context(), b.hb, 20*time.Second)
		if err != nil {
			t.Error(err)
		}
		done <- result
	}()
	time.Sleep(100 * time.Millisecond)
	o := newOrder(t, a, task.TaskID, "hostb")
	select {
	case result := <-done:
		if len(result.Orders) != 1 || result.Orders[0].ID != o.ID {
			t.Fatalf("woken heartbeat = %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the heartbeat did not return for the new order")
	}
}
