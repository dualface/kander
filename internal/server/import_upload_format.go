package server

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/dualface/kander/v2/api"
)

type uploadLimits struct {
	files, dirs, manifest int
	bytes, fileBytes      int64
	timeout               time.Duration
}

func defaultUploadLimits() uploadLimits {
	return uploadLimits{
		files: api.ImportUploadMaxFiles, dirs: api.ImportUploadMaxDirs, manifest: api.ImportUploadMaxManifest,
		bytes: api.ImportUploadMaxBytes, fileBytes: api.ImportUploadMaxFileBytes, timeout: api.ImportUploadTimeout,
	}
}

func (l uploadLimits) wireBytes() int64 {
	return l.bytes + int64(l.manifest) + 4 + int64(len(api.ImportUploadComplete))
}

func uploadTooLarge() error {
	return &api.Error{Code: api.CodeInvalidRequest, Status: http.StatusRequestEntityTooLarge, Message: "board upload exceeds its size or entry limit"}
}

func invalidUpload() error {
	return importRejected("malformed, incomplete, or inconsistent board upload; nothing was published")
}

// The archive format has no link, device, permission, or other file type
// fields. Validate portable path syntax before any entry reaches the disk.
func validUploadPath(rel string) bool {
	if len(rel) > 4096 || rel == "." || !fs.ValidPath(rel) || strings.ContainsAny(rel, "\\:<>|\"?*") {
		return false
	}
	private := rel == ".kander" || strings.HasPrefix(rel, ".kander/")
	for part := range strings.SplitSeq(rel, "/") {
		if len(part) > 255 || (strings.HasPrefix(part, ".") && !private) ||
			strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") || strings.ContainsFunc(part, unicode.IsControl) {
			return false
		}
	}
	_, err := filepath.Localize(rel)
	return err == nil
}

// source is a label only. It is never resolved on the receiving host.
func validUploadSource(source string) bool {
	if !utf8.ValidString(source) || len(source) > 131072 || strings.ContainsFunc(source, unicode.IsControl) {
		return false
	}
	if strings.HasPrefix(source, "/") || strings.HasPrefix(source, `\\`) {
		return true
	}
	return len(source) >= 3 && (source[0] >= 'A' && source[0] <= 'Z' || source[0] >= 'a' && source[0] <= 'z') &&
		source[1] == ':' && (source[2] == '\\' || source[2] == '/')
}

func readUploadManifest(r io.Reader, limits uploadLimits) (api.ImportUploadManifest, error) {
	var m api.ImportUploadManifest
	var size uint32
	if err := binary.Read(r, binary.BigEndian, &size); err != nil {
		return m, invalidUpload()
	}
	if int64(size) > int64(limits.manifest) {
		return m, uploadTooLarge()
	}
	body := &io.LimitedReader{R: r, N: int64(size)}
	d := json.NewDecoder(body)
	d.DisallowUnknownFields()
	if err := decodeUploadManifest(d, &m, limits); err != nil {
		return m, err
	}
	if _, err := d.Token(); err != io.EOF || body.N != 0 {
		return m, invalidUpload()
	}
	if err := checkUploadManifest(m, limits); err != nil {
		return m, err
	}
	return m, nil
}

// Decode each array element separately. A small manifest can declare many
// empty entries, so the file and directory limits must bound allocation too.
func decodeUploadManifest(d *json.Decoder, m *api.ImportUploadManifest, limits uploadLimits) error {
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return invalidUpload()
	}
	seen := make(map[string]bool)
	for d.More() {
		token, err := d.Token()
		key, ok := token.(string)
		if err != nil || !ok || seen[key] {
			return invalidUpload()
		}
		seen[key] = true
		switch key {
		case "project":
			err = d.Decode(&m.Project)
		case "remote":
			err = d.Decode(&m.Remote)
		case "from":
			err = d.Decode(&m.From)
		case "skip_conflicts":
			err = d.Decode(&m.SkipConflicts)
		case "snapshot":
			err = d.Decode(&m.Snapshot)
		case "directories":
			if err := readUploadEntries(d, &m.Directories, limits.dirs); err != nil {
				return err
			}
		case "files":
			var entries []uploadFileEntry
			if err := readUploadEntries(d, &entries, limits.files); err != nil {
				return err
			}
			m.Files = make([]api.ImportUploadFile, 0, len(entries))
			for _, entry := range entries {
				if entry.Size == nil {
					return invalidUpload()
				}
				m.Files = append(m.Files, api.ImportUploadFile{Path: entry.Path, Size: *entry.Size, SHA256: entry.SHA256})
			}
		default:
			return invalidUpload()
		}
		if err != nil {
			return invalidUpload()
		}
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') || !seen["directories"] || !seen["files"] {
		return invalidUpload()
	}
	return nil
}

type uploadFileEntry struct {
	Path   string `json:"path"`
	Size   *int64 `json:"size"`
	SHA256 string `json:"sha256"`
}

func readUploadEntries[T any](d *json.Decoder, entries *[]T, limit int) error {
	if token, err := d.Token(); err != nil || token != json.Delim('[') {
		return invalidUpload()
	}
	for d.More() {
		if len(*entries) == limit {
			return uploadTooLarge()
		}
		var entry T
		if err := d.Decode(&entry); err != nil {
			return invalidUpload()
		}
		*entries = append(*entries, entry)
	}
	if token, err := d.Token(); err != nil || token != json.Delim(']') {
		return invalidUpload()
	}
	return nil
}

func checkUploadManifest(m api.ImportUploadManifest, limits uploadLimits) error {
	if !api.ValidProjectName(m.Project) || !validUploadSource(m.From) || len(m.Remote) > 16<<10 || (m.Remote != "" && !api.SafeGitRemote(m.Remote)) {
		return invalidUpload()
	}
	if len(m.Files) > limits.files || len(m.Directories) > limits.dirs {
		return uploadTooLarge()
	}
	dirs := make(map[string]struct{}, len(m.Directories)+1)
	dirs["."] = struct{}{}
	for i, dir := range m.Directories {
		if _, parent := dirs[path.Dir(dir)]; !validUploadPath(dir) || !parent || (i > 0 && m.Directories[i-1] >= dir) {
			return invalidUpload()
		}
		dirs[dir] = struct{}{}
	}
	var total int64
	hash := sha256.New()
	for i, file := range m.Files {
		_, isDir := dirs[file.Path]
		_, parent := dirs[path.Dir(file.Path)]
		if !validUploadPath(file.Path) || !parent || isDir || file.Size < 0 || (i > 0 && m.Files[i-1].Path >= file.Path) {
			return invalidUpload()
		}
		if file.Size > limits.fileBytes || file.Size > limits.bytes-total {
			return uploadTooLarge()
		}
		total += file.Size
		var sum [sha256.Size]byte
		if len(file.SHA256) != sha256.Size*2 || strings.ToLower(file.SHA256) != file.SHA256 {
			return invalidUpload()
		}
		if _, err := hex.Decode(sum[:], []byte(file.SHA256)); err != nil {
			return invalidUpload()
		}
		hashImportEntry(hash, file.Path, sum)
	}
	if hex.EncodeToString(hash.Sum(nil)) != m.Snapshot {
		return invalidUpload()
	}
	return nil
}
