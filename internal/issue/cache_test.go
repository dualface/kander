package issue

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func cacheTestSnapshot(number int, fetchedAt time.Time) IssueSnapshot {
	return IssueSnapshot{
		Repository: Repository{
			Host: "github.com", Owner: "dualface", Name: "kander",
			URL: "https://github.com/dualface/kander", Remote: "origin",
		},
		Number:         number,
		Title:          "issue " + strconv.Itoa(number),
		Body:           "body " + strconv.Itoa(number),
		State:          "open",
		Author:         "alice",
		Labels:         []string{"bug"},
		CreatedAt:      time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:      time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
		CommentsLoaded: true,
		Comments: []IssueComment{{
			Author:    "bob",
			Body:      "confirmed",
			CreatedAt: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
		}},
		FetchedAt: fetchedAt,
	}
}

func TestCachedSnapshotRoundTrip(t *testing.T) {
	root := t.TempDir()
	snapshot := cacheTestSnapshot(42, time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC))
	snapshot.Body = "\x1b[31mred\x1b[0m body"
	if err := WriteCachedSnapshot(root, snapshot, DefaultCacheBounds()); err != nil {
		t.Fatalf("write: %v", err)
	}

	path, err := CacheFilePath(root, snapshot.Repository, 42)
	if err != nil {
		t.Fatalf("path: %v", err)
	}
	directory := filepath.Join(root, ".kander", "caches", "issues", "v1")
	if filepath.Dir(path) != directory {
		t.Fatalf("path=%s want directory %s", path, directory)
	}
	key, err := snapshot.Repository.IssueSourceKey(42)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(key))
	if want := hex.EncodeToString(sum[:]) + ".json"; filepath.Base(path) != want {
		t.Fatalf("file name=%s want %s", filepath.Base(path), want)
	}
	if runtime.GOOS != "windows" {
		file, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if file.Mode().Perm() != 0o600 {
			t.Fatalf("cache file mode=%o", file.Mode().Perm())
		}
		dir, err := os.Stat(directory)
		if err != nil {
			t.Fatal(err)
		}
		if dir.Mode().Perm() != 0o700 {
			t.Fatalf("cache directory mode=%o", dir.Mode().Perm())
		}
	}

	got, ok := ReadCachedSnapshot(root, snapshot.Repository, 42)
	if !ok {
		t.Fatal("a written snapshot must read back")
	}
	if got.Number != 42 || got.Title != snapshot.Title || got.State != snapshot.State {
		t.Fatalf("snapshot=%+v", got)
	}
	if strings.Contains(got.Body, "\x1b") {
		t.Fatalf("cached body kept a control sequence: %q", got.Body)
	}
	if len(got.Comments) != 1 || got.Comments[0].Author != "bob" || got.Comments[0].Body != "confirmed" {
		t.Fatalf("comments=%+v", got.Comments)
	}
	if !got.FetchedAt.Equal(snapshot.FetchedAt) {
		t.Fatalf("fetched at=%s want %s", got.FetchedAt, snapshot.FetchedAt)
	}
	if SnapshotDigest(got) == "" {
		t.Fatal("digest must be derived from the content")
	}
}

func TestCacheFilePathRejectsInvalidIdentity(t *testing.T) {
	root := t.TempDir()
	repository := cacheTestSnapshot(1, time.Now()).Repository
	repository.URL = "https://evil.example/dualface/kander"
	if _, err := CacheFilePath(root, repository, 1); err == nil {
		t.Fatal("a repository whose URL contradicts its identity must be refused")
	}
	if _, err := CacheFilePath(root, repository, 0); err == nil {
		t.Fatal("a non-positive issue number must be refused")
	}
}

