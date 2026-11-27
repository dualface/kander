package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/fs"
)

// gcProject is the server project of a registered name.
func gcProject(t *testing.T, f *gcFixture, name string) *project {
	t.Helper()
	info, err := f.s.projectByName(name)
	if err != nil {
		t.Fatal(err)
	}
	p, err := f.s.project(info.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// gcArchivedCard creates a card in todo and archives it.
func gcArchivedCard(t *testing.T, p *client.Project, slug, group string) string {
	t.Helper()
	task := gcTodoTask(t, p, slug, group)
	if _, err := p.Move(t.Context(), "", task.TaskID, api.Move{State: "archived", Result: "cancelled", Reason: "test", Decision: "user"}, nil); err != nil {
		t.Fatal(err)
	}
	return task.TaskID
}

// holdBoardLock holds the named lock of a board exclusively through another
// handle, like a board writer of another process, until release.
func holdBoardLock(t *testing.T, boardRoot, name string) (release func()) {
	t.Helper()
	file, err := fs.OpenLockFile(boardRoot, filepath.Join(boardRoot, ".kander", "locks", name))
	if err != nil {
		t.Fatal(err)
	}
	lock, err := fs.LockExclusive(file)
	if err != nil {
		file.Close()
		t.Fatal(err)
	}
	released := false
	release = func() {
		if !released {
			released = true
			lock.Unlock()
			file.Close()
		}
	}
	t.Cleanup(release)
	return release
}

// receiptErrors are the project errors of the receipt stage.
func receiptErrors(p api.ServerGCProject) []string {
	var out []string
	for _, e := range p.Errors {
		if strings.Contains(e, "idempotency receipts") {
			out = append(out, e)
		}
	}
	return out
}

// A board, card, or journal lock another process holds makes the receipt stage
// of that project skip at once with one board.lock_busy error; the next project
// is still handled.
func TestServerGCReceiptStageNeverWaitsForABoardLock(t *testing.T) {
	for _, held := range []string{"board", "card", "journal"} {
		t.Run(held, func(t *testing.T) {
			f := newGCFixture(t)
			a := f.project("a")
			f.project("b")
			stubCompaction(t, noCompaction)
			card := gcArchivedCard(t, a, "alpha", "")
			old := f.clock.now().UTC().Add(-48 * time.Hour)
			cardReceipt := writeReceipt(t, f.s, "a", receipt{Key: "move-alpha", Operation: "move:" + card, Digest: "d", Status: receiptDone, HTTP: 200, CreatedAt: old})
			freeReceipt := writeReceipt(t, f.s, "b", receipt{Key: "live-b", Operation: "observation:20261101-b-task", Digest: "d", Status: receiptDone, HTTP: 200, CreatedAt: old})
			pa, pb := gcProject(t, f, "a"), gcProject(t, f, "b")
			lock := map[string]string{"board": "board.lock", "card": card + ".lock", "journal": "journal.lock"}[held]
			release := holdBoardLock(t, pa.board, lock)
			report, err := f.gc(api.ServerGCRequest{OlderThan: "1h", LogKeep: 1 << 20})
			stillHeld := fileExists(receiptNamePath(pa.root, cardReceipt))
			release()
			if err != nil || len(report.Projects) != 2 {
				t.Fatalf("gc: %+v %v", report, err)
			}
			busy, free := report.Projects[0], report.Projects[1]
			if errs := receiptErrors(busy); len(errs) != 1 || !strings.Contains(errs[0], "board.lock_busy") || len(busy.Receipts.Kept) != 0 {
				t.Fatalf("the busy project must report one board.lock_busy error: %+v", busy)
			}
			if !stillHeld {
				t.Fatal("the receipt of the busy project must stay")
			}
			if len(free.Errors) != 0 || free.Receipts.Files != 1 || fileExists(receiptNamePath(pb.root, freeReceipt)) {
				t.Fatalf("the free project must be handled: %+v", free)
			}
		})
	}
}

// A recorded member whose eligibility the busy board cannot decide stays in the
// intent as recorded and is not reported as kept.
func TestServerGCKeepsAnIntentMemberAsRecordedWhileTheBoardIsBusy(t *testing.T) {
	f := newGCFixture(t)
	a := f.project("a")
	stubCompaction(t, noCompaction)
	card := gcArchivedCard(t, a, "alpha", "")
	pa := gcProject(t, f, "a")
	name := writeReceipt(t, f.s, "a", receipt{Key: "move-alpha", Operation: "move:" + card, Digest: "d", Status: receiptDone, HTTP: 200, CreatedAt: f.clock.now().UTC().Add(-2 * time.Hour)})
	data, err := os.ReadFile(receiptNamePath(pa.root, name))
	if err != nil {
		t.Fatal(err)
	}
	member := receiptIntentMember{Name: name, SHA256: contentHash(data), Bytes: int64(len(data)), OlderThan: "1h"}
	if err := writeReceiptIntent(pa.root, receiptIntent{Schema: 1, Receipts: []receiptIntentMember{member}}); err != nil {
		t.Fatal(err)
	}
	release := holdBoardLock(t, pa.board, "board.lock")
	report, err := f.gc(api.ServerGCRequest{OlderThan: "720h", LogKeep: 1 << 20})
	raw, readErr := os.ReadFile(receiptIntentPath(pa.root))
	release()
	if err != nil || len(report.Projects[0].Receipts.Kept) != 0 || len(receiptErrors(report.Projects[0])) != 1 {
		t.Fatalf("gc: %+v %v", report, err)
	}
	var left receiptIntent
	if readErr != nil || json.Unmarshal(raw, &left) != nil || len(left.Receipts) != 1 || left.Receipts[0] != member {
		t.Fatalf("the member must stay as recorded: %s %v", raw, readErr)
	}
}

// Cancelling a gc whose only project has a busy board ends it at once.
func TestServerGCStopsAtCancellationWhileTheBoardIsBusy(t *testing.T) {
	f := newGCFixture(t)
	a := f.project("a")
	card := gcArchivedCard(t, a, "alpha", "")
	writeReceipt(t, f.s, "a", receipt{Key: "move-alpha", Operation: "move:" + card, Digest: "d", Status: receiptDone, HTTP: 200, CreatedAt: f.clock.now().UTC().Add(-48 * time.Hour)})
	pa := gcProject(t, f, "a")
	holdBoardLock(t, pa.board, "board.lock")
	run := gcRun{data: f.s.data, version: "test", now: f.clock.now, targets: f.s.gcTargets("a")}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan api.ServerGCReport, 1)
	go func() { done <- run.run(ctx, gcOptions{project: "a", olderThan: time.Hour, logKeep: 1 << 20}) }()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the gc must return within 2 seconds while the board lock is held")
	}
}
