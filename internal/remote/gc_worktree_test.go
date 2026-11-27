package remote

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
	kfs "github.com/dualface/kander/v2/internal/fs"
)

const gcTask = "20261101-gc-sample-task"

func TestGCRemovesTerminalWorktreeAndBranch(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	plan := f.run(true)
	if plan.Worktrees.Count != 1 || plan.Branches.Count != 1 || plan.Worktrees.Bytes == 0 || plan.Incomplete() {
		t.Fatalf("plan: %+v", plan)
	}
	if !f.registered(w.Path) || !f.branchExists(w.Branch) {
		t.Fatal("a dry run must not remove the worktree or the branch")
	}
	result := f.run(false)
	if result.Worktrees.Count != 1 || result.Branches.Count != 1 || result.Incomplete() {
		t.Fatalf("result: %+v", result)
	}
	if f.registered(w.Path) || f.branchExists(w.Branch) {
		t.Fatal("the worktree and its branch must be gone")
	}
	for _, path := range []string{filepath.Join(f.root, "worktrees", gcTask), filepath.Join(f.root, "worktree-intents", gcTask)} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s must be removed once empty: %v", path, err)
		}
	}
	again := f.run(false)
	if !again.Empty() || again.Incomplete() {
		t.Fatalf("a second run must find nothing: %+v", again)
	}
}

// Conditions 1 and 2 make an entry a candidate; an entry that fails them is
// neither removed nor reported.
func TestGCLeavesCardsThatAreNotCandidates(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "working")
	recent := "20261101-gc-recent-task"
	f.card(recent, "done").finished = time.Now().Format("2006-01-02 15:04")
	first, second := f.worktree(gcTask, 1), f.worktree(recent, 1)
	result := f.run(false)
	if !result.Empty() || result.Incomplete() {
		t.Fatalf("result: %+v", result)
	}
	if !f.registered(first.Path) || !f.registered(second.Path) {
		t.Fatal("worktrees that are not candidates must stay")
	}
}

func TestGCKeepsWorktreeForEachProtectingCondition(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(f *gcFixture, w ExecutionWorktree)
		reason  func(f *gcFixture, w ExecutionWorktree) string
	}{
		{"standing claim", func(f *gcFixture, w ExecutionWorktree) {
			f.svc.cards[gcTask].task.Claim = &api.ClaimView{ClientID: f.s.Client.ClientID(), ExecutionID: w.ExecutionID, Generation: 1, Status: api.ClaimActive}
		}, func(*gcFixture, ExecutionWorktree) string { return config.Text("gc.client_claim_valid") }},
		{"unreleased credential", func(f *gcFixture, w ExecutionWorktree) {
			f.write(credentialDir(f.s.Home, f.s.ServerURL, f.s.Info.ID, gcTask), w.ExecutionID+".json", "{}")
		}, func(f *gcFixture, w ExecutionWorktree) string {
			return config.Text("gc.client_credential", filepath.Join(credentialDir(f.s.Home, f.s.ServerURL, f.s.Info.ID, gcTask), w.ExecutionID+".json"))
		}},
		{"executor alive", func(f *gcFixture, w ExecutionWorktree) {
			f.retain(w, "herdr:pane-1")
			f.probe = []string{"alive"}
		}, func(*gcFixture, ExecutionWorktree) string {
			return config.Text("gc.client_executor", "herdr:pane-1 alive", gcTask)
		}},
		{"executor unknown", func(f *gcFixture, w ExecutionWorktree) {
			f.retain(w, "herdr:pane-1")
			f.probe = []string{"unknown"}
		}, func(*gcFixture, ExecutionWorktree) string {
			return config.Text("gc.client_executor", "herdr:pane-1 unknown", gcTask)
		}},
		{"lock held", func(f *gcFixture, w ExecutionWorktree) {
			f.holdLock(filepath.Join(credentialDir(f.s.Home, f.s.ServerURL, f.s.Info.ID, gcTask), "worktree-"+w.ExecutionID+".lock"))
		}, func(f *gcFixture, w ExecutionWorktree) string {
			return config.Text("gc.client_locked", filepath.Join(credentialDir(f.s.Home, f.s.ServerURL, f.s.Info.ID, gcTask), "worktree-"+w.ExecutionID+".lock"))
		}},
		{"other repository", func(f *gcFixture, w ExecutionWorktree) {
			other := filepath.Join(t.TempDir(), "other")
			gcGit(t, filepath.Dir(other), "clone", f.bare, other)
			f.checkout = other
		}, nil},
		{"command inside the worktree", func(f *gcFixture, w ExecutionWorktree) {
			f.checkout = filepath.Join(w.Path, "sub")
		}, func(*gcFixture, ExecutionWorktree) string { return config.Text("gc.client_inside") }},
		{"uncommitted change", func(f *gcFixture, w ExecutionWorktree) {
			f.write(w.Path, "work.txt", "edited\n")
		}, func(*gcFixture, ExecutionWorktree) string { return config.Text("gc.client_dirty") }},
		{"untracked file", func(f *gcFixture, w ExecutionWorktree) {
			f.write(w.Path, "notes.txt", "draft\n")
		}, func(*gcFixture, ExecutionWorktree) string { return config.Text("gc.client_dirty") }},
		{"unintegrated commit", func(f *gcFixture, w ExecutionWorktree) {
			f.commit(w, "local only\n")
		}, nil},
		{"delivery without evidence", func(f *gcFixture, w ExecutionWorktree) {
			tip := f.commit(w, "delivered\n")
			f.svc.cards[gcTask].task.Delivery = &api.DeliveryEvidence{TaskID: gcTask, Generation: 1, Commit: tip}
		}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newGCFixture(t)
			f.card(gcTask, "done")
			w := f.worktree(gcTask, 1)
			c.prepare(f, w)
			before := snapshot(t, w.Path)
			result := f.run(false)
			reason := keptReason(result.Worktrees, gcTask+"/g1")
			if reason == "" || result.Worktrees.Count != 0 || !result.Incomplete() {
				t.Fatalf("the worktree must be kept with a reason: %+v", result)
			}
			if c.reason != nil && reason != c.reason(f, w) {
				t.Fatalf("reason %q, want %q", reason, c.reason(f, w))
			}
			if !strings.Contains(snapshotKeys(before), "work.txt") || !sameSnapshot(before, snapshot(t, w.Path)) {
				t.Fatal("the files of a kept worktree must not change")
			}
			if !f.branchExists(w.Branch) {
				t.Fatal("the branch of a kept worktree must stay")
			}
		})
	}
}

