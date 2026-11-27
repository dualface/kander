package server

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
)

// intentMember records the receipt file name as it is now, with olderThan.
func intentMember(t *testing.T, root, name, olderThan string) receiptIntentMember {
	t.Helper()
	data, err := os.ReadFile(receiptNamePath(root, name))
	if err != nil {
		t.Fatal(err)
	}
	return receiptIntentMember{Name: name, SHA256: contentHash(data), Bytes: int64(len(data)), OlderThan: olderThan}
}

func readIntent(t *testing.T, root string) (receiptIntent, bool) {
	t.Helper()
	raw, err := os.ReadFile(receiptIntentPath(root))
	if errors.Is(err, os.ErrNotExist) {
		return receiptIntent{}, false
	}
	var intent receiptIntent
	if err != nil || json.Unmarshal(raw, &intent) != nil {
		t.Fatalf("receipt intent: %s %v", raw, err)
	}
	return intent, true
}

// A member whose file is already gone ends as settled: it adds no file and no
// byte, in a plan and in a run, and the run removes the intent.
func TestServerGCSettlesAnIntentMemberWhoseFileIsGone(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	stubCompaction(t, noCompaction)
	p := gcProject(t, f, "demo")
	name := writeReceipt(t, f.s, "demo", receipt{Key: "gone", Operation: "observation:20261101-a-task", Digest: "d", Status: receiptDone, CreatedAt: f.clock.now().UTC().Add(-48 * time.Hour)})
	member := intentMember(t, p.root, name, "1h")
	if err := os.Remove(receiptNamePath(p.root, name)); err != nil {
		t.Fatal(err)
	}
	if err := writeReceiptIntent(p.root, receiptIntent{Schema: 1, Receipts: []receiptIntentMember{member}}); err != nil {
		t.Fatal(err)
	}
	for _, dryRun := range []bool{true, false} {
		report, err := f.gc(api.ServerGCRequest{Project: "demo", DryRun: dryRun, OlderThan: "1h", LogKeep: 1 << 20})
		if got := report.Projects[0].Receipts; err != nil || got.Files != 0 || got.Bytes != 0 || got.Settled != 1 {
			t.Fatalf("dry run %v: %+v %v", dryRun, report.Projects[0], err)
		}
	}
	if _, found := readIntent(t, p.root); found {
		t.Fatal("the run must remove the intent")
	}
}

// A member ends only once the intent no longer holds it: when the intent
// cannot be rewritten, the run reports the error and counts nothing settled.
func TestServerGCCountsSettledMembersOnlyAfterTheIntentIsRewritten(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a directory the test user cannot write")
	}
	f := newGCFixture(t)
	f.project("demo")
	stubCompaction(t, noCompaction)
	p := gcProject(t, f, "demo")
	name := writeReceipt(t, f.s, "demo", receipt{Key: "gone", Operation: "observation:20261101-a-task", Digest: "d", Status: receiptDone, CreatedAt: f.clock.now().UTC().Add(-48 * time.Hour)})
	member := intentMember(t, p.root, name, "1h")
	if err := os.Remove(receiptNamePath(p.root, name)); err != nil {
		t.Fatal(err)
	}
	if err := writeReceiptIntent(p.root, receiptIntent{Schema: 1, Receipts: []receiptIntentMember{member}}); err != nil {
		t.Fatal(err)
	}
	control := filepath.Dir(receiptIntentPath(p.root))
	if err := os.Chmod(control, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(control, 0o700) })
	report, err := f.gc(api.ServerGCRequest{Project: "demo", OlderThan: "1h", LogKeep: 1 << 20})
	if got := report.Projects[0]; err != nil || got.Receipts.Settled != 0 || len(receiptErrors(got)) != 1 {
		t.Fatalf("a failed rewrite: %+v %v", got, err)
	}
	if intent, found := readIntent(t, p.root); !found || len(intent.Receipts) != 1 || intent.Receipts[0] != member {
		t.Fatalf("the member must stay recorded: %+v", intent)
	}
}

