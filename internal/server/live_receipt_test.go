package server

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

// liveFixture is a project with one claimed card and one claimed group.
type liveFixture struct {
	*gcFixture
	p          *client.Project
	proj       *project
	task       string
	taskGrant  api.ClaimGrant
	taskProof  client.Claim
	group      string
	members    []string
	groupGrant api.GroupGrant
	groupProof client.Claim
}

func newLiveFixture(t *testing.T) *liveFixture {
	t.Helper()
	f := &liveFixture{gcFixture: newGCFixture(t), group: "20261101-live-group"}
	f.p = f.project("demo")
	info, _ := f.s.projectByName("demo")
	f.proj, _ = f.s.project(info.ID)
	ctx := t.Context()
	f.task = gcTodoTask(t, f.p, "alpha", "").TaskID
	var err error
	if f.taskGrant, err = f.p.Claim(ctx, "", f.task, api.ClaimRequest{ClientID: "gc-host", ExecutionID: "exec-a", Owner: "claude"}); err != nil {
		t.Fatal(err)
	}
	f.taskProof = client.Claim{Token: f.taskGrant.Token, Generation: f.taskGrant.Claim.Generation}
	for _, slug := range []string{"one", "two"} {
		f.members = append(f.members, gcTodoTask(t, f.p, slug, f.group).TaskID)
	}
	claim := api.GroupClaim{ExecutionID: "coordinator-a", Request: board.CoordinatorClaim{GroupID: f.group, Owner: "codex", Token: "public-session", Basis: "confirmed group fixture", Members: f.members}}
	if f.groupGrant, err = f.p.ClaimGroup(ctx, "", f.group, claim, nil); err != nil {
		t.Fatal(err)
	}
	f.groupProof = client.Claim{Token: f.groupGrant.Token, Generation: f.groupGrant.Claim.Generation}
	return f
}