func snapshotKeys(m map[string]string) string {
	var keys []string
	for key := range m {
		keys = append(keys, key)
	}
	return strings.Join(keys, "\n")
}

// retain records a retained execution of w with a window and a session.
func (f *gcFixture) retain(w ExecutionWorktree, window string) {
	f.t.Helper()
	value := RetainedExecution{ServerURL: f.s.ServerURL, ClientID: f.s.Client.ClientID(), ProjectID: f.s.Info.ID, TaskID: gcTask, ExecutionID: w.ExecutionID, Generation: w.Generation, Session: "claude s-1", Window: window, Worktree: w.Path}
	f.writeJSON(filepath.Join(f.s.Home, "retained-executions", projectNamespace(f.s.ServerURL, f.s.Info.ID), gcTask), w.ExecutionID+".json", value)
}

func (f *gcFixture) writeJSON(dir, name string, value any) {
	f.t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		f.t.Fatal(err)
	}
	f.write(dir, name, string(data))
}

// holdLock holds the exclusive lock of path until the test ends, as another
// kander process would.
func (f *gcFixture) holdLock(path string) {
	f.t.Helper()
	if err := kfs.EnsurePrivateDirectory(f.s.Home, filepath.Dir(path), true); err != nil {
		f.t.Fatal(err)
	}
	file, err := kfs.OpenLockFile(f.s.Home, path)
	if err != nil {
		f.t.Fatal(err)
	}
	lock, err := kfs.LockExclusive(file)
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = lock.Unlock(); _ = file.Close() })
}

func TestGCRemovesStoppedExecutorAndExecutorlessRecords(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	f.retain(w, "herdr:pane-1")
	// A hand claim records neither a window nor a session: nothing to probe.
	f.writeJSON(filepath.Join(f.s.Home, "retained-executions", projectNamespace(f.s.ServerURL, f.s.Info.ID), gcTask), "x-hand.json", RetainedExecution{TaskID: gcTask, ExecutionID: "x-hand", Generation: 1})
	if result := f.run(false); result.Worktrees.Count != 1 || result.Incomplete() {
		t.Fatalf("result: %+v", result)
	}
}

func TestGCAcceptsIntegratedTipAndPublishedDelivery(t *testing.T) {
	for _, name := range []string{"integrated", "evidence ref"} {
		t.Run(name, func(t *testing.T) {
			f := newGCFixture(t)
			card := f.card(gcTask, "done")
			w := f.worktree(gcTask, 1)
			tip := f.commit(w, "delivered\n")
			if name == "integrated" {
				gcGit(t, f.repo, "push", "origin", tip+":refs/heads/v2")
			} else {
				card.task.Delivery = &api.DeliveryEvidence{TaskID: gcTask, Generation: 1, Commit: tip}
				gcGit(t, f.repo, "push", "origin", tip+":"+api.DeliveryRef(f.s.Info.ID, gcTask, 1, tip))
			}
			result := f.run(false)
			if result.Worktrees.Count != 1 || result.Branches.Count != 1 || result.Incomplete() {
				t.Fatalf("result: %+v", result)
			}
			if f.registered(w.Path) || f.branchExists(w.Branch) {
				t.Fatal("the worktree and its branch must be gone")
			}
		})
	}
}

