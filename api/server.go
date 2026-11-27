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
	ImportPhaseUpload  = "upload"
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

// ServerGCRequest asks a running local server to reclaim its data. Like
// import, the route accepts only loopback peers with the administration
// token. An empty Project means every registered project. OlderThan is a Go
// duration of at least one hour; LogKeep is the number of bytes of server.log
// to keep, at least 1 MiB. A dry run only plans.
type ServerGCRequest struct {
	Project   string `json:"project,omitempty"`
	DryRun    bool   `json:"dry_run,omitzero"`
	OlderThan string `json:"older_than"`
	LogKeep   int64  `json:"log_keep"`
}

// ServerGCReport is the plan of a dry run or the outcome of a gc. Projects
// lists every project the request covered, in name order. Log is nil when the
// request covered one project only. Errors lists failures outside a project.
type ServerGCReport struct {
	DryRun   bool              `json:"dry_run"`
	Projects []ServerGCProject `json:"projects"`
	Log      *ServerGCLog      `json:"log,omitempty"`
	Errors   []string          `json:"errors,omitempty"`
}

// ServerGCProject is the outcome of one project. Errors lists the failures
// that stopped a category of this project or skipped the whole project;
// Warnings lists notes of the review run compaction. Kept entries and errors
// make the command exit 1.
type ServerGCProject struct {
	Name       string             `json:"name"`
	Compaction ServerGCCompaction `json:"compaction"`
	Receipts   ServerGCReceipts   `json:"receipts"`
	Warnings   []string           `json:"warnings,omitempty"`
	Errors     []string           `json:"errors,omitempty"`
}

// ServerGCCompaction counts the review runs compacted (or, in a dry run, to
// compact) and the files and bytes their compaction removes.
type ServerGCCompaction struct {
	Runs  int            `json:"runs"`
	Files int            `json:"files"`
	Bytes int64          `json:"bytes"`
	Kept  []ServerGCKept `json:"kept,omitempty"`
}

// ServerGCReceipts counts the idempotency receipts deleted (or, in a dry run,
// to delete) and their bytes.
type ServerGCReceipts struct {
	Files int            `json:"files"`
	Bytes int64          `json:"bytes"`
	Kept  []ServerGCKept `json:"kept,omitempty"`
}

// ServerGCKept is a candidate a gc kept, with the reason.
type ServerGCKept struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

// ServerGCLog is the server.log trim. Size is the size before the trim, Bytes
// the bytes moved out of server.log into server.log.1.gz (0 when the log is
// not larger than the limit). Recovered is the tail an interrupted earlier
// trim saved aside: appended back to server.log first, or in a dry run, still
// to append back. Kept, when set, says why a due trim failed.
type ServerGCLog struct {
	Path      string `json:"path"`
	Size      int64  `json:"size"`
	Bytes     int64  `json:"bytes"`
	Recovered int64  `json:"recovered,omitempty"`
	Kept      string `json:"kept,omitempty"`
}

// Incomplete reports whether a gc kept a candidate or met an error, which
// makes the command exit 1.
func (r ServerGCReport) Incomplete() bool {
	if len(r.Errors) > 0 || r.Log != nil && r.Log.Kept != "" {
		return true
	}
	for _, p := range r.Projects {
		if len(p.Errors) > 0 || len(p.Compaction.Kept) > 0 || len(p.Receipts.Kept) > 0 {
			return true
		}
	}
	return false
}
