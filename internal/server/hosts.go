package server

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/dualface/kander/v2/api"
)

// hostsFile is the server-level record of the hosts that sent heartbeats.
const hostsFile = "hosts.json"

// Bounds of one heartbeat; a resident worker reports far less.
const (
	maxHostProjects    = 256
	maxHostNames       = 64
	maxHostVersionSize = 128
)

// hostRecord is one host as stored: its last heartbeat and the server time of
// that heartbeat. ConflictAt is the last time two worker IDs of the client ID
// sent heartbeats within the online window of each other.
type hostRecord struct {
	api.HostHeartbeat
	LastHeartbeat time.Time `json:"last_heartbeat"`
	ConflictAt    time.Time `json:"conflict_at,omitzero"`
}

type hostRegistry struct {
	Hosts []hostRecord `json:"hosts"`
}

// hostBook holds the hosts of the server and wakes the heartbeats that wait
// for a start order. mu guards hosts; it is never held across a project lock.
type hostBook struct {
	mu    sync.Mutex
	hosts map[string]hostRecord
	// wake is closed and replaced when a start order changes, so every waiting
	// heartbeat looks for its orders again.
	wakeMu sync.Mutex
	wake   chan struct{}
}

func (b *hostBook) changed() <-chan struct{} {
	b.wakeMu.Lock()
	defer b.wakeMu.Unlock()
	if b.wake == nil {
		b.wake = make(chan struct{})
	}
	return b.wake
}

// notify wakes every waiting heartbeat.
func (b *hostBook) notify() {
	b.wakeMu.Lock()
	defer b.wakeMu.Unlock()
	if b.wake != nil {
		close(b.wake)
	}
	b.wake = make(chan struct{})
}

func (s *Server) loadHosts() error {
	var reg hostRegistry
	if _, err := readJSON(s.data, hostsFile, &reg); err != nil {
		return err
	}
	s.hosts.hosts = map[string]hostRecord{}
	for _, h := range reg.Hosts {
		if api.ValidIdentity(h.ClientID) {
			s.hosts.hosts[h.ClientID] = h
		}
	}
	return nil
}

// writeHostsLocked replaces hosts.json; the caller holds s.hosts.mu.
func (s *Server) writeHostsLocked() error {
	reg := hostRegistry{Hosts: make([]hostRecord, 0, len(s.hosts.hosts))}
	for _, h := range s.hosts.hosts {
		reg.Hosts = append(reg.Hosts, h)
	}
	slices.SortFunc(reg.Hosts, func(a, b hostRecord) int { return strings.Compare(a.Name, b.Name) })
	return writeJSON(s.data, hostsFile, reg)
}

// hostView renders a record with the online state and the conflict on the server clock.
func hostView(h hostRecord, now time.Time) api.Host {
	view := api.Host{
		ClientID: h.ClientID, Name: h.Name, WorkerID: h.WorkerID, Version: h.Version, Accepting: h.Accepting,
		OrderProtocol: h.OrderProtocol, Projects: h.Projects, LastHeartbeat: h.LastHeartbeat,
		Online: now.Sub(h.LastHeartbeat) < api.HostOnlineWindow,
	}
	if view.Projects == nil {
		view.Projects = []api.HostProject{}
	}
	if !h.ConflictAt.IsZero() && now.Sub(h.ConflictAt) < api.HostOnlineWindow {
		view.Conflict = api.ConflictClientID
	}
	return view
}

// hostByName returns the host that uses name.
func (s *Server) hostByName(name string) (hostRecord, bool) {
	s.hosts.mu.Lock()
	defer s.hosts.mu.Unlock()
	for _, h := range s.hosts.hosts {
		if h.Name == name {
			return h, true
		}
	}
	return hostRecord{}, false
}

// hostByClient returns the host of a client ID.
func (s *Server) hostByClient(clientID string) (hostRecord, bool) {
	s.hosts.mu.Lock()
	defer s.hosts.mu.Unlock()
	h, ok := s.hosts.hosts[clientID]
	return h, ok
}

func (s *Server) hostList() api.HostList {
	s.hosts.mu.Lock()
	defer s.hosts.mu.Unlock()
	now := s.now()
	list := api.HostList{Hosts: make([]api.Host, 0, len(s.hosts.hosts))}
	for _, h := range s.hosts.hosts {
		list.Hosts = append(list.Hosts, hostView(h, now))
	}
	slices.SortFunc(list.Hosts, func(a, b api.Host) int { return strings.Compare(a.Name, b.Name) })
	return list
}

