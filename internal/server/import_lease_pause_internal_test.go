package server

import (
	"bytes"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

// pauseHeld is longer than the default TTL the rig's server uses.
const pauseHeld = api.DefaultTTL + time.Minute

// pauseRig is an imported project whose working card has an active claim of
// host-a, on a server with a controlled clock.
type pauseRig struct {
	t      *testing.T
	dir    string
	now    *atomic.Int64
	s      *Server
	h      *httptest.Server
	p      *project
	task   string
	grant  api.ClaimGrant
	remote *client.Project
}

func newPauseRig(t *testing.T) *pauseRig {
	t.Helper()
	r := &pauseRig{t: t, dir: t.TempDir(), now: &atomic.Int64{}}
	r.now.Store(time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC).UnixNano())
	r.open()
	target := v1Board(t)
	var initial api.ImportResult
	r.p, initial = importProject(t, r.s, target)
	r.task = workingImportTask(t, target)
	c, err := client.New(r.h.URL, "host-a")
	if err != nil {
		t.Fatal(err)
	}
	r.remote = c.Project(initial.Project.ID)
	r.grant, err = r.remote.Takeover(t.Context(), "", r.task, api.ClaimRequest{ClientID: "host-a", ExecutionID: "exec-a", Owner: "codex", Decision: "user:test"})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func (r *pauseRig) clock() time.Time { return time.Unix(0, r.now.Load()) }

// open starts a server on the rig's data directory; the test closes it.
func (r *pauseRig) open() {
	r.t.Helper()
	r.s, r.h = openTest(r.t, r.dir, r.clock)
	s, h := r.s, r.h
	r.t.Cleanup(func() {
		h.Close()
		s.Close()
	})
}

// restart closes the server and opens it again on the same data directory.
func (r *pauseRig) restart() {
	r.t.Helper()
	r.h.Close()
	if err := r.s.Close(); err != nil {
		r.t.Fatal(err)
	}
	r.open()
	c, err := client.New(r.h.URL, "host-a")
	if err != nil {
		r.t.Fatal(err)
	}
	r.remote = c.Project(r.remote.ID())
	if r.p, err = r.s.project(r.remote.ID()); err != nil {
		r.t.Fatal(err)
	}
}

// holdAt advances the clock by pauseHeld when the import reaches step, which
// runs while it holds the project lock, and then returns result.
func (r *pauseRig) holdAt(step string, result error) {
	fired := false
	r.s.importFault = func(current string) error {
		if current != step || fired {
			return nil
		}
		fired = true
		r.now.Add(int64(pauseHeld))
		return result
	}
}

func (r *pauseRig) claimFile() []byte {
	r.t.Helper()
	data, err := os.ReadFile(filepath.Join(r.p.root, claimPath(r.task)))
	if err != nil {
		r.t.Fatal(err)
	}
	return data
}

func (r *pauseRig) assertPaused() {
	r.t.Helper()
	task, err := r.remote.Show(r.t.Context(), r.task)
	if err != nil || task.Claim == nil {
		r.t.Fatalf("claim after import: %+v %v", task.Claim, err)
	}
	if want := r.grant.Claim.ExpiresAt.Add(pauseHeld); task.Claim.Status != api.ClaimActive || !task.Claim.ExpiresAt.Equal(want) {
		r.t.Fatalf("claim after import: %s until %v, want active until %v", task.Claim.Status, task.Claim.ExpiresAt, want)
	}
}

func TestImportRoutesPauseActiveLeases(t *testing.T) {
	t.Run("loopback", func(t *testing.T) {
		r := newPauseRig(t)
		source, _ := newImportFixture(t, "loopback-incoming")
		r.holdAt("prepared", nil)
		if result, err := onlineImport(t, r.s, r.h.URL, api.ImportRequest{Project: "demo", From: source}); err != nil || result.AddedTasks != 1 {
			t.Fatalf("loopback import: %+v %v", result, err)
		}
		r.assertPaused()
	})
	t.Run("upload", func(t *testing.T) {
		r := newPauseRig(t)
		source, _ := newImportFixture(t, "upload-incoming")
		r.holdAt("prepared", nil)
		if result, err := uploadTestBoard(t, r.h.URL, api.ImportRequest{Project: "demo", From: source}); err != nil || result.AddedTasks != 1 {
			t.Fatalf("upload import: %+v %v", result, err)
		}
		r.assertPaused()
	})
}

// A publication that fails and cannot be recovered in process leaves the
// project closed, and no lease file is written, although the published board
// holds the active claim.
func TestImportBrokenWritesNoLease(t *testing.T) {
	r := newPauseRig(t)
	claim := r.claimFile()
	source, _ := newImportFixture(t, "broken-incoming")
	advanced := false
	r.s.importFault = func(step string) error {
		if step != "recorded" {
			return nil
		}
		if !advanced {
			advanced = true
			r.now.Add(int64(pauseHeld))
		}
		// The in-process recovery reaches the same step and fails too.
		return errors.New("publication step failed")
	}
	if _, err := r.s.Import("demo", "", source); err == nil {
		t.Fatal("failed publication reported success")
	}
	if !r.p.importBroken {
		t.Fatal("unrecovered publication left the project open")
	}
	if !bytes.Equal(r.claimFile(), claim) {
		t.Fatal("a closed project had its lease written")
	}
}

// A simulated crash writes no lease, at any point of the import; the restart
// finishes the publication and gives the active claim at least one TTL.
func TestImportCrashWritesNoLeaseAndRestartSettlesIt(t *testing.T) {
	for _, step := range []string{"copying", "prepared", "published", "recorded"} {
		t.Run(step, func(t *testing.T) {
			r := newPauseRig(t)
			claim := r.claimFile()
			source, _ := newImportFixture(t, "crash-incoming")
			r.holdAt(step, errImportCrash)
			if _, err := r.s.Import("demo", "", source); !errors.Is(err, errImportCrash) {
				t.Fatalf("simulated crash: %v", err)
			}
			if !bytes.Equal(r.claimFile(), claim) {
				t.Fatal("a crashed import wrote a lease")
			}
			r.restart()
			task, err := r.remote.Show(t.Context(), r.task)
			if err != nil || task.Claim == nil {
				t.Fatalf("claim after restart: %+v %v", task.Claim, err)
			}
			if task.Claim.Status != api.ClaimActive || task.Claim.ExpiresAt.Before(r.clock().Add(api.DefaultTTL)) {
				t.Fatalf("claim after restart: %s until %v, want active for a TTL from %v", task.Claim.Status, task.Claim.ExpiresAt, r.clock())
			}
			if _, err := os.Lstat(filepath.Join(r.p.root, "control", "import-pending.json")); !os.IsNotExist(err) {
				t.Fatalf("the restart left the publication intent: %v", err)
			}
		})
	}
}

// When an import cannot recover the publication intent an earlier one left,
// the intent stays and no lease is written: the board must keep the digest the
// intent names.
func TestImportRecoveryFailureWritesNoLease(t *testing.T) {
	r := newPauseRig(t)
	source, _ := newImportFixture(t, "interrupted-incoming")
	r.s.importFault = func(step string) error {
		if step == "prepared" {
			return errImportCrash
		}
		return nil
	}
	if _, err := r.s.Import("demo", "", source); !errors.Is(err, errImportCrash) {
		t.Fatalf("simulated crash: %v", err)
	}
	r.holdAt("published", errors.New("recovery step failed"))
	if _, err := r.s.Import("demo", "", source); err == nil {
		t.Fatal("failed recovery reported success")
	}
	staged := scanImportTestTree(t, r.p.board)
	var pending importPublication
	if found, err := readJSON(r.p.root, filepath.Join("control", "import-pending.json"), &pending); err != nil || !found {
		t.Fatalf("publication intent: %v %v", found, err)
	}
	if staged.digest != pending.stagedDigest() {
		t.Fatal("a lease write changed the published board the intent names")
	}
}

// The first import after a restart creates the dirty index set while it
// pauses the leases; the release still refreshes and clears it.
func TestImportPauseLeavesNoDirtyIndexRows(t *testing.T) {
	r := newPauseRig(t)
	r.restart()
	source, _ := newImportFixture(t, "after-restart")
	r.holdAt("prepared", nil)
	if _, err := r.s.Import("demo", "", source); err != nil {
		t.Fatal(err)
	}
	r.p.mu.Lock()
	dirty := len(r.p.indexDirty)
	r.p.mu.Unlock()
	if dirty != 0 {
		t.Fatalf("%d refreshed index rows stayed dirty", dirty)
	}
	r.assertPaused()
}