func TestReadCachedSnapshotMissesAreSilent(t *testing.T) {
	root := t.TempDir()
	snapshot := cacheTestSnapshot(42, time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC))
	if err := WriteCachedSnapshot(root, snapshot, DefaultCacheBounds()); err != nil {
		t.Fatalf("write: %v", err)
	}
	path, err := CacheFilePath(root, snapshot.Repository, 42)
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadCachedSnapshot(root, snapshot.Repository, 42); !ok {
		t.Fatal("the stored snapshot must hit")
	}
	if _, ok := ReadCachedSnapshot(root, snapshot.Repository, 41); ok {
		t.Fatal("an unknown issue must miss")
	}
	other := snapshot.Repository
	other.Owner = "someone-else"
	if _, ok := ReadCachedSnapshot(root, other, 42); ok {
		t.Fatal("another repository identity must miss")
	}

	mutate := func(t *testing.T, change func(*CachedSnapshot)) []byte {
		t.Helper()
		var record CachedSnapshot
		if err := json.Unmarshal(original, &record); err != nil {
			t.Fatal(err)
		}
		change(&record)
		data, err := marshalCachedSnapshot(record)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	cases := []struct {
		name   string
		mutate func(*CachedSnapshot)
		data   []byte
	}{
		{name: "unknown cache version", mutate: func(record *CachedSnapshot) { record.SchemaVersion = CacheSchema + 1 }},
		{name: "unknown snapshot version", mutate: func(record *CachedSnapshot) { record.Snapshot.SchemaVersion = ImportSchema + 1 }},
		{name: "wrong source key", mutate: func(record *CachedSnapshot) {
			record.SourceKey = "github://github.com/dualface/kander/issues/41"
		}},
		{name: "missing digest", mutate: func(record *CachedSnapshot) { record.Digest = "" }},
		{name: "digest mismatch", mutate: func(record *CachedSnapshot) { record.Digest = "0000" }},
		{name: "number mismatch", mutate: func(record *CachedSnapshot) { record.Snapshot.Issue.Number = 41 }},
		{name: "broken timestamp", mutate: func(record *CachedSnapshot) { record.Snapshot.Issue.CreatedAt = "yesterday" }},
		{name: "over-limit body", mutate: func(record *CachedSnapshot) {
			record.Snapshot.Issue.Body = strings.Repeat("x", MaxIssueBodyBytes+1)
		}},
		{name: "damaged record", data: []byte("{\"schema_version\": ")},
		{name: "oversized file", data: []byte(strings.Repeat("x", MaxCacheFileBytes+1))},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			data := test.data
			if data == nil {
				data = mutate(t, test.mutate)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, ok := ReadCachedSnapshot(root, snapshot.Repository, 42); ok {
				t.Fatal("a damaged cache entry must miss")
			}
		})
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := ReadCachedSnapshot(root, snapshot.Repository, 42); !ok {
		t.Fatal("the restored snapshot must hit")
	}
}

