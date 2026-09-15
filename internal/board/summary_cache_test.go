package board

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"sync"
	"testing"
	"time"
)

func summaryCacheBoard(t *testing.T, n int) (string, []string) {
	t.Helper()
	resetLang(t)
	root := tempBoard(t)
	ids := make([]string, 0, n)
	for i := 0; i < n; i++ {
		slug := "sum" + itoa(i)
		large := i%2 == 0
		path, err := NewTask(root, "chore", slug, "Summary "+itoa(i), "en", large)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, filepath.Base(path))
	}
	return root, ids
}

func mustIndex(t *testing.T, root string) *SummaryIndex {
	t.Helper()
	idx, err := NewSummaryIndex(root, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(idx.Close)
	return idx
}

func mustView(t *testing.T, idx *SummaryIndex) BoardView {
	t.Helper()
	view, err := idx.View(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return view
}

func requireMatchingPayload(t *testing.T, root string, view BoardView) {
	t.Helper()
	payload, err := BoardPayload(root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(view.Warnings, payload.Warnings) {
		t.Fatalf("warnings %#v vs %#v", view.Warnings, payload.Warnings)
	}
	if !reflect.DeepEqual(view.Tasks, payload.Tasks) {
		t.Fatalf("tasks\n got %#v\nwant %#v", view.Tasks, payload.Tasks)
	}
}

func locateCard(t *testing.T, root, id string) Entry {
	t.Helper()
	scanned, err := scan(root)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := scanned.Entries[id]
	if !ok {
		t.Fatalf("missing %s", id)
	}
	return entry
}

func TestStrongIntervalAndCanonicalRoot(t *testing.T) {
	if StrongInterval(5) != 60*time.Second || StrongInterval(60) != 60*time.Second || StrongInterval(90) != 90*time.Second {
		t.Fatalf("StrongInterval: 5=%s 60=%s 90=%s", StrongInterval(5), StrongInterval(60), StrongInterval(90))
	}
	if _, err := CanonicalRoot(" \t"); err == nil {
		t.Fatal("empty root")
	}
	root := t.TempDir()
	got, err := CanonicalRoot(root)
	if err != nil || got != filepath.Clean(root) {
		t.Fatalf("canonical %q %v", got, err)
	}
}

func TestSummaryCacheFirstLoadMatchesPayloadAndHotReuse(t *testing.T) {
	root, ids := summaryCacheBoard(t, 3)
	idx := mustIndex(t, root)
	first := mustView(t, idx)
	if got := idx.Stats(); got.DocumentReads != len(ids) || got.Parses != len(ids) || got.Reused || got.Cards != len(ids) {
		t.Fatalf("first stats %+v want reads/parses=%d", got, len(ids))
	}
	requireMatchingPayload(t, root, first)

	second := mustView(t, idx)
	stats := idx.Stats()
	if stats.DocumentReads != 0 || stats.Parses != 0 || !stats.Reused {
		t.Fatalf("hot stats %+v", stats)
	}
	if len(first.Tasks) == 0 || &first.Tasks[0] != &second.Tasks[0] {
		t.Fatal("unchanged round rebuilt the sorted slice")
	}
	requireMatchingPayload(t, root, second)
}

func TestSummaryCacheIncrementalSingleAndSevenCardEdits(t *testing.T) {
	root, ids := summaryCacheBoard(t, 7)
	idx := mustIndex(t, root)
	mustView(t, idx)

	setMeta(t, locateCard(t, root, ids[0]).Document, "Summary 0", "Summary A")
	view := mustView(t, idx)
	if got := idx.Stats(); got.DocumentReads != 1 || got.Parses != 1 || got.Reused {
		t.Fatalf("single edit stats %+v", got)
	}
	if view.Tasks[0].Title != "Summary A" && !titlePresent(view, "Summary A") {
		t.Fatalf("missing edited title: %+v", view.Tasks)
	}
	requireMatchingPayload(t, root, view)

	for _, id := range ids {
		setMeta(t, locateCard(t, root, id).Document, "Summary", "Changed")
	}
	view = mustView(t, idx)
	if got := idx.Stats(); got.DocumentReads != 7 || got.Parses != 7 || got.Reused {
		t.Fatalf("seven edit stats %+v", got)
	}
	requireMatchingPayload(t, root, view)
}

func titlePresent(view BoardView, title string) bool {
	for _, task := range view.Tasks {
		if task.Title == title {
			return true
		}
	}
	return false
}

func TestSummaryCacheAddDeleteMoveAndFormChange(t *testing.T) {
	root, ids := summaryCacheBoard(t, 7)
	idx := mustIndex(t, root)
	mustView(t, idx)

	for i, state := range States {
		if state == "backlog" {
			continue
		}
		from := locateCard(t, root, ids[i]).Path
		to := filepath.Join(root, state, filepath.Base(from))
		if err := os.Rename(from, to); err != nil {
			t.Fatal(err)
		}
	}
	view := mustView(t, idx)
	if got := idx.Stats(); got.DocumentReads != 6 || got.Parses != 6 {
		t.Fatalf("move stats %+v", got)
	}
	seen := map[string]bool{}
	for _, task := range view.Tasks {
		seen[task.State] = true
	}
	for _, state := range States {
		if !seen[state] {
			t.Fatalf("missing state %s", state)
		}
	}
	requireMatchingPayload(t, root, view)

	removed := locateCard(t, root, ids[0])
	if err := os.RemoveAll(removed.Path); err != nil {
		t.Fatal(err)
	}
	view = mustView(t, idx)
	if got := idx.Stats(); got.Cards != 6 || got.Reused {
		t.Fatalf("delete stats %+v", got)
	}
	requireMatchingPayload(t, root, view)

	path, err := NewTask(root, "chore", "sumnew", "Summary new", "en", true)
	if err != nil {
		t.Fatal(err)
	}
	view = mustView(t, idx)
	if got := idx.Stats(); got.DocumentReads != 1 || got.Parses != 1 || got.Cards != 7 {
		t.Fatalf("add stats %+v", got)
	}
	if !titlePresent(view, "Summary new") {
		t.Fatal("added card missing")
	}
	requireMatchingPayload(t, root, view)

	id := filepath.Base(path)
	entry := locateCard(t, root, id)
	text, err := os.ReadFile(entry.Document)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(entry.Path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "backlog", id+".md"), text, 0o600); err != nil {
		t.Fatal(err)
	}
	view = mustView(t, idx)
	if got := idx.Stats(); got.DocumentReads != 1 || got.Parses != 1 {
		t.Fatalf("form stats %+v", got)
	}
	requireMatchingPayload(t, root, view)
}

func TestSummaryCacheRevisionLocationAndAtomicReplace(t *testing.T) {
	root, ids := summaryCacheBoard(t, 2)
	idx := mustIndex(t, root)
	mustView(t, idx)

	moved := locateCard(t, root, ids[1])
	if err := os.Rename(moved.Path, filepath.Join(root, "todo", filepath.Base(moved.Path))); err != nil {
		t.Fatal(err)
	}
	rec, err := readVersion(root, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	rec.Revision++
	if err := writeJSON(root, control(root, "versions", ids[0]+".json"), rec, true); err != nil {
		t.Fatal(err)
	}
	view := mustView(t, idx)
	if got := idx.Stats(); got.DocumentReads != 2 || got.Parses != 2 {
		t.Fatalf("revision/move stats %+v", got)
	}
	requireMatchingPayload(t, root, view)

	entry := locateCard(t, root, ids[0])
	body, err := os.ReadFile(entry.Document)
	if err != nil {
		t.Fatal(err)
	}
	replaced := bytes.Replace(body, []byte("Summary 0"), []byte("Replaced 0"), 1)
	tmp := entry.Document + ".tmp"
	if err := os.WriteFile(tmp, replaced, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, entry.Document); err != nil {
		t.Fatal(err)
	}
	view = mustView(t, idx)
	if got := idx.Stats(); got.DocumentReads != 1 || got.Parses != 1 {
		t.Fatalf("replace stats %+v", got)
	}
	if !titlePresent(view, "Replaced 0") {
		t.Fatal("atomic replace not visible")
	}
	requireMatchingPayload(t, root, view)
}

func TestSummaryCacheSameFingerprintWaitsForStrong(t *testing.T) {
	root, ids := summaryCacheBoard(t, 1)
	idx := mustIndex(t, root)
	mustView(t, idx)
	entry := locateCard(t, root, ids[0])
	info, err := os.Stat(entry.Document)
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(entry.Document)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(body, []byte("Summary 0"), []byte("Summarz 0"), 1)
	if len(edited) != len(body) {
		t.Fatal("edit changed size")
	}
	if err := os.WriteFile(entry.Document, edited, info.Mode().Perm()); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(entry.Document, info.ModTime(), info.ModTime()); err != nil {
		t.Fatal(err)
	}
	version, err := revision(root, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	before, err := documentFingerprint(entry, version)
	if err != nil {
		t.Fatal(err)
	}
	afterEntry := locateCard(t, root, ids[0])
	after, err := documentFingerprint(afterEntry, version)
	if err != nil {
		t.Fatal(err)
	}
	view := mustView(t, idx)
	if before == after {
		if got := idx.Stats(); got.DocumentReads != 0 || got.Parses != 0 || !got.Reused {
			t.Fatalf("incremental should miss preserved fingerprint: %+v", got)
		}
		if titlePresent(view, "Summarz 0") {
			t.Fatal("incremental published same-fingerprint edit")
		}
	}
	idx.lastStrong = idx.now().Add(-2 * idx.strongEvery)
	view = mustView(t, idx)
	if got := idx.Stats(); !got.Strong || got.DocumentReads != 1 {
		t.Fatalf("strong stats %+v", got)
	}
	if !titlePresent(view, "Summarz 0") {
		t.Fatal("strong pass missed preserved-fingerprint edit")
	}
	requireMatchingPayload(t, root, view)
}

func TestSummaryCachePendingAndIncompleteDoNotPublish(t *testing.T) {
	root, ids := summaryCacheBoard(t, 2)
	idx := mustIndex(t, root)
	first := mustView(t, idx)
	cached := len(idx.cards)

	s, err := ReadSnapshot(root, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	operation, err := operationID()
	if err != nil {
		t.Fatal(err)
	}
	before := s.Text
	record := OperationRecord{Schema: 1, ID: operation, Phase: "prepared", Revisions: map[string]uint64{ids[0]: s.Revision + 1},
		Files: []FileChange{{Path: filepath.Join(s.Entry.State, ids[0], "spec.md"), Before: &before, After: before + "\nnew text\n"}}}
	if err := writeOperation(root, control(root, "operations", operation+".json"), record, false); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.View(context.Background()); SnapshotReadStatus(err) != "recoverable" {
		t.Fatalf("pending: %v", err)
	}
	if len(idx.cards) != cached {
		t.Fatal("pending scan mutated cache")
	}
	if !reflect.DeepEqual(first.Tasks, idx.view.Tasks) {
		t.Fatal("pending published a new view")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := idx.View(ctx); err == nil {
		t.Fatal("canceled view succeeded")
	}
	if len(idx.cards) != cached {
		t.Fatal("canceled view treated as empty board")
	}

	entry := locateCard(t, root, ids[1])
	if err := os.WriteFile(entry.Document, []byte{0xff, 0xfe, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(control(root, "operations", operation+".json")); err != nil {
		t.Fatal(err)
	}
	if _, err := idx.View(context.Background()); err == nil {
		t.Fatal("invalid utf-8 succeeded")
	}
	if len(idx.cards) != cached {
		t.Fatal("failed read published a mixed generation")
	}
}

func TestSummaryCacheInvalidateCloseAndIsolation(t *testing.T) {
	root, ids := summaryCacheBoard(t, 2)
	idx := mustIndex(t, root)
	mustView(t, idx)
	idx.Invalidate(ids[0])
	mustView(t, idx)
	if got := idx.Stats(); got.DocumentReads != 1 || got.Parses != 1 {
		t.Fatalf("invalidate one %+v", got)
	}
	idx.Invalidate()
	mustView(t, idx)
	if got := idx.Stats(); got.DocumentReads != 2 || got.Parses != 2 {
		t.Fatalf("invalidate all %+v", got)
	}

	other, otherIDs := summaryCacheBoard(t, 1)
	second := mustIndex(t, other)
	view := mustView(t, second)
	if CanonicalRootMust(t, other) == idx.root {
		t.Fatal("distinct boards shared a cache key")
	}
	if len(view.Tasks) != 1 || view.Tasks[0].TaskID != otherIDs[0] {
		t.Fatalf("isolated view %+v", view.Tasks)
	}

	idx.Close()
	if _, err := idx.View(context.Background()); err == nil {
		t.Fatal("closed view succeeded")
	}
	idx.Invalidate(ids[0])
}

func CanonicalRootMust(t *testing.T, root string) string {
	t.Helper()
	got, err := CanonicalRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestSummaryCacheConcurrentViewInvalidate(t *testing.T) {
	root, ids := summaryCacheBoard(t, 4)
	idx := mustIndex(t, root)
	mustView(t, idx)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 12; j++ {
				if _, err := idx.View(context.Background()); err != nil {
					t.Errorf("view: %v", err)
					return
				}
				idx.Invalidate(ids[j%len(ids)])
			}
		}()
	}
	wg.Wait()
	requireMatchingPayload(t, root, mustView(t, idx))
}

func TestSummaryCacheInvalidateDoesNotWaitForScan(t *testing.T) {
	root, ids := summaryCacheBoard(t, 1)
	idx := mustIndex(t, root)
	mustView(t, idx)
	blocked, release := make(chan struct{}), make(chan struct{})
	idx.beforeScan = func() {
		idx.beforeScan = nil
		close(blocked)
		<-release
	}
	errc := make(chan error, 1)
	go func() {
		_, err := idx.View(context.Background())
		errc <- err
	}()
	<-blocked
	start := time.Now()
	idx.Invalidate(ids[0])
	if time.Since(start) > 200*time.Millisecond {
		t.Fatal("Invalidate waited for scan I/O")
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	if got := idx.Stats(); got.DocumentReads != 1 || got.Parses != 1 {
		t.Fatalf("stale scan published without retry: %+v", got)
	}
	requireMatchingPayload(t, root, mustView(t, idx))
}

func TestSummaryCacheConcurrentWritesMatchPayload(t *testing.T) {
	root, ids := summaryCacheBoard(t, 3)
	idx := mustIndex(t, root)
	mustView(t, idx)
	before, err := ReadSnapshot(root, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	blocked, release := make(chan struct{}), make(chan struct{})
	idx.beforeScan = func() {
		idx.beforeScan = nil
		close(blocked)
		<-release
	}
	errc := make(chan error, 1)
	go func() {
		_, viewErr := idx.View(context.Background())
		errc <- viewErr
	}()
	<-blocked
	if err := UpdateDocument(root, ids[0], UpdateOptions{Document: "spec.md", Text: before.Text + "\nnote\n", ExpectedRevision: before.Revision}); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-errc; err != nil {
		t.Fatal(err)
	}
	after, err := ReadSnapshot(root, ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision <= before.Revision {
		t.Fatalf("revision %d after concurrent write from %d", after.Revision, before.Revision)
	}
	requireMatchingPayload(t, root, mustView(t, idx))
}

func TestSummaryCacheSetStrongEvery(t *testing.T) {
	root, _ := summaryCacheBoard(t, 1)
	idx := mustIndex(t, root)
	mustView(t, idx)
	idx.SetStrongEvery(time.Hour)
	mustView(t, idx)
	if idx.Stats().Strong {
		t.Fatal("hour interval should not strong immediately")
	}
	idx.lastStrong = idx.now().Add(-time.Hour)
	idx.SetStrongEvery(time.Minute)
	mustView(t, idx)
	if got := idx.Stats(); !got.Strong || got.DocumentReads == 0 {
		t.Fatalf("updated interval did not strong: %+v", got)
	}
}

func TestSummaryCacheHotRefreshBudget(t *testing.T) {
	root, sample := privateSampleBoard(t)
	const rounds = 21
	if !sample {
		root, _ = summaryCacheBoard(t, 24)
	}
	payloadSamples := make([]time.Duration, 0, rounds)
	_, err := BoardPayload(root)
	if err != nil {
		if sample {
			t.Skipf("sample board is not readable as a payload: %v", err)
		}
		t.Fatal(err)
	}
	for i := 0; i < rounds; i++ {
		start := time.Now()
		if _, err := BoardPayload(root); err != nil {
			t.Fatal(err)
		}
		payloadSamples = append(payloadSamples, time.Since(start))
	}
	idx := mustIndex(t, root)
	firstStart := time.Now()
	first := mustView(t, idx)
	firstDur := time.Since(firstStart)
	firstStats := idx.Stats()
	if len(first.Tasks) == 0 {
		t.Fatal("empty board")
	}
	cacheSamples := make([]time.Duration, 0, rounds)
	for i := 0; i < rounds; i++ {
		start := time.Now()
		if _, err := idx.View(context.Background()); err != nil {
			t.Fatal(err)
		}
		cacheSamples = append(cacheSamples, time.Since(start))
		if got := idx.Stats(); got.DocumentReads != 0 || got.Parses != 0 || !got.Reused {
			t.Fatalf("hot cache work %+v", got)
		}
	}
	hotAllocs := measureHotAllocs(t, idx, rounds)
	idx.lastStrong = idx.now().Add(-2 * idx.strongEvery)
	strongStart := time.Now()
	mustView(t, idx)
	strongDur := time.Since(strongStart)
	strongStats := idx.Stats()
	payloadP50 := durationP50(payloadSamples)
	cacheP50 := durationP50(cacheSamples)
	firstRounds := measureViews(t, func() (*SummaryIndex, func()) {
		idx, err := NewSummaryIndex(root, time.Hour)
		if err != nil {
			t.Fatal(err)
		}
		return idx, idx.Close
	}, rounds)
	singleID := first.Tasks[0].TaskID
	sevenIDs := make([]string, 0, 7)
	for _, task := range first.Tasks {
		if len(sevenIDs) == 7 {
			break
		}
		sevenIDs = append(sevenIDs, task.TaskID)
	}
	singleRounds := measureEdits(t, idx, []string{singleID}, rounds)
	sevenRounds := measureEdits(t, idx, sevenIDs, rounds)
	strongRounds := make([]timedRound, 0, rounds)
	for i := 0; i < rounds; i++ {
		idx.lastStrong = idx.now().Add(-2 * idx.strongEvery)
		strongRounds = append(strongRounds, timeRound(func() SummaryStats {
			mustView(t, idx)
			return idx.Stats()
		}))
	}
	discardRounds := make([]timedRound, 0, rounds)
	for i := 0; i < rounds; i++ {
		discardRounds = append(discardRounds, timeRound(func() SummaryStats {
			idx.afterScan = func() {
				idx.afterScan = nil
				idx.Invalidate()
			}
			mustView(t, idx)
			return idx.Stats()
		}))
	}
	t.Logf("sample=%v cards=%d digest=%s", sample, firstStats.Cards, viewDigest(first))
	t.Logf("payload p50=%s p95=%s", payloadP50, durationP95(payloadSamples))
	t.Logf("hot p50=%s p95=%s alloc_p50=%d reads=0 parses=0", cacheP50, durationP95(cacheSamples), uintP50(hotAllocs))
	logScenario(t, "first", firstRounds)
	logScenario(t, "single", singleRounds)
	logScenario(t, "seven", sevenRounds)
	logScenario(t, "strong", strongRounds)
	logScenario(t, "discard", discardRounds)
	t.Logf("spot first=%s reads=%d parses=%d strong=%s reads=%d parses=%d reused=%v",
		firstDur, firstStats.DocumentReads, firstStats.Parses,
		strongDur, strongStats.DocumentReads, strongStats.Parses, strongStats.Reused)
	if sample && cacheP50 > payloadP50/5 {
		t.Fatalf("hot cache p50 %s exceeds 20%% of uncached p50 %s", cacheP50, payloadP50)
	}
	if !sample && cacheP50 > payloadP50 {
		t.Fatalf("synthetic hot cache p50 %s slower than payload p50 %s", cacheP50, payloadP50)
	}
}

type timedRound struct {
	d             time.Duration
	alloc         uint64
	reads, parses int
}

func timeRound(fn func() SummaryStats) timedRound {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	st := fn()
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	return timedRound{d: elapsed, alloc: after.TotalAlloc - before.TotalAlloc, reads: st.DocumentReads, parses: st.Parses}
}

func measureViews(t *testing.T, setup func() (*SummaryIndex, func()), n int) []timedRound {
	t.Helper()
	out := make([]timedRound, 0, n)
	for i := 0; i < n; i++ {
		idx, cleanup := setup()
		out = append(out, timeRound(func() SummaryStats {
			mustView(t, idx)
			return idx.Stats()
		}))
		cleanup()
	}
	return out
}

func measureEdits(t *testing.T, idx *SummaryIndex, ids []string, n int) []timedRound {
	t.Helper()
	out := make([]timedRound, 0, n)
	for i := 0; i < n; i++ {
		bumpDocuments(t, idx.root, ids)
		out = append(out, timeRound(func() SummaryStats {
			mustView(t, idx)
			return idx.Stats()
		}))
	}
	return out
}

func bumpDocuments(t *testing.T, root string, ids []string) {
	t.Helper()
	for _, id := range ids {
		entry := locateCard(t, root, id)
		info, err := os.Stat(entry.Document)
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(entry.Document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(entry.Document, append(data, '\n'), info.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
}

func logScenario(t *testing.T, name string, rounds []timedRound) {
	t.Helper()
	if len(rounds) == 0 {
		t.Fatalf("no rounds for %s", name)
	}
	ds := make([]time.Duration, len(rounds))
	allocs := make([]uint64, len(rounds))
	var reads, parses int
	for i, round := range rounds {
		ds[i] = round.d
		allocs[i] = round.alloc
		reads += round.reads
		parses += round.parses
	}
	t.Logf("%s n=%d p50=%s p95=%s alloc_p50=%d last_reads=%d last_parses=%d sum_reads=%d sum_parses=%d",
		name, len(rounds), durationP50(ds), durationP95(ds), uintP50(allocs),
		rounds[len(rounds)-1].reads, rounds[len(rounds)-1].parses, reads, parses)
}

func uintP50(samples []uint64) uint64 {
	sorted := append([]uint64(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}

func measureHotAllocs(t *testing.T, idx *SummaryIndex, n int) []uint64 {
	t.Helper()
	out := make([]uint64, 0, n)
	for i := 0; i < n; i++ {
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		mustView(t, idx)
		runtime.ReadMemStats(&after)
		out = append(out, after.TotalAlloc-before.TotalAlloc)
	}
	return out
}

func viewDigest(view BoardView) string {
	sum := sha256.New()
	for _, task := range view.Tasks {
		fmt.Fprintf(sum, "%s\t%s\t%s\t%s\t%s\n", task.TaskID, task.State, task.Kind, task.Title, task.Time)
	}
	return hex.EncodeToString(sum.Sum(nil))
}

func durationP50(samples []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[len(sorted)/2]
}

func durationP95(samples []time.Duration) time.Duration {
	sorted := append([]time.Duration(nil), samples...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	return sorted[(len(sorted)*95)/100]
}

func privateSampleBoard(t *testing.T) (string, bool) {
	t.Helper()
	src := os.Getenv("KANDER_SUMMARY_CACHE_SAMPLE")
	if src == "" {
		src = filepath.Join(os.Getenv("HOME"), "works", "quicktui-mono", "kanban")
	}
	info, err := os.Stat(src)
	if err != nil || !info.IsDir() {
		return "", false
	}
	dst := t.TempDir()
	for _, state := range States {
		if err := os.MkdirAll(filepath.Join(dst, state), 0o755); err != nil {
			t.Fatal(err)
		}
		from := filepath.Join(src, state)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		copyTree(t, from, filepath.Join(dst, state))
	}
	for _, part := range []string{"versions", "operations"} {
		from := filepath.Join(src, ".kander", part)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		copyTree(t, from, filepath.Join(dst, ".kander", part))
	}
	if err := ensureControl(dst); err != nil {
		t.Fatal(err)
	}
	return dst, true
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, info.Mode().Perm())
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		return joinClose(out, copyErr)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func joinClose(file *os.File, err error) error {
	if closeErr := file.Close(); err == nil {
		return closeErr
	}
	return err
}
