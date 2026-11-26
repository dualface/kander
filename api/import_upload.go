package api

import (
	"math"
	"time"
)

// ImportUploadRoute is the domain route for file uploads. The project name
// is in the path so an embedding host can authorize it before reading the body.
const ImportUploadRoute = "/api/v1/projects/{name}/import"

// ImportUploadType identifies the v1 upload archive: a big-endian uint32
// manifest length, that many bytes of UTF-8 JSON, the raw file bodies in
// manifest order and a completion marker. Nothing may follow the marker.
const ImportUploadType = "application/vnd.kander.board-upload.v1"

// Default upload limits. An embedding host can set others in
// ImportUploadLimits; the archive format is the same.
const (
	ImportUploadComplete     = "KANDER-BOARD-COMPLETE\n"
	ImportUploadMaxFiles     = 1_000_000
	ImportUploadMaxDirs      = 1_000_000
	ImportUploadMaxBytes     = 128 << 30
	ImportUploadMaxFileBytes = 256 << 20
	ImportUploadMaxManifest  = 256 << 20
	// ImportUploadTimeout is the receive deadline of an upload before its
	// manifest is read, and the fixed part of ImportUploadDeadline.
	ImportUploadTimeout = 30 * time.Minute
	// ImportUploadRate is the slowest transfer, in bytes per second, that
	// ImportUploadDeadline still allows for the declared file contents.
	ImportUploadRate = 8 << 20
)

// ImportUploadLimits bound one board upload. Every field must be positive,
// and Manifest must fit the uint32 length prefix of the archive.
type ImportUploadLimits struct {
	// Files is the number of regular files.
	Files int
	// Dirs is the number of directories.
	Dirs int
	// Bytes is the total size of the file contents.
	Bytes int64
	// FileBytes is the size of one file.
	FileBytes int64
	// Manifest is the size of the JSON manifest in bytes.
	Manifest int
}

// DefaultImportUploadLimits returns the limits a server uses when its host
// sets none.
func DefaultImportUploadLimits() ImportUploadLimits {
	return ImportUploadLimits{
		Files: ImportUploadMaxFiles, Dirs: ImportUploadMaxDirs, Bytes: ImportUploadMaxBytes,
		FileBytes: ImportUploadMaxFileBytes, Manifest: ImportUploadMaxManifest,
	}
}

// ImportUploadDeadline is the time an upload that declares bytes of file
// contents may take to arrive, counted from the start of the request:
// ImportUploadTimeout plus bytes at ImportUploadRate, rounded up to a second.
func ImportUploadDeadline(bytes int64) time.Duration {
	if bytes <= 0 {
		return ImportUploadTimeout
	}
	seconds := bytes / ImportUploadRate
	if bytes%ImportUploadRate != 0 {
		seconds++
	}
	if seconds > int64((math.MaxInt64-ImportUploadTimeout)/time.Second) {
		return math.MaxInt64
	}
	return ImportUploadTimeout + time.Duration(seconds)*time.Second
}

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