func TestSnapshotFromImportRerunsTheSanitizer(t *testing.T) {
	record, err := BuildImportSnapshot(cacheTestSnapshot(42, time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	record.Issue.Body = "\x1b[31mred\x1b[0m \x1b]8;;https://evil.example\x07link\x1b]8;;\x07"
	record.Comments[0].Body = "lines\x1b[2J"
	got, err := SnapshotFromImport(record)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	for _, text := range []string{got.Body, got.Comments[0].Body} {
		if strings.Contains(text, "\x1b") || strings.Contains(text, "evil.example") {
			t.Fatalf("sanitizer did not rerun: %q", text)
		}
	}
	record, err = BuildImportSnapshot(cacheTestSnapshot(42, time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	record.Issue.Body = strings.Repeat("x", MaxIssueBodyBytes+1)
	if _, err := SnapshotFromImport(record); err == nil {
		t.Fatal("an over-limit stored body must be rejected instead of truncated")
	}
}

func TestUnmarshalImportSnapshotValidatesTheHeader(t *testing.T) {
	record, err := BuildImportSnapshot(cacheTestSnapshot(42, time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC)))
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalImportSnapshot(record)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := UnmarshalImportSnapshot(data)
	if err != nil || decoded.SourceKey != record.SourceKey || decoded.Issue.Number != 42 {
		t.Fatalf("decoded=%+v err=%v", decoded, err)
	}
	if _, err := UnmarshalImportSnapshot([]byte("{")); err == nil {
		t.Fatal("damaged JSON must be rejected")
	}
	record.SchemaVersion = ImportSchema + 1
	data, err = MarshalImportSnapshot(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalImportSnapshot(data); err == nil {
		t.Fatal("an unknown schema version must be rejected")
	}
	record.SchemaVersion = ImportSchema
	record.SourceKey = ""
	data, err = MarshalImportSnapshot(record)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := UnmarshalImportSnapshot(data); err == nil {
		t.Fatal("a missing source key must be rejected")
	}
}

func TestCacheWriteReplacesOneIssue(t *testing.T) {
	root := t.TempDir()
	for _, body := range []string{"first", "second"} {
		snapshot := cacheTestSnapshot(42, time.Date(2026, 9, 11, 3, 0, 0, 0, time.UTC))
		snapshot.Body = body
		if err := WriteCachedSnapshot(root, snapshot, DefaultCacheBounds()); err != nil {
			t.Fatalf("write %s: %v", body, err)
		}
	}
	directory := filepath.Join(root, ".kander", "caches", "issues", "v1")
	entries, err := os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("one issue must map to one file: %v", entries)
	}
	got, ok := ReadCachedSnapshot(root, cacheTestSnapshot(42, time.Now()).Repository, 42)
	if !ok || got.Body != "second" {
		t.Fatalf("snapshot=%+v ok=%v", got, ok)
	}
}

func TestPruneEvictsTheOldestByEntryCount(t *testing.T) {
	root := t.TempDir()
	repository := cacheTestSnapshot(1, time.Now()).Repository
	bounds := CacheBounds{MaxEntries: 2, MaxBytes: MaxCacheBytes}
	for index, number := range []int{1, 2, 3} {
		snapshot := cacheTestSnapshot(number, time.Date(2026, 9, 11, 0, index, 0, 0, time.UTC))
		if err := WriteCachedSnapshot(root, snapshot, bounds); err != nil {
			t.Fatalf("write %d: %v", number, err)
		}
	}
	if _, ok := ReadCachedSnapshot(root, repository, 1); ok {
		t.Fatal("the oldest entry survived the count bound")
	}
	for _, number := range []int{2, 3} {
		if _, ok := ReadCachedSnapshot(root, repository, number); !ok {
			t.Fatalf("issue %d must survive the count bound", number)
		}
	}
}

func TestPruneEvictsTheOldestByTotalBytes(t *testing.T) {
	root := t.TempDir()
	repository := cacheTestSnapshot(1, time.Now()).Repository
	directory := filepath.Join(root, ".kander", "caches", "issues", "v1")
	write := func(number, minute int, bounds CacheBounds) {
		t.Helper()
		snapshot := cacheTestSnapshot(number, time.Date(2026, 9, 11, 0, minute, 0, 0, time.UTC))
		snapshot.Body = strings.Repeat("x", 4000)
		if err := WriteCachedSnapshot(root, snapshot, bounds); err != nil {
			t.Fatalf("write %d: %v", number, err)
		}
	}
	write(1, 0, CacheBounds{})
	entries, err := os.ReadDir(directory)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatal(err)
	}
	// Two records fit into the bound, three do not, so the oldest one has to go.
	bounds := CacheBounds{MaxEntries: MaxCachedIssues, MaxBytes: 2*info.Size() + 1}
	write(2, 1, bounds)
	write(3, 2, bounds)

	if _, ok := ReadCachedSnapshot(root, repository, 1); ok {
		t.Fatal("the oldest entry survived the byte bound")
	}
	for _, number := range []int{2, 3} {
		if _, ok := ReadCachedSnapshot(root, repository, number); !ok {
			t.Fatalf("issue %d must survive the byte bound", number)
		}
	}
	var total int64
	entries, err = os.ReadDir(directory)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		total += info.Size()
	}
	if total > bounds.MaxBytes {
		t.Fatalf("cache total=%d exceeds the bound %d", total, bounds.MaxBytes)
	}
}
