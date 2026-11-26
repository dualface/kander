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
	Project       string `json:"project"`
	Remote        string `json:"remote,omitempty"`
	From          string `json:"from"`
	SkipConflicts bool   `json:"skip_conflicts,omitzero"`
}

// ImportResult describes one board import. Source is the absolute source path
// of this request, also when the snapshot was imported earlier. Snapshot is the
// digest of the source board, and Files and Tasks count its entries before
// conflict handling. The same snapshot returns the earlier import outcome.
type ImportResult struct {
	Project          Project          `json:"project"`
	Source           string           `json:"source"`
	Files            int              `json:"files"`
	Tasks            int              `json:"tasks"`
	Snapshot         string           `json:"snapshot"`
	Existing         bool             `json:"existing,omitzero"`
	DefaultedSize    []string         `json:"defaulted_size,omitempty"`
	Warnings         []ImportWarning  `json:"warnings,omitempty"`
	AddedTasks       int              `json:"added_tasks,omitzero"`
	ExistingTasks    []string         `json:"existing_tasks,omitempty"`
	SkippedConflicts []ImportConflict `json:"skipped_conflicts,omitempty"`
}

// ImportConflict names a task or a producer's shared record skipped in a merge.
type ImportConflict struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// ImportWarning is a board check problem the import accepted because every
// card it concerns is closed (done, archived or trash).
type ImportWarning struct {
	TaskIDs []string `json:"task_ids"`
	Message string   `json:"message"`
}

// Import phases, in the order a first successful import reports them.
const (
	ImportPhaseLock    = "lock"
	ImportPhaseScan    = "scan"
	ImportPhaseCopy    = "copy"
	ImportPhaseVerify  = "verify"
	ImportPhaseCheck   = "check"
	ImportPhasePublish = "publish"
)

// ImportProgress reports where an import is. Done and Total count files and
// are set while the phase walks the source or the staged copy; Bytes and
// TotalBytes are the sizes of those files.
type ImportProgress struct {
	Phase      string `json:"phase"`
	Done       int    `json:"done,omitzero"`
	Total      int    `json:"total,omitzero"`
	Bytes      int64  `json:"bytes,omitzero"`
	TotalBytes int64  `json:"total_bytes,omitzero"`
}

// ImportStreamType is the media type a client puts in Accept to receive the
// import as newline-delimited JSON. The server answers every other request
// with the single JSON result of an import without progress.
const ImportStreamType = "application/x-ndjson"

// ImportStreamLine is one line of a streamed import. Exactly one of Progress,
// Result and Error is set. The last line is the Result, or the Error with the
// HTTP status the single-response form would have used.
type ImportStreamLine struct {
	Progress *ImportProgress `json:"progress,omitempty"`
	Result   *ImportResult   `json:"result,omitempty"`
	Status   int             `json:"status,omitempty"`
	Error    *Error          `json:"error,omitempty"`
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
