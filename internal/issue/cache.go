package issue

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/fs"
)

const (
	// CacheSchema is the version of the outer cache record. A record written by
	// another version is ignored instead of being reinterpreted.
	CacheSchema = 1

	// MaxCachedIssues and MaxCacheBytes bound the machine-local issue snapshot
	// cache. Pruning brings both back within the bounds by evicting the oldest
	// records first.
	MaxCachedIssues = 200
	MaxCacheBytes   = 16 << 20

	// MaxCacheFileBytes bounds one cache file. A snapshot whose record would
	// exceed it is not cached; an existing file above the bound is a miss.
	MaxCacheFileBytes = 1 << 20

	// cacheVersionDir is the versioned subtree of the cache root the snapshot
	// layout lives in, so a future layout can appear beside it.
	cacheVersionDir = "v1"
)

// CachedSnapshot is the outer record of one cached issue snapshot. The inner
// ImportSnapshot stays the versioned, sanitized and bounded payload that the
// card attachments also use; the outer fields carry the cache format version,
// the canonical source key, the fetch time the eviction order uses and the
// content digest.
type CachedSnapshot struct {
	SchemaVersion int            `json:"schema_version"`
	SourceKey     string         `json:"source_key"`
	FetchedAt     string         `json:"fetched_at"`
	Digest        string         `json:"digest"`
	Snapshot      ImportSnapshot `json:"snapshot"`
}

// CacheBounds bound the snapshot cache. A zero field keeps its default.
type CacheBounds struct {
	MaxEntries int
	MaxBytes   int64
}

// DefaultCacheBounds returns the documented cache bounds.
func DefaultCacheBounds() CacheBounds {
	return CacheBounds{MaxEntries: MaxCachedIssues, MaxBytes: MaxCacheBytes}
}

// CacheFilePath returns the absolute path of the cache file of one issue. The
// name is the hex sha256 of the canonical source key, so remote text never
// reaches the file name and one issue maps to exactly one file.
func CacheFilePath(root string, repository Repository, number int) (string, error) {
	key, err := repository.IssueSourceKey(number)
	if err != nil {
		return "", err
	}
	return filepath.Join(board.CacheRoot(root), "issues", cacheVersionDir, cacheFileName(key)), nil
}

func cacheFileName(sourceKey string) string {
	sum := sha256.Sum256([]byte(sourceKey))
	return hex.EncodeToString(sum[:]) + ".json"
}

// ReadCachedSnapshot returns the cached snapshot of one issue. A missing file, a
// file above MaxCacheFileBytes, a damaged or unreadable record, a cache version
// or identity mismatch, and content that no longer passes NormalizeSnapshot all
// report a miss: the cache is an optimization, so a miss never surfaces an error
// and never blocks the list.
func ReadCachedSnapshot(root string, repository Repository, number int) (IssueSnapshot, bool) {
	path, err := CacheFilePath(root, repository, number)
	if err != nil {
		return IssueSnapshot{}, false
	}
	file, err := fs.OpenRegularFileIfExists(root, path)
	if err != nil || file == nil {
		return IssueSnapshot{}, false
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || info.Size() > MaxCacheFileBytes {
		return IssueSnapshot{}, false
	}
	data, err := io.ReadAll(io.LimitReader(file, MaxCacheFileBytes+1))
	if err != nil || len(data) > MaxCacheFileBytes {
		return IssueSnapshot{}, false
	}
	var record CachedSnapshot
	if err := json.Unmarshal(data, &record); err != nil {
		return IssueSnapshot{}, false
	}
	if record.SchemaVersion != CacheSchema || record.SourceKey == "" {
		return IssueSnapshot{}, false
	}
	expected, err := repository.IssueSourceKey(number)
	if err != nil || record.SourceKey != expected || record.Snapshot.SourceKey != expected {
		return IssueSnapshot{}, false
	}
	if record.Snapshot.SchemaVersion != ImportSchema || record.Snapshot.Issue.Number != number {
		return IssueSnapshot{}, false
	}
	snapshot, err := SnapshotFromImport(record.Snapshot)
	if err != nil {
		return IssueSnapshot{}, false
	}
	if record.Digest == "" || record.Digest != SnapshotDigest(snapshot) {
		return IssueSnapshot{}, false
	}
	return snapshot, true
}

// WriteCachedSnapshot stores one normalized issue snapshot in the machine-local
// cache and prunes the cache back inside its bounds. The file is written
// atomically with private permissions below the board cache root. The cache is
// best effort: a snapshot that cannot be cached, including one whose record
// would exceed MaxCacheFileBytes, is simply not stored and the caller keeps
// using the fetched copy.
func WriteCachedSnapshot(root string, snapshot IssueSnapshot, bounds CacheBounds) error {
	record, err := BuildImportSnapshot(snapshot)
	if err != nil {
		return err
	}
	stored, err := SnapshotFromImport(record)
	if err != nil {
		return err
	}
	fetchedAt := stored.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now().UTC()
	}
	entry := CachedSnapshot{
		SchemaVersion: CacheSchema,
		SourceKey:     record.SourceKey,
		FetchedAt:     formatSnapshotTime(fetchedAt),
		Digest:        SnapshotDigest(stored),
		Snapshot:      record,
	}
	encoded, err := marshalCachedSnapshot(entry)
	if err != nil {
		return err
	}
	if len(encoded) > MaxCacheFileBytes {
		return nil
	}
	directory, err := board.EnsureCacheDir(root, "issues", cacheVersionDir)
	if err != nil {
		return err
	}
	if err := fs.WriteTextAtomic(root, filepath.Join(directory, cacheFileName(record.SourceKey)), string(encoded), true); err != nil {
		return err
	}
	return pruneCache(root, directory, bounds)
}

