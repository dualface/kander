package server

import (
	"bytes"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

// A successful release receipt of a group whose members are all archived or in
// trash goes after the retention; every other group receipt, a failed release,
// a release of a group with an open member, and a release of a group no card
// names stay.
func TestServerGCExpiresTheReleaseReceiptsOfEndedGroups(t *testing.T) {
	f := newGCFixture(t)
	a := f.project("demo")
	stubCompaction(t, noCompaction)
	ctx := t.Context()
	ended, open := "20261101-ended-group", "20261101-open-group"
	gcArchivedCard(t, a, "ended-one", ended)
	trashed := gcTodoTask(t, a, "ended-two", ended)
	if _, err := a.Move(ctx, "", trashed.TaskID, api.Move{State: "trash", Result: "trashed", Reason: "test", Decision: "user"}, nil); err != nil {
		t.Fatal(err)
	}
	gcArchivedCard(t, a, "open-one", open)
	gcTodoTask(t, a, "open-two", open)
	old := f.clock.now().UTC().Add(-48 * time.Hour)
	write := func(key, operation string, status int) string {
		return writeReceipt(t, f.s, "demo", receipt{Key: key, Operation: operation, Digest: "d", Status: receiptDone, HTTP: status, CreatedAt: old})
	}
	due := write("release-ended", "group-release:"+ended, http.StatusOK)
	kept := map[string]string{
		write("claim-ended", "group-claim:"+ended, http.StatusOK):                 "a group claim",
		write("release-open", "group-release:"+open, http.StatusOK):               "a group with an open member",
		write("release-failed", "group-release:"+ended, http.StatusConflict):      "a failed release",
		write("release-nobody", "group-release:20261101-no-group", http.StatusOK): "a group no card names",
	}
	p := gcProject(t, f, "demo")
	report, err := f.gc(api.ServerGCRequest{OlderThan: "1h", LogKeep: 1 << 20})
	if err != nil || report.Incomplete() || report.Projects[0].Receipts.Files != 1 {
		t.Fatalf("gc: %+v %v", report, err)
	}
	if fileExists(receiptNamePath(p.root, due)) {
		t.Fatal("the release receipt of an ended group must go")
	}
	for name, why := range kept {
		if !fileExists(receiptNamePath(p.root, name)) {
			t.Fatalf("the receipt of %s must stay", why)
		}
	}
}

// A board scan that may hide a member of the group cannot tell that the group
// ended, so a release receipt stays: a card whose text cannot be read, and a
// member card directory whose spec.md is gone, which the scan reports as a
// problem instead of an entry.
func TestGroupReleaseDueNeedsACompleteScan(t *testing.T) {
	for name, damage := range map[string]func(b board.Board, member, other string) error{
		"unreadable text": func(b board.Board, _, other string) error {
			return os.WriteFile(b.Entries[other].Document, []byte{0xff, 0xfe}, 0o600)
		},
		"missing spec.md": func(b board.Board, member, _ string) error {
			return os.Remove(b.Entries[member].Document)
		},
	} {
		t.Run(name, func(t *testing.T) {
			f := newGCFixture(t)
			a := f.project("demo")
			group := "20261101-ended-group"
			gcArchivedCard(t, a, "ended-one", group)
			member := gcArchivedCard(t, a, "ended-two", group)
			other := gcArchivedCard(t, a, "unrelated", "")
			p := gcProject(t, f, "demo")
			r := receipt{Operation: "group-release:" + group, Status: receiptDone, HTTP: http.StatusOK, CreatedAt: f.clock.now().UTC().Add(-48 * time.Hour)}
			if due, err := receiptDue(r, f.clock.now(), time.Hour, &bindingFacts{board: p.board}); err != nil || !due {
				t.Fatalf("complete board: %v %v", due, err)
			}
			b, err := board.ScanNoWait(p.board)
			if err != nil {
				t.Fatal(err)
			}
			if err := damage(b, member, other); err != nil {
				t.Fatal(err)
			}
			if due, err := receiptDue(r, f.clock.now(), time.Hour, &bindingFacts{board: p.board}); err != nil || due {
				t.Fatalf("the receipt must stay: %v %v", due, err)
			}
		})
	}
}

// A replay of a release whose receipt a gc removed is refused and leaves the
// group lease record as it was.
func TestServerGCReleaseReceiptReplayCannotChangeTheLease(t *testing.T) {
	f := newGCFixture(t)
	a := f.project("demo")
	stubCompaction(t, noCompaction)
	ctx := t.Context()
	group := "20261101-ended-group"
	one := gcTodoTask(t, a, "coordinator-one", group)
	two := gcTodoTask(t, a, "coordinator-two", group)
	claim := api.GroupClaim{BranchMode: "main-develop", ExecutionID: "coordinator", Request: board.CoordinatorClaim{GroupID: group, Owner: "codex", Token: "public-session", Basis: "gc fixture", Members: []string{one.TaskID, two.TaskID}}}
	grant, err := a.ClaimGroup(ctx, "", group, claim, nil)
	if err != nil {
		t.Fatal(err)
	}
	proof := client.Claim{Token: grant.Token, Generation: grant.Claim.Generation}
	release := api.GroupRelease{GroupID: group, Reason: "done", Decision: "user:release"}
	if _, err := a.ReleaseGroup(ctx, "release-key", group, release, proof); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{one.TaskID, two.TaskID} {
		if _, err := a.Move(ctx, "", id, api.Move{State: "archived", Result: "cancelled", Reason: "test", Decision: "user"}, nil); err != nil {
			t.Fatal(err)
		}
	}
	p := gcProject(t, f, "demo")
	releaseReceipt := ""
	for name, value := range receiptFiles(t, f.s, "demo") {
		if value == "done group-release:"+group {
			releaseReceipt = name
		}
	}
	if releaseReceipt == "" {
		t.Fatal("the release wrote no receipt")
	}
	record := filepath.Join(p.root, servicePath(groupClaimName(group)))
	before, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	f.clock.advance(2 * time.Hour)
	if report, err := f.gc(api.ServerGCRequest{Project: "demo", OlderThan: "1h", LogKeep: 1 << 20}); err != nil || fileExists(receiptNamePath(p.root, releaseReceipt)) {
		t.Fatalf("the release receipt must go: %+v %v", report, err)
	}
	_, err = a.ReleaseGroup(ctx, "release-key", group, release, proof)
	var refused *api.Error
	if !errors.As(err, &refused) || refused.Status < 300 || refused.Status >= 500 {
		t.Fatalf("the replayed release must be refused with a non-2xx answer below 500: %v", err)
	}
	after, err := os.ReadFile(record)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("the lease record changed:\n%s\n%s", before, after)
	}
	if strings.TrimSpace(string(after)) == "" {
		t.Fatal("the lease record is empty")
	}
}
