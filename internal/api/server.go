package api

import "time"

// Server administration is independent of the board protocol handshake so an
// older local server can be restarted after a protocol upgrade.
const HeaderAdminToken = "Kander-Admin-Token"
const AdminTokenFile = "admin-token.json"
const DefaultListen = "127.0.0.1:7878"
const DefaultTTL = 90 * time.Second
const DefaultRenewInterval = 15 * time.Second

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
