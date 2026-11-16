package api

import "time"

// Server information and shutdown are independent of the board protocol
// handshake so an older local server can be restarted after a protocol
// upgrade. Import changes board data, so it also passes the handshake.
const HeaderAdminToken = "Kander-Admin-Token"
const AdminTokenFile = "admin-token.json"
const DefaultListen = "127.0.0.1:7878"
const DefaultTTL = 90 * time.Second
const DefaultRenewInterval = 15 * time.Second

// ImportRequest asks a running local server to import a v1 board. From is an
// absolute path on the server host; the route accepts only loopback peers with
// the administration token, as shutdown does.
type ImportRequest struct {
	Project string `json:"project"`
	Remote  string `json:"remote,omitempty"`
	From    string `json:"from"`
}

// ImportResult describes one board import. Source is the absolute source path
// of this request, also when the snapshot was imported earlier.
type ImportResult struct {
	Project  Project `json:"project"`
	Source   string  `json:"source"`
	Files    int     `json:"files"`
	Tasks    int     `json:"tasks"`
	Snapshot string  `json:"snapshot"`
	Existing bool    `json:"existing,omitzero"`
}

// ServerInfo describes the running instance, never its secrets.
type ServerInfo struct {
	URL       string    `json:"url"`
	Version   string    `json:"version"`
	PID       int       `json:"pid"`
	DataDir   string    `json:"data_dir"`
	LogPath   string    `json:"log_path"`
	Listen    string    `json:"listen"`
	TTL       Duration  `json:"ttl"`
	Renew     Duration  `json:"renew"`
	StartedAt time.Time `json:"started_at"`
	Projects  int       `json:"projects"`
}