func TestGCNeverDeletesBranchesOutsideTheTaskNames(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	w := f.worktreeOn(gcTask, 1, "feature/gc-sample")
	result := f.run(false)
	if keptReason(result.Worktrees, gcTask+"/g1") != config.Text("gc.client_branch_name", "feature/gc-sample") {
		t.Fatalf("result: %+v", result)
	}
	if !f.registered(w.Path) || !f.branchExists("feature/gc-sample") {
		t.Fatal("a branch outside the task branch names must stay")
	}
}

// A new generation has its own lock and directory: while its creation holds
// its lock, removing the old generation leaves it alone.
func TestGCLeavesNewGenerationWhileRemovingOldOne(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	old, current := f.worktree(gcTask, 1), f.worktree(gcTask, 2)
	f.holdLock(filepath.Join(credentialDir(f.s.Home, f.s.ServerURL, f.s.Info.ID, gcTask), "worktree-"+current.ExecutionID+".lock"))
	f.write(current.Path, "notes.txt", "in progress\n")
	before := snapshot(t, current.Path)
	result := f.run(false)
	if result.Worktrees.Count != 1 || keptReason(result.Worktrees, gcTask+"/g2") == "" {
		t.Fatalf("result: %+v", result)
	}
	if f.registered(old.Path) || !f.registered(current.Path) || !f.branchExists(current.Branch) {
		t.Fatal("only the old generation may go")
	}
	if !sameSnapshot(before, snapshot(t, current.Path)) {
		t.Fatal("the new generation must not change")
	}
	if _, err := os.Lstat(filepath.Join(f.root, "worktree-intents", gcTask, "g2.json")); err != nil {
		t.Fatalf("the new generation's intent must stay: %v", err)
	}
}

// A dry run never fetches: a target commit this repository lacks is reported
// as decided after the fetch, and nothing in the client directory, the
// repository or the remote changes.
func TestGCDryRunNeitherFetchesNorChangesAnything(t *testing.T) {
	f := newGCFixture(t)
	card := f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	tip := f.commit(w, "delivered\n")
	// Another clone integrates the tip and moves the target past it, so this
	// repository has the tip but not the new target commit.
	other := filepath.Join(t.TempDir(), "other")
	gcGit(t, filepath.Dir(other), "clone", f.bare, other)
	gcGit(t, w.Path, "push", "origin", tip+":refs/heads/v2")
	gcGit(t, other, "fetch", "origin", "v2")
	gcGit(t, other, "checkout", "-q", "FETCH_HEAD")
	f.write(other, "later.txt", "later\n")
	gcGit(t, other, "add", "later.txt")
	gcGit(t, other, "commit", "-m", "Later")
	gcGit(t, other, "push", "origin", "HEAD:refs/heads/v2")
	evidence := api.DeliveryRef(f.s.Info.ID, gcTask, 1, tip)
	gcGit(t, w.Path, "push", "origin", tip+":"+evidence)
	f.svc.taskScopes[gcTask] = api.EvidenceCleanup{Ready: true, Tasks: []string{gcTask}}
	card.task.Delivery = nil
	home, refs, remote := snapshot(t, f.s.Home), gcGit(t, f.repo, "for-each-ref"), gcGit(t, f.repo, "ls-remote", "origin")
	plan := f.run(true)
	if plan.Worktrees.Count != 0 || len(plan.Worktrees.Deferred) != 1 || plan.Worktrees.Deferred[0].ID != gcTask+"/g1" || plan.Incomplete() {
		t.Fatalf("plan: %+v", plan)
	}
	if plan.EvidenceRefs.Count != 1 {
		t.Fatalf("the plan must count the evidence ref: %+v", plan)
	}
	if !sameSnapshot(home, snapshot(t, f.s.Home)) || refs != gcGit(t, f.repo, "for-each-ref") || remote != gcGit(t, f.repo, "ls-remote", "origin") || !f.registered(w.Path) {
		t.Fatal("a dry run must change no data file, ref, worktree or remote ref")
	}
	result := f.run(false)
	if result.Worktrees.Count != 1 || result.EvidenceRefs.Count != 1 || result.Incomplete() {
		t.Fatalf("the run fetches and then removes: %+v", result)
	}
	if strings.Contains(gcGit(t, f.repo, "ls-remote", "origin"), evidence) {
		t.Fatal("the evidence ref must be deleted")
	}
}