// livenessReceipts lists the receipt files, at the top and in the slots,
// whose operation is operation.
func (f *liveFixture) livenessReceipts(t *testing.T, operation string) []string {
	t.Helper()
	var names []string
	root := filepath.Join(f.proj.root, servicePath("receipts"))
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var r receipt
		if err := json.Unmarshal(data, &r); err != nil {
			return err
		}
		if r.Operation == operation {
			rel, _ := filepath.Rel(root, path)
			names = append(names, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return names
}

func (f *liveFixture) claim(t *testing.T) claimRecord {
	t.Helper()
	r, found, err := f.proj.readClaim(f.task)
	if err != nil || !found {
		t.Fatalf("claim of %s: %v", f.task, err)
	}
	return r
}

func (f *liveFixture) groupClaim(t *testing.T) claimRecord {
	t.Helper()
	r, found, err := f.proj.readGroupClaim(f.group)
	if err != nil || !found {
		t.Fatalf("group claim of %s: %v", f.group, err)
	}
	return r
}

// sameLease reports whether two claim records agree on everything an
// observation must never move: holder, generation, lease and status.
func sameLease(a, b claimRecord) bool {
	a.Observation, b.Observation = nil, nil
	return reflect.DeepEqual(a, b)
}

func alive(reason string) api.AgentObservation {
	return api.AgentObservation{Status: "alive", Reason: reason}
}

func TestLivenessObservationsKeepOneReceiptPerExecution(t *testing.T) {
	f := newLiveFixture(t)
	ctx := t.Context()
	for i := range 100 {
		round := strconv.Itoa(i)
		if _, err := f.p.ObserveAgent(ctx, "observe-"+round, f.task, alive("round "+round), f.taskProof); err != nil {
			t.Fatal(err)
		}
		if _, err := f.p.ObserveGroup(ctx, "group-observe-"+round, f.group, alive("round "+round), f.groupProof); err != nil {
			t.Fatal(err)
		}
	}
	for _, operation := range []string{"observation:" + f.task, "group-observation:" + f.group} {
		names := f.livenessReceipts(t, operation)
		if len(names) != 1 || !strings.HasPrefix(names[0], liveReceiptDir+"/") {
			t.Fatalf("%s: 100 observations of one execution left %v, want one slot", operation, names)
		}
	}
	// A successor execution has a slot of its own; the earlier one stays until a gc.
	f.clock.advance(91 * time.Second)
	successor, err := f.p.Takeover(ctx, "", f.task, api.ClaimRequest{ClientID: "gc-host", ExecutionID: "exec-b", Owner: "codex", Decision: "explicit takeover", ExpectedGeneration: f.taskGrant.Claim.Generation})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.ObserveAgent(ctx, "successor-observe", f.task, alive("successor"), client.Claim{Token: successor.Token, Generation: successor.Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	if names := f.livenessReceipts(t, "observation:"+f.task); len(names) != 2 {
		t.Fatalf("two executions must keep one slot each: %v", names)
	}
	// Other operations still keep one receipt per key at the top.
	if names := f.livenessReceipts(t, "takeover:"+f.task); len(names) != 1 || strings.Contains(names[0], "/") {
		t.Fatalf("a takeover receipt must stay where it was: %v", names)
	}
}

func TestLivenessReplayOfTheLatestKeyReturnsTheFirstResponse(t *testing.T) {
	f := newLiveFixture(t)
	ctx := t.Context()
	first, err := f.p.ObserveAgent(ctx, "latest", f.task, alive("latest"), f.taskProof)
	if err != nil {
		t.Fatal(err)
	}
	firstGroup, err := f.p.ObserveGroup(ctx, "group-latest", f.group, alive("latest"), f.groupProof)
	if err != nil {
		t.Fatal(err)
	}
	// A second run would stamp a later observation time.
	f.clock.advance(time.Second)
	before := dataFiles(t, f.proj.board)
	again, err := f.p.ObserveAgent(ctx, "latest", f.task, alive("latest"), f.taskProof)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("replay = %+v %v, want the first response %+v", again, err, first)
	}
	againGroup, err := f.p.ObserveGroup(ctx, "group-latest", f.group, alive("latest"), f.groupProof)
	if err != nil || !reflect.DeepEqual(firstGroup, againGroup) {
		t.Fatalf("group replay = %+v %v, want the first response %+v", againGroup, err, firstGroup)
	}
	if after := dataFiles(t, f.proj.board); !mapsEqual(before, after) {
		t.Fatal("a replay of the latest key must not change the board")
	}
	// The same key with another body is still a mismatch.
	if _, err := f.p.ObserveAgent(ctx, "latest", f.task, alive("changed"), f.taskProof); !api.IsCode(err, api.CodeIdempotencyMismatch) {
		t.Fatalf("a reused key with another body: %v", err)
	}
}

func TestLivenessReplayOfAnOlderKeyNeverMovesTheClaimBack(t *testing.T) {
	f := newLiveFixture(t)
	ctx := t.Context()
	for _, key := range []string{"old", "new"} {
		if _, err := f.p.ObserveAgent(ctx, key, f.task, alive(key), f.taskProof); err != nil {
			t.Fatal(err)
		}
	}
	f.clock.advance(30 * time.Second)
	if _, err := f.p.Renew(ctx, f.task, f.taskProof); err != nil {
		t.Fatal(err)
	}
	renewed := f.claim(t)
	_, err := f.p.ObserveAgent(ctx, "old", f.task, alive("old"), f.taskProof)
	notServerError(t, "older key", err)
	replayed := f.claim(t)
	if !sameLease(renewed, replayed) {
		t.Fatalf("an older key moved the claim: %+v, want %+v", replayed, renewed)
	}
	if replayed.Observation == nil || replayed.Observation.Reason != "old" {
		t.Fatalf("the older key runs as a new observation: %+v", replayed.Observation)
	}
	if _, err := f.p.ObserveAgent(ctx, "next", f.task, alive("next"), f.taskProof); err != nil {
		t.Fatal(err)
	}
	if next := f.claim(t); next.Observation == nil || next.Observation.Reason != "next" || !sameLease(renewed, next) {
		t.Fatalf("the next observation must restore the latest values: %+v", next)
	}

	// After a takeover the older key of the earlier execution is refused and
	// leaves the successor's claim as it was.
	f.clock.advance(91 * time.Second)
	if _, err := f.p.Takeover(ctx, "", f.task, api.ClaimRequest{ClientID: "gc-host", ExecutionID: "exec-b", Owner: "codex", Decision: "explicit takeover", ExpectedGeneration: f.taskGrant.Claim.Generation}); err != nil {
		t.Fatal(err)
	}
	successor := f.claim(t)
	_, err = f.p.ObserveAgent(ctx, "old", f.task, alive("old"), f.taskProof)
	notServerError(t, "older key after a takeover", err)
	if err == nil {
		t.Fatal("the earlier execution's observation must be refused after a takeover")
	}
	if after := f.claim(t); !reflect.DeepEqual(successor, after) || after.Generation != f.taskGrant.Claim.Generation+1 || after.ExecutionID != "exec-b" {
		t.Fatalf("a refused older key changed the successor's claim: %+v, want %+v", after, successor)
	}
}

func TestGroupLivenessReplayOfAnOlderKeyNeverMovesTheLeaseBack(t *testing.T) {
	f := newLiveFixture(t)
	ctx := t.Context()
	for _, key := range []string{"old", "new"} {
		if _, err := f.p.ObserveGroup(ctx, "group-"+key, f.group, alive(key), f.groupProof); err != nil {
			t.Fatal(err)
		}
	}
	f.clock.advance(30 * time.Second)
	if _, err := f.p.RenewGroup(ctx, f.group, f.groupProof); err != nil {
		t.Fatal(err)
	}
	renewed := f.groupClaim(t)
	_, err := f.p.ObserveGroup(ctx, "group-old", f.group, alive("old"), f.groupProof)
	notServerError(t, "older group key", err)
	if replayed := f.groupClaim(t); !sameLease(renewed, replayed) {
		t.Fatalf("an older key moved the group lease: %+v, want %+v", replayed, renewed)
	}
	if _, err := f.p.ObserveGroup(ctx, "group-next", f.group, alive("next"), f.groupProof); err != nil {
		t.Fatal(err)
	}
	if next := f.groupClaim(t); next.Observation == nil || next.Observation.Reason != "next" || !sameLease(renewed, next) {
		t.Fatalf("the next observation must restore the latest values: %+v", next)
	}

	// A successor coordinator: the earlier execution's older key is refused.
	f.clock.advance(91 * time.Second)
	c := f.groupGrant.Checkpoint
	claim := api.GroupClaim{ExecutionID: "coordinator-b", ExpectedGeneration: f.groupGrant.Claim.Generation, Decision: "explicit coordinator handoff after lease loss", Request: board.CoordinatorClaim{GroupID: f.group, ExpectedRevision: c.Revision, ExpectedEpoch: c.Authority.Epoch, Owner: "codex", Token: "successor-session", Basis: "confirmed group fixture", Members: f.members}}
	if _, err := f.p.ClaimGroup(ctx, "group-takeover", f.group, claim, nil); err != nil {
		t.Fatal(err)
	}
	successor := f.groupClaim(t)
	_, err = f.p.ObserveGroup(ctx, "group-old", f.group, alive("old"), f.groupProof)
	notServerError(t, "older group key after a takeover", err)
	if !api.IsCode(err, api.CodeTakenOver) {
		t.Fatalf("the earlier coordinator's observation after a takeover: %v", err)
	}
	if after := f.groupClaim(t); !reflect.DeepEqual(successor, after) || after.ExecutionID != "coordinator-b" {
		t.Fatalf("a refused older key changed the successor's lease: %+v, want %+v", after, successor)
	}
}

// The server gc removes a slot like any liveness receipt: 24 hours after its
// latest observation. A slot the gc recorded but could not remove is finished
// by the next gc.
func TestServerGCExpiresLivenessSlots(t *testing.T) {
	f := newLiveFixture(t)
	stubCompaction(t, noCompaction)
	ctx := t.Context()
	if _, err := f.p.ObserveAgent(ctx, "observe", f.task, alive("x"), f.taskProof); err != nil {
		t.Fatal(err)
	}
	if _, err := f.p.ObserveGroup(ctx, "group-observe", f.group, alive("x"), f.groupProof); err != nil {
		t.Fatal(err)
	}
	slots := append(f.livenessReceipts(t, "observation:"+f.task), f.livenessReceipts(t, "group-observation:"+f.group)...)
	if len(slots) != 2 {
		t.Fatalf("slots = %v", slots)
	}
	request := api.ServerGCRequest{Project: "demo", OlderThan: "168h", LogKeep: 1 << 20}

	f.clock.advance(23 * time.Hour)
	report, err := f.gc(request)
	if err != nil || report.Projects[0].Receipts.Files != 0 || report.Incomplete() || len(report.Projects[0].Errors) != 0 {
		t.Fatalf("23h: %+v %v", report.Projects[0], err)
	}

	f.clock.advance(2 * time.Hour)
	previous := receiptRemoveStep
	t.Cleanup(func() { receiptRemoveStep = previous })
	receiptRemoveStep = func(name string) error {
		if name == slots[0] {
			return errors.New("removal failed")
		}
		return nil
	}
	report, err = f.gc(request)
	if err != nil || report.Projects[0].Receipts.Files != 1 || len(report.Projects[0].Receipts.Kept) != 1 || report.Projects[0].Receipts.Kept[0].ID != strings.TrimSuffix(slots[0], ".json") {
		t.Fatalf("25h with a failed removal: %+v %v", report.Projects[0], err)
	}
	if !fileExists(receiptIntentPath(f.proj.root)) {
		t.Fatal("the slot that could not be removed must stay recorded")
	}
	receiptRemoveStep = previous
	report, err = f.gc(request)
	if err != nil || report.Projects[0].Receipts.Files != 1 || report.Incomplete() {
		t.Fatalf("the next gc must finish the recorded slot: %+v %v", report.Projects[0], err)
	}
	if left := append(f.livenessReceipts(t, "observation:"+f.task), f.livenessReceipts(t, "group-observation:"+f.group)...); len(left) != 0 || fileExists(receiptIntentPath(f.proj.root)) {
		t.Fatalf("slots left: %v", left)
	}
	_, err = f.p.ObserveAgent(ctx, "observe", f.task, alive("x"), f.taskProof)
	notServerError(t, "replay after the slot expired", err)
}

// slotOf is the slot file of the latest observation of operation.
func (f *liveFixture) slotOf(t *testing.T, operation string) string {
	t.Helper()
	names := f.livenessReceipts(t, operation)
	if len(names) != 1 || !strings.HasPrefix(names[0], liveReceiptDir+"/") {
		t.Fatalf("%s: slots %v", operation, names)
	}
	return filepath.Join(f.proj.root, servicePath("receipts", filepath.FromSlash(names[0])))
}

// An observation whose board transaction stopped after it was prepared leaves
// its pending receipt in the slot for the recovery. A later observation of the
// same execution with another key must not write its own pending receipt over
// it: the recovery would find neither its before nor its after image, and the
// server would not start.
func TestLivenessSlotWaitsForThePreparedTransactionThatStagesIt(t *testing.T) {
	f := newLiveFixture(t)
	ctx := t.Context()
	first, err := f.p.ObserveAgent(ctx, "observe-a", f.task, alive("a"), f.taskProof)
	if err != nil {
		t.Fatal(err)
	}
	slot := f.slotOf(t, "observation:"+f.task)
	after, err := os.ReadFile(slot)
	if err != nil {
		t.Fatal(err)
	}
	// Turn the committed observation back into the state a failed apply
	// leaves: the slot holds A's pending receipt and a prepared record stages
	// A's done receipt over it.
	var done receipt
	if err := json.Unmarshal(after, &done); err != nil {
		t.Fatal(err)
	}
	pending := done
	pending.Status, pending.HTTP, pending.Body = receiptPending, 0, nil
	before, _ := json.MarshalIndent(pending, "", "  ")
	beforeText := string(before) + "\n"
	if err := os.WriteFile(slot, []byte(beforeText), 0o600); err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(f.proj.board, slot)
	record := board.OperationRecord{Schema: 1, ID: "0123456789abcdef0123456789abcdef", Phase: "prepared", Revisions: map[string]uint64{}, Groups: []string{serviceGroup},
		Files: []board.FileChange{{Path: rel, Before: &beforeText, After: string(after)}}}
	data, _ := json.MarshalIndent(record, "", "  ")
	pendingDir := filepath.Join(f.proj.board, ".kander", "operations", "pending")
	if err := os.MkdirAll(pendingDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pendingDir, record.ID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}

	// B of the same execution waits for the recovery and leaves the slot alone.
	if _, err := f.p.ObserveAgent(ctx, "observe-b", f.task, alive("b"), f.taskProof); !api.IsCode(err, api.CodeNotReady) {
		t.Fatalf("an observation while the slot waits for recovery: %v", err)
	}
	if kept, err := os.ReadFile(slot); err != nil || string(kept) != beforeText {
		t.Fatalf("the slot must keep the pending receipt the recovery expects: %v", err)
	}

	// A restart recovers A, and A's key replays its answer.
	f.http.Close()
	if err := f.s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err := Open(Options{DataDir: f.s.data, Now: f.clock.now, Version: "test", Logger: f.s.log})
	if err != nil {
		t.Fatalf("the restart must recover the prepared observation: %v", err)
	}
	h := httptest.NewServer(s.Handler())
	t.Cleanup(func() { h.Close(); s.Close() })
	c, err := client.New(h.URL, "gc-host")
	if err != nil {
		t.Fatal(err)
	}
	p := c.Project(f.proj.info.ID)
	if recovered, err := os.ReadFile(slot); err != nil || string(recovered) != string(after) {
		t.Fatalf("the recovery must leave A's done receipt in the slot: %v", err)
	}
	again, err := p.ObserveAgent(ctx, "observe-a", f.task, alive("a"), f.taskProof)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("A's key after the recovery = %+v %v, want %+v", again, err, first)
	}
	if _, err := p.ObserveAgent(ctx, "observe-b", f.task, alive("b"), f.taskProof); err != nil {
		t.Fatalf("B after the recovery: %v", err)
	}
}

// legacyReceipt moves the slot receipt of operation to the place a server
// before the slots kept the receipt of key.
func (f *liveFixture) legacyReceipt(t *testing.T, operation, key string) {
	t.Helper()
	slot := f.slotOf(t, operation)
	if err := os.Rename(slot, filepath.Join(f.proj.root, legacyReceiptPath(key))); err != nil {
		t.Fatal(err)
	}
}

// After an upgrade, a retry of a key an older server answered replays that
// answer from the receipt it left at the top. The receipt stays where it is and
// no slot is written.
func TestLivenessReplaysTheReceiptOfAServerBeforeTheSlots(t *testing.T) {
	f := newLiveFixture(t)
	ctx := t.Context()
	first, err := f.p.ObserveAgent(ctx, "legacy", f.task, alive("legacy"), f.taskProof)
	if err != nil {
		t.Fatal(err)
	}
	f.legacyReceipt(t, "observation:"+f.task, "legacy")
	stopped := api.AgentObservation{Status: "stopped", Reason: "coordinator ended"}
	firstGroup, err := f.p.ObserveGroup(ctx, "group-legacy", f.group, stopped, f.groupProof)
	if err != nil {
		t.Fatal(err)
	}
	f.legacyReceipt(t, "group-observation:"+f.group, "group-legacy")
	f.clock.advance(time.Second)
	before := dataFiles(t, f.proj.board)

	again, err := f.p.ObserveAgent(ctx, "legacy", f.task, alive("legacy"), f.taskProof)
	if err != nil || !reflect.DeepEqual(first, again) {
		t.Fatalf("replay of an older server's receipt = %+v %v, want %+v", again, err, first)
	}
	// The lease of that generation has ended; the answer is still the one sent.
	againGroup, err := f.p.ObserveGroup(ctx, "group-legacy", f.group, stopped, f.groupProof)
	if err != nil || !reflect.DeepEqual(firstGroup, againGroup) {
		t.Fatalf("replay of an older server's stopped answer = %+v %v, want %+v", againGroup, err, firstGroup)
	}
	if after := dataFiles(t, f.proj.board); !mapsEqual(before, after) {
		t.Fatal("a replay of an older server's receipt must not change the board")
	}
	for _, operation := range []string{"observation:" + f.task, "group-observation:" + f.group} {
		if names := f.livenessReceipts(t, operation); len(names) != 1 || strings.Contains(names[0], "/") {
			t.Fatalf("%s: the older receipt must stay at the top, alone: %v", operation, names)
		}
	}
	// The same key with another body is not that request.
	_, err = f.p.ObserveAgent(ctx, "legacy", f.task, alive("changed"), f.taskProof)
	notServerError(t, "an older server's key with another body", err)
}