// SnapshotDigest identifies the content of one normalized snapshot: the issue
// updated time plus the body and the comments. It deliberately ignores the
// fetch time, which changes on every read, so an unchanged refresh can keep the
// displayed content and its scroll position.
func SnapshotDigest(snapshot IssueSnapshot) string {
	digest := sha256.New()
	digestField(digest, "updated_at", formatSnapshotTime(snapshot.UpdatedAt))
	digestField(digest, "body", snapshot.Body)
	for _, comment := range snapshot.Comments {
		digestField(digest, "comment_author", comment.Author)
		digestField(digest, "comment_created_at", formatSnapshotTime(comment.CreatedAt))
		digestField(digest, "comment_body", comment.Body)
	}
	return hex.EncodeToString(digest.Sum(nil))
}

func digestField(digest hash.Hash, name, value string) {
	fmt.Fprintf(digest, "%s\x00%d\x00%s", name, len(value), value)
}

// UnmarshalImportSnapshot decodes one machine-readable snapshot record and
// validates its schema version and source key, so a caller can trust
// record.SourceKey after a successful decode.
func UnmarshalImportSnapshot(data []byte) (ImportSnapshot, error) {
	var record ImportSnapshot
	if err := json.Unmarshal(data, &record); err != nil {
		return ImportSnapshot{}, WrapError(err, ErrorInvalidResponse, "import", Sanitize(err.Error()))
	}
	if record.SchemaVersion != ImportSchema {
		return ImportSnapshot{}, NewError(ErrorInvalidResponse, "import", strconv.Itoa(record.SchemaVersion))
	}
	if record.SourceKey == "" {
		return ImportSnapshot{}, NewError(ErrorInvalidResponse, "import", "source_key")
	}
	return record, nil
}

// SnapshotFromImport turns one stored snapshot record back into the
// provider-neutral view. The fields pass NormalizeSnapshot again, so a cached
// copy is sanitized and bounded exactly like a fresh provider reply; a record
// that no longer satisfies the bounds is an error, not a truncated view.
func SnapshotFromImport(record ImportSnapshot) (IssueSnapshot, error) {
	if record.SchemaVersion != ImportSchema {
		return IssueSnapshot{}, NewError(ErrorInvalidResponse, "import", strconv.Itoa(record.SchemaVersion))
	}
	createdAt, err := parseSnapshotTime(record.Issue.CreatedAt)
	if err != nil {
		return IssueSnapshot{}, NewError(ErrorInvalidResponse, "import", "created_at")
	}
	updatedAt, err := parseSnapshotTime(record.Issue.UpdatedAt)
	if err != nil {
		return IssueSnapshot{}, NewError(ErrorInvalidResponse, "import", "updated_at")
	}
	fetchedAt, _ := parseSnapshotTime(record.FetchedAt)
	snapshot := IssueSnapshot{
		Repository: Repository{
			Host:    record.Repository.Host,
			Owner:   record.Repository.Owner,
			Name:    record.Repository.Name,
			URL:     record.Repository.URL,
			Private: record.Repository.Private,
		},
		Number:         record.Issue.Number,
		Title:          record.Issue.Title,
		Body:           record.Issue.Body,
		State:          record.Issue.State,
		Author:         record.Issue.Author,
		URL:            record.SourceURL,
		Labels:         record.Issue.Labels,
		Assignees:      record.Issue.Assignees,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
		CommentsLoaded: record.CommentsLoaded,
		FetchedAt:      fetchedAt,
	}
	comments := make([]IssueComment, 0, len(record.Comments))
	for _, comment := range record.Comments {
		createdAt, err := parseSnapshotTime(comment.CreatedAt)
		if err != nil {
			return IssueSnapshot{}, NewError(ErrorInvalidResponse, "import", "comment created_at")
		}
		comments = append(comments, IssueComment{
			Author:    comment.Author,
			Body:      comment.Body,
			CreatedAt: createdAt,
		})
	}
	snapshot.Comments = comments
	return NormalizeSnapshot(snapshot)
}

