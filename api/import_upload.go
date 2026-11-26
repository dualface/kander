package api

import "time"

// ImportUploadRoute is the domain route for file uploads. The project name
// is in the path so an embedding host can authorize it before reading the body.
const ImportUploadRoute = "/api/v1/projects/{name}/import"

// ImportUploadType identifies the v1 upload archive: a big-endian uint32
// manifest length, that many bytes of UTF-8 JSON, the raw file bodies in
// manifest order and a completion marker. Nothing may follow the marker.
const ImportUploadType = "application/vnd.kander.board-upload.v1"

const (
	ImportUploadComplete     = "KANDER-BOARD-COMPLETE\n"
	ImportUploadMaxFiles     = 100_000
	ImportUploadMaxDirs      = 100_000
	ImportUploadMaxBytes     = 2 << 30
	ImportUploadMaxFileBytes = 64 << 20
	ImportUploadMaxManifest  = 64 << 20
	ImportUploadTimeout      = 30 * time.Minute
)

// ImportUploadManifest describes one recovered v1 source board. Directories
// and files are sorted by their slash-separated paths. Directories preserve
// the shape of the source board without adding any bytes to the snapshot.
type ImportUploadManifest struct {
	ImportRequest                    // From is the client-side path, a display label only.
	Snapshot      string             `json:"snapshot"`
	Directories   []string           `json:"directories"`
	Files         []ImportUploadFile `json:"files"`
}

// ImportUploadFile describes a regular file. Size is the exact framed body
// length, and SHA256 is the lowercase hex digest of that body.
type ImportUploadFile struct {
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
