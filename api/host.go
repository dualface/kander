package api

import (
	"regexp"
	"time"
)

// Host registration, host heartbeats and start orders: a host's resident worker
// reports which projects it can start cards of, and any host of a project can
// ask a named host to start a todo card. See docs/distributed-server.md
// "Hosts and start orders".

// Routes of hosts and start orders. HostsRoute and HostHeartbeatRoute belong to
// the server; the start order routes to a project.
const (
	HostsRoute         = "/api/v1/hosts"
	HostHeartbeatRoute = "/api/v1/hosts/heartbeat"
	StartOrdersRoute   = "/api/v1/projects/{project}/start-orders"
	StartOrderRoute    = StartOrdersRoute + "/{order}"
)

// HostOnlineWindow is how long after its last heartbeat, on the server clock, a
// host counts as online. The same window detects two workers of one client ID.
const HostOnlineWindow = 60 * time.Second

// HostHeartbeatMaxWait bounds the long poll of one heartbeat.
const HostHeartbeatMaxWait = 25 * time.Second

// StartOrderAcceptWindow is how long, on the server clock, an order waits for
// its target host to take it before it expires.
const StartOrderAcceptWindow = 120 * time.Second

// StartOrderProtocol is the start order protocol a worker speaks; a host that
// reports a lower one cannot take orders.
const StartOrderProtocol = 1

// Error codes of hosts and start orders.
const (
	// CodeHostNameConflict refuses a heartbeat whose host name another client
	// ID already uses on this server.
	CodeHostNameConflict = "host_name_conflict"
	// CodeRemoteRequired refuses an order for a project without a Git remote.
	CodeRemoteRequired = "remote_required"
	// CodeTaskNotTodo refuses an order for a card that is not in todo.
	CodeTaskNotTodo = "task_not_todo"
	// CodeHostUnknown refuses an order for a host that never sent a heartbeat.
	CodeHostUnknown = "host_unknown"
	// CodeHostOffline refuses an order for a host whose last heartbeat is older
	// than HostOnlineWindow.
	CodeHostOffline = "host_offline"
	// CodeHostUnsupported refuses an order for a host that does not take orders
	// or speaks an older start order protocol.
	CodeHostUnsupported = "host_unsupported"
	// CodeHostProjectMissing refuses an order for a host that did not report the project.
	CodeHostProjectMissing = "host_project_missing"
	// CodeHostAgentUnavailable refuses an order whose agent the host did not report.
	CodeHostAgentUnavailable = "host_agent_unavailable"
	// CodeHostLauncherUnavailable refuses an order whose launcher the host did not report.
	CodeHostLauncherUnavailable = "host_launcher_unavailable"
	// CodeStartOrderOpen refuses an order for a card that already has an
	// order that is not finished.
	CodeStartOrderOpen = "start_order_open"
	// CodeStartOrderRefused refuses a transition the start order does not allow
	// in its state: a claim, an acceptance, a failure report, or a cancel.
	CodeStartOrderRefused = "start_order_refused"
)

// HostNamePattern is the host name rule: an ASCII letter or digit, then up to
// 62 ASCII letters, digits, or hyphens. internal/config keeps a copy because it
// cannot import this package; a test pins the two together.
const HostNamePattern = `^[A-Za-z0-9][A-Za-z0-9-]{0,62}$`

var hostNameRe = regexp.MustCompile(HostNamePattern)

// ValidHostName reports whether name can name a host.
func ValidHostName(name string) bool { return hostNameRe.MatchString(name) }

// HostProject is one project a host can start cards of: the project name and
// the names of the agents and launchers the host found usable for it. It never
// carries a path, a PATH, or an environment value.
type HostProject struct {
	Project   string   `json:"project"`
	Agents    []string `json:"agents"`
	Launchers []string `json:"launchers"`
}

// HostHeartbeat is POST /api/v1/hosts/heartbeat?wait=S by a host's resident
// worker. WorkerID is random per worker process, so two workers of one client
// ID are told apart.
type HostHeartbeat struct {
	ClientID      string        `json:"client_id"`
	Name          string        `json:"name"`
	WorkerID      string        `json:"worker_id"`
	Version       string        `json:"version"`
	Accepting     bool          `json:"accepting"`
	OrderProtocol int           `json:"order_protocol"`
	Projects      []HostProject `json:"projects"`
}