func marshalCachedSnapshot(entry CachedSnapshot) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(entry); err != nil {
		return nil, WrapError(err, ErrorInvalidResponse, "cache", Sanitize(err.Error()))
	}
	return out.Bytes(), nil
}

// cacheEntry is one candidate of a prune pass. An entry whose record cannot be
// read has no fetch time and is evicted before every readable record.
type cacheEntry struct {
	name      string
	size      int64
	fetchedAt time.Time
	ordered   bool
}

// pruneCache brings the cache directory back within its bounds by deleting the
// oldest records first. It reads the records only when a bound is exceeded, so
// the common write stays a listing plus one stat per file.
func pruneCache(root, directory string, bounds CacheBounds) error {
	maxEntries := bounds.MaxEntries
	if maxEntries <= 0 {
		maxEntries = MaxCachedIssues
	}
	maxBytes := bounds.MaxBytes
	if maxBytes <= 0 {
		maxBytes = MaxCacheBytes
	}
	listing, err := fs.ListDirectory(root, directory)
	if err != nil {
		return err
	}
	entries := make([]cacheEntry, 0, len(listing))
	var total int64
	for _, item := range listing {
		if item.Kind != fs.KindFile || !strings.HasSuffix(item.Name, ".json") {
			continue
		}
		size, ok := cacheFileSize(root, filepath.Join(directory, item.Name))
		if !ok {
			continue
		}
		entries = append(entries, cacheEntry{name: item.Name, size: size})
		total += size
	}
	if len(entries) <= maxEntries && total <= maxBytes {
		return nil
	}
	for index := range entries {
		if entries[index].size > MaxCacheFileBytes {
			continue
		}
		fetchedAt, ok := cacheFileFetchedAt(root, filepath.Join(directory, entries[index].name))
		entries[index].fetchedAt = fetchedAt
		entries[index].ordered = ok
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].ordered != entries[j].ordered {
			return !entries[i].ordered
		}
		if entries[i].fetchedAt.Equal(entries[j].fetchedAt) {
			return entries[i].name < entries[j].name
		}
		return entries[i].fetchedAt.Before(entries[j].fetchedAt)
	})
	remaining := len(entries)
	for _, entry := range entries {
		if remaining <= maxEntries && total <= maxBytes {
			break
		}
		if _, err := fs.RemoveRegularFileIfExists(root, filepath.Join(directory, entry.name)); err != nil {
			return err
		}
		total -= entry.size
		remaining--
	}
	return nil
}

func cacheFileSize(root, path string) (int64, bool) {
	file, err := fs.OpenRegularFileIfExists(root, path)
	if err != nil || file == nil {
		return 0, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return 0, false
	}
	return info.Size(), true
}

func cacheFileFetchedAt(root, path string) (time.Time, bool) {
	data, found, err := fs.ReadRegularFileIfExists(root, path)
	if err != nil || !found {
		return time.Time{}, false
	}
	var record CachedSnapshot
	if err := json.Unmarshal(data, &record); err != nil {
		return time.Time{}, false
	}
	if record.SchemaVersion != CacheSchema {
		return time.Time{}, false
	}
	fetchedAt, err := parseSnapshotTime(record.FetchedAt)
	if err != nil {
		return time.Time{}, false
	}
	return fetchedAt, true
}