func validHeartbeat(hb *api.HostHeartbeat) error {
	if !api.ValidIdentity(hb.ClientID) || !api.ValidIdentity(hb.WorkerID) {
		return apiError(api.CodeInvalidRequest, "client_id and worker_id must be identities")
	}
	if !api.ValidHostName(hb.Name) {
		return apiError(api.CodeInvalidRequest, "invalid host name")
	}
	if hb.Version == "" || len(hb.Version) > maxHostVersionSize || strings.ContainsAny(hb.Version, "\r\n") {
		return apiError(api.CodeInvalidRequest, "invalid worker version")
	}
	if len(hb.Projects) > maxHostProjects {
		return apiError(api.CodeInvalidRequest, "too many projects in one heartbeat")
	}
	seen := map[string]bool{}
	for i, p := range hb.Projects {
		if !api.ValidProjectName(p.Project) || seen[p.Project] {
			return apiError(api.CodeInvalidRequest, "invalid or repeated project name")
		}
		seen[p.Project] = true
		if len(p.Agents) > maxHostNames || len(p.Launchers) > maxHostNames {
			return apiError(api.CodeInvalidRequest, "too many agents or launchers in one heartbeat")
		}
		for _, name := range append(slices.Clone(p.Agents), p.Launchers...) {
			if !api.ValidIdentity(name) {
				return apiError(api.CodeInvalidRequest, "invalid agent or launcher name")
			}
		}
		hb.Projects[i].Agents = sortedUnique(p.Agents)
		hb.Projects[i].Launchers = sortedUnique(p.Launchers)
	}
	return nil
}

func sortedUnique(values []string) []string {
	out := slices.Clone(values)
	slices.Sort(out)
	out = slices.Compact(out)
	if out == nil {
		out = []string{}
	}
	return out
}

// recordHeartbeat stores a heartbeat on the server clock. A host name another
// client ID uses is refused; a second worker ID of the same client ID within
// the online window marks the conflict.
func (s *Server) recordHeartbeat(hb api.HostHeartbeat) (api.Host, error) {
	s.hosts.mu.Lock()
	defer s.hosts.mu.Unlock()
	if s.hosts.hosts == nil {
		s.hosts.hosts = map[string]hostRecord{}
	}
	for _, other := range s.hosts.hosts {
		if other.Name == hb.Name && other.ClientID != hb.ClientID {
			return api.Host{}, apiError(api.CodeHostNameConflict, "host name "+hb.Name+" is used by client "+other.ClientID)
		}
	}
	now := s.now()
	previous, found := s.hosts.hosts[hb.ClientID]
	record := hostRecord{HostHeartbeat: hb, LastHeartbeat: now, ConflictAt: previous.ConflictAt}
	if found && previous.WorkerID != hb.WorkerID && now.Sub(previous.LastHeartbeat) < api.HostOnlineWindow {
		record.ConflictAt = now
		s.log.Warn("two workers use one client ID", "client", hb.ClientID, "worker", hb.WorkerID, "previous_worker", previous.WorkerID)
	}
	s.hosts.hosts[hb.ClientID] = record
	if err := s.writeHostsLocked(); err != nil {
		if found {
			s.hosts.hosts[hb.ClientID] = previous
		} else {
			delete(s.hosts.hosts, hb.ClientID)
		}
		return api.Host{}, err
	}
	return hostView(record, now), nil
}

func (s *Server) hostRoutes(mux *routeTable) {
	mux.HandleFunc("GET "+api.HostsRoute, s.domain(func(*http.Request) outcome {
		return outcome{status: http.StatusOK, body: s.hostList()}
	}))
	mux.HandleFunc("POST "+api.HostHeartbeatRoute, s.domain(func(r *http.Request) outcome {
		wait, err := eventWait(r)
		if err != nil {
			return outcome{err: err}
		}
		wait = min(wait, api.HostHeartbeatMaxWait)
		var hb api.HostHeartbeat
		if err := decode(r, &hb); err != nil {
			return outcome{err: err}
		}
		if err := validHeartbeat(&hb); err != nil {
			return outcome{err: err}
		}
		host, err := s.recordHeartbeat(hb)
		if err != nil {
			return outcome{err: err}
		}
		ctx, cancel := context.WithTimeout(r.Context(), wait)
		defer cancel()
		stop := context.AfterFunc(s.shutdown, cancel)
		defer stop()
		orders, err := s.waitHostOrders(ctx, hb.ClientID)
		if err != nil {
			return outcome{err: err}
		}
		return outcome{status: http.StatusOK, body: api.HostHeartbeatResult{Host: host, Orders: orders}}
	}))
}

// waitHostOrders returns the open orders of a host at once when one of them is
// pending; otherwise it waits until ctx ends or an order changes, and answers
// the open orders then.
func (s *Server) waitHostOrders(ctx context.Context, clientID string) ([]api.StartOrder, error) {
	for {
		changed := s.hosts.changed()
		orders, err := s.hostOrders(clientID)
		if err != nil {
			return nil, err
		}
		if ctx.Err() != nil || slices.ContainsFunc(orders, func(o api.StartOrder) bool { return o.State == api.StartOrderPending }) {
			return orders, nil
		}
		select {
		case <-ctx.Done():
			return s.hostOrders(clientID)
		case <-changed:
		}
	}
}

// hostOrders lists the open start orders that target a host, over every
// project, in creation order.
func (s *Server) hostOrders(clientID string) ([]api.StartOrder, error) {
	s.mu.Lock()
	projects := make([]*project, 0, len(s.projects))
	for _, p := range s.projects {
		projects = append(projects, p)
	}
	s.mu.Unlock()
	orders := []api.StartOrder{}
	for _, p := range projects {
		open, err := p.openOrdersOf(clientID)
		if err != nil {
			return nil, err
		}
		orders = append(orders, open...)
	}
	slices.SortFunc(orders, func(a, b api.StartOrder) int {
		if c := a.CreatedAt.Compare(b.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	return orders, nil
}