// Host is one host as the server knows it. Online and Conflict use the server
// clock: Conflict is "client_id_conflict" while two worker IDs of the client
// ID sent heartbeats within HostOnlineWindow of each other.
type Host struct {
	ClientID      string        `json:"client_id"`
	Name          string        `json:"name"`
	WorkerID      string        `json:"worker_id"`
	Version       string        `json:"version"`
	Accepting     bool          `json:"accepting"`
	OrderProtocol int           `json:"order_protocol"`
	Projects      []HostProject `json:"projects"`
	LastHeartbeat time.Time     `json:"last_heartbeat"`
	Online        bool          `json:"online"`
	Conflict      string        `json:"conflict,omitempty"`
}

// ConflictClientID marks a client ID that two workers used within the online window.
const ConflictClientID = "client_id_conflict"

// HostList is GET /api/v1/hosts.
type HostList struct {
	Hosts []Host `json:"hosts"`
}

// HostHeartbeatResult answers a heartbeat: the host as recorded and the start
// orders of this host that are not finished, in creation order: the pending
// orders it may take and the accepted orders it took.
type HostHeartbeatResult struct {
	Host   Host         `json:"host"`
	Orders []StartOrder `json:"orders"`
}

// States of a start order. Started, failed, cancelled and expired are final.
const (
	StartOrderPending   = "pending"
	StartOrderAccepted  = "accepted"
	StartOrderStarted   = "started"
	StartOrderFailed    = "failed"
	StartOrderCancelled = "cancelled"
	StartOrderExpired   = "expired"
)

// Reasons the server records for a finished start order.
const (
	OrderReasonAlreadyClaimed = "already_claimed"
	OrderReasonHostLost       = "host_lost"
	OrderReasonSuperseded     = "superseded"
	OrderReasonRolledBack     = "launch_rolled_back"
	OrderReasonExpired        = "expired"
	OrderReasonCancelled      = "cancelled"
)

// StartOrderFinal reports whether state is a final start order state.
func StartOrderFinal(state string) bool {
	switch state {
	case StartOrderStarted, StartOrderFailed, StartOrderCancelled, StartOrderExpired:
		return true
	}
	return false
}

// OrderReason says why an order finished other than started: Code is a stable
// code, Message a diagnostic.
type OrderReason struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}

// StartOrder is one start order. Generation is the claim generation the order
// is bound to (0 until the target host's start claimed the card), and Window the
// WINDOW the confirmed start recorded.
type StartOrder struct {
	ID                string       `json:"id"`
	ProjectID         string       `json:"project_id"`
	TaskID            string       `json:"task_id"`
	TargetClientID    string       `json:"target_client_id"`
	TargetHost        string       `json:"target_host"`
	Agent             string       `json:"agent,omitempty"`
	Launcher          string       `json:"launcher,omitempty"`
	RequesterClientID string       `json:"requester_client_id"`
	State             string       `json:"state"`
	Reason            *OrderReason `json:"reason,omitempty"`
	Generation        uint64       `json:"generation,omitzero"`
	Window            string       `json:"window,omitempty"`
	CreatedAt         time.Time    `json:"created_at"`
	UpdatedAt         time.Time    `json:"updated_at"`
}

// NewStartOrder is POST /api/v1/projects/{project}/start-orders. Host is the
// target host name; Agent and Launcher are optional and must be among those the
// host reported for the project when given. The requester is the client ID of
// the request header.
type NewStartOrder struct {
	TaskID   string `json:"task_id"`
	Host     string `json:"host"`
	Agent    string `json:"agent,omitempty"`
	Launcher string `json:"launcher,omitempty"`
}

// StartOrderFailure is POST .../start-orders/{order}/fail by the target host:
// the start failed before it claimed the card.
type StartOrderFailure struct {
	Code    string `json:"code"`
	Message string `json:"message,omitempty"`
}