// An intent whose only member was replaced by a newer receipt is not an empty
// plan: the text command runs and ends it, and says how many members it ended.
func TestServerGCCommandEndsAnIntentWhoseOnlyMemberWasReplaced(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	stubCompaction(t, noCompaction)
	p := gcProject(t, f, "demo")
	name := writeReceipt(t, f.s, "demo", receipt{Key: "replaced", Operation: "observation:20261101-a-task", Digest: "d", Status: receiptDone, CreatedAt: f.clock.now().UTC().Add(-48 * time.Hour)})
	member := intentMember(t, p.root, name, "1h")
	if err := writeReceiptIntent(p.root, receiptIntent{Schema: 1, Receipts: []receiptIntentMember{member}}); err != nil {
		t.Fatal(err)
	}
	writeReceipt(t, f.s, "demo", receipt{Key: "replaced", Operation: "observation:20261101-a-task", Digest: "d", Status: receiptDone, CreatedAt: f.clock.now().UTC()})
	gcCommandEnv(t, f.http.URL, "")
	code, stdout, stderr := runGCCommand("--data", f.s.data, "--project", "demo", "--yes")
	if _, found := readIntent(t, p.root); code != 0 || found || !fileExists(receiptNamePath(p.root, name)) {
		t.Fatalf("the intent must end and the new receipt stay: exit %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, config.Text("gc.receipts_settled", 1)) {
		t.Fatalf("the text output must count the settled member:\n%s", stdout)
	}
}

// A member whose eligibility check fails stays recorded with its own retention
// and is reported as kept; once the check works, that retention decides.
func TestServerGCKeepsAnIntentMemberWhoseCheckFails(t *testing.T) {
	f := newGCFixture(t)
	a := f.project("demo")
	stubCompaction(t, noCompaction)
	p := gcProject(t, f, "demo")
	card := gcArchivedCard(t, a, "alpha", "")
	name := writeReceipt(t, f.s, "demo", receipt{Key: "move-alpha", Operation: "move:" + card, Digest: "d", Status: receiptDone, HTTP: 200, CreatedAt: f.clock.now().UTC().Add(-2 * time.Hour)})
	member := intentMember(t, p.root, name, "1h")
	if err := writeReceiptIntent(p.root, receiptIntent{Schema: 1, Receipts: []receiptIntentMember{member}}); err != nil {
		t.Fatal(err)
	}
	previous := gcScan
	t.Cleanup(func() { gcScan = previous })
	gcScan = func(string) (board.Board, error) { return board.Board{}, errors.New("injected scan failure") }
	gcCommandEnv(t, f.http.URL, "")
	code, stdout, stderr := runGCCommand("--data", f.s.data, "--project", "demo", "--older-than", "720h", "--yes", "--json")
	var report api.ServerGCReport
	if code != 1 || json.Unmarshal([]byte(stdout), &report) != nil {
		t.Fatalf("a failed check: exit %d\n%s\n%s", code, stdout, stderr)
	}
	kept := report.Projects[0].Receipts.Kept
	if len(kept) != 1 || kept[0].ID != strings.TrimSuffix(name, ".json") || !strings.Contains(kept[0].Reason, "injected scan failure") {
		t.Fatalf("the member must be kept with the reason: %+v", report.Projects[0])
	}
	if intent, found := readIntent(t, p.root); !found || len(intent.Receipts) != 1 || intent.Receipts[0] != member {
		t.Fatalf("the member must stay with its retention: %+v", intent)
	}

	gcScan = previous
	code, stdout, stderr = runGCCommand("--data", f.s.data, "--project", "demo", "--older-than", "720h", "--yes", "--json")
	report = api.ServerGCReport{}
	if code != 0 || json.Unmarshal([]byte(stdout), &report) != nil || report.Projects[0].Receipts.Files != 1 || fileExists(receiptNamePath(p.root, name)) {
		t.Fatalf("the recorded retention must decide: exit %d\n%s\n%s", code, stdout, stderr)
	}
}

func TestServerGCPlanWithOnlySettledMembersIsNotEmpty(t *testing.T) {
	plan := api.ServerGCReport{DryRun: true, Projects: []api.ServerGCProject{{Name: "demo", Receipts: api.ServerGCReceipts{Settled: 1}}}}
	if gcEmpty(plan) {
		t.Fatal("a plan with a settled member must not be empty")
	}
}

// receiptHolds runs the gc of one project with a lock that records, for each
// project-lock hold, how many receipt removals it attempted. fail, when set,
// may fail an attempt: it gets the hold index and the attempt index in it.
type receiptHolds struct {
	t     *testing.T
	f     *gcFixture
	holds []int
	fail  func(hold, attempt int) error
}

func (h *receiptHolds) run(ctx context.Context, olderThan time.Duration) api.ServerGCReport {
	h.t.Helper()
	h.holds = nil
	target := h.f.s.gcTargets("demo")[0]
	lock := target.lock
	target.lock = func() (func(), error) {
		unlock, err := lock()
		if err == nil {
			h.holds = append(h.holds, 0)
		}
		return unlock, err
	}
	previous := receiptRemoveStep
	defer func() { receiptRemoveStep = previous }()
	receiptRemoveStep = func(string) error {
		hold := len(h.holds) - 1
		attempt := h.holds[hold]
		h.holds[hold]++
		if h.fail != nil {
			return h.fail(hold, attempt)
		}
		return nil
	}
	run := gcRun{data: h.f.s.data, version: "test", now: h.f.clock.now, targets: []gcTarget{target}}
	return run.run(ctx, gcOptions{project: "demo", olderThan: olderThan, logKeep: 1 << 20})
}

func (h *receiptHolds) within(limit int) bool {
	for _, n := range h.holds {
		if n > limit {
			return false
		}
	}
	return true
}

func setReceiptBatch(t *testing.T, n int) {
	t.Helper()
	previous := gcReceiptBatch
	t.Cleanup(func() { gcReceiptBatch = previous })
	gcReceiptBatch = n
}

// dueReceipts writes n liveness receipts that are due, and returns their names.
func dueReceipts(t *testing.T, f *gcFixture, n int) []string {
	t.Helper()
	names := make([]string, n)
	for i := range names {
		names[i] = writeReceipt(t, f.s, "demo", receipt{Key: "due-" + strconv.Itoa(i), Operation: "observation:20261101-a-task", Digest: "d", Status: receiptDone, CreatedAt: f.clock.now().UTC().Add(-48 * time.Hour)})
	}
	return names
}

// recordIntent records names as one intent, each member with its own retention.
func recordIntent(t *testing.T, root string, names []string) []receiptIntentMember {
	t.Helper()
	members := make([]receiptIntentMember, len(names))
	for i, name := range names {
		members[i] = intentMember(t, root, name, strconv.Itoa(i+1)+"h0m0s")
	}
	if err := writeReceiptIntent(root, receiptIntent{Schema: 1, Receipts: members}); err != nil {
		t.Fatal(err)
	}
	return members
}

// Finishing an intent handles at most a batch of members per project-lock hold.
func TestServerGCFinishesAnIntentInShards(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	stubCompaction(t, noCompaction)
	setReceiptBatch(t, 2)
	p := gcProject(t, f, "demo")
	names := dueReceipts(t, f, 5)
	recordIntent(t, p.root, names)
	h := &receiptHolds{t: t, f: f}
	report := h.run(t.Context(), time.Hour)
	if report.Incomplete() || report.Projects[0].Receipts.Files != 5 || !h.within(2) || len(h.holds) < 3 {
		t.Fatalf("holds %v: %+v", h.holds, report.Projects[0])
	}
	for _, name := range names {
		if fileExists(receiptNamePath(p.root, name)) {
			t.Fatalf("%s must be removed", name)
		}
	}
	if _, found := readIntent(t, p.root); found {
		t.Fatal("the intent must end")
	}
}

// A cancellation between shards keeps the members not handled yet as recorded;
// the next gc ends them.
func TestServerGCKeepsTheRestOfAnIntentWhenCancelledBetweenShards(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	stubCompaction(t, noCompaction)
	setReceiptBatch(t, 2)
	p := gcProject(t, f, "demo")
	members := recordIntent(t, p.root, dueReceipts(t, f, 5))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h := &receiptHolds{t: t, f: f, fail: func(hold, attempt int) error {
		if hold == 0 && attempt == 1 {
			cancel()
		}
		return nil
	}}
	report := h.run(ctx, time.Hour)
	if report.Projects[0].Receipts.Files != 2 || len(h.holds) != 1 {
		t.Fatalf("holds %v: %+v", h.holds, report.Projects[0])
	}
	intent, found := readIntent(t, p.root)
	if !found || len(intent.Receipts) != 3 {
		t.Fatalf("the unhandled members must stay: %+v", intent)
	}
	for i, member := range intent.Receipts {
		if member != members[2+i] {
			t.Fatalf("member %d changed: %+v, recorded %+v", i, member, members[2+i])
		}
	}
	h.fail = nil
	report = h.run(t.Context(), time.Hour)
	if _, found := readIntent(t, p.root); found || report.Incomplete() || report.Projects[0].Receipts.Files != 3 {
		t.Fatalf("the next gc must end the rest: %+v", report.Projects[0])
	}
}

// Members whose removal failed stay recorded across batches, but no hold
// handles more than a batch: the scan handles only its own receipts, and the
// next gc finishes the members in shards.
func TestServerGCKeepsEveryHoldWithinTheBatchAfterFailedRemovals(t *testing.T) {
	f := newGCFixture(t)
	f.project("demo")
	stubCompaction(t, noCompaction)
	setReceiptBatch(t, 2)
	p := gcProject(t, f, "demo")
	dueReceipts(t, f, 6)
	failed := 0
	h := &receiptHolds{t: t, f: f, fail: func(hold, attempt int) error {
		if failed < 2 && attempt == 0 {
			failed++
			return errors.New("removal failed")
		}
		return nil
	}}
	report := h.run(t.Context(), time.Hour)
	if !h.within(2) || report.Projects[0].Receipts.Files != 4 || len(report.Projects[0].Receipts.Kept) != 2 {
		t.Fatalf("scan holds %v: %+v", h.holds, report.Projects[0])
	}
	if intent, found := readIntent(t, p.root); !found || len(intent.Receipts) != 2 {
		t.Fatalf("the failed members must stay recorded: %+v", intent)
	}
	h.fail = nil
	report = h.run(t.Context(), time.Hour)
	if _, found := readIntent(t, p.root); found || !h.within(2) || report.Projects[0].Receipts.Files != 2 || report.Incomplete() {
		t.Fatalf("finish holds %v: %+v", h.holds, report.Projects[0])
	}
}
