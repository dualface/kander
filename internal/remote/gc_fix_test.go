package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
)

// moveTargetPast integrates tip and then moves the integration target past it
// from another clone, so the fixture's repository lacks the target commit and
// a dry run must defer the worktree to the fetch of a run.
func (f *gcFixture) moveTargetPast(w ExecutionWorktree, tip string) {
	f.t.Helper()
	other := filepath.Join(f.t.TempDir(), "other")
	gcGit(f.t, filepath.Dir(other), "clone", f.bare, other)
	gcGit(f.t, w.Path, "push", "origin", tip+":refs/heads/v2")
	gcGit(f.t, other, "fetch", "origin", "v2")
	gcGit(f.t, other, "checkout", "-q", "FETCH_HEAD")
	f.write(other, "later.txt", "later\n")
	gcGit(f.t, other, "add", "later.txt")
	gcGit(f.t, other, "commit", "-m", "Later")
	gcGit(f.t, other, "push", "origin", "HEAD:refs/heads/v2")
}

// The integration fetch changes only its own tracking ref: a configured
// refmap that maps the target onto a local branch, tag following and tag
// pruning leave the user's refs alone.
func TestGCIntegrationFetchChangesNoOtherRef(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	tip := f.commit(w, "delivered\n")
	gcGit(t, f.repo, "push", "origin", tip+":refs/heads/v2")
	gcGit(t, f.repo, "tag", "-a", "-m", "Release", "release-1", tip)
	gcGit(t, f.repo, "push", "origin", "release-1")
	gcGit(t, f.repo, "tag", "-d", "release-1")
	gcGit(t, f.repo, "tag", "local-only", f.base)
	gcGit(t, f.repo, "branch", "mine", f.base)
	for _, setting := range [][]string{{"fetch.prune", "true"}, {"fetch.pruneTags", "true"}, {"remote.origin.pruneTags", "true"}} {
		gcGit(t, f.repo, "config", setting[0], setting[1])
	}
	gcGit(t, f.repo, "config", "--add", "remote.origin.fetch", "+refs/heads/v2:refs/heads/mine")
	result := f.run(false)
	if result.Worktrees.Count != 1 || result.Incomplete() {
		t.Fatalf("result: %+v", result)
	}
	if got := gcGit(t, f.repo, "rev-parse", "mine"); got != f.base {
		t.Fatalf("the refmap must not move the user's branch: %s", got)
	}
	if tags := gcGit(t, f.repo, "tag"); tags != "local-only" {
		t.Fatalf("no tag may be fetched or pruned: %q", tags)
	}
}

// Stale tracking refs below the project's evidence prefix go, also when no
// remote ref was deleted in the same run; other refs stay.
func TestGCRemovesStaleEvidenceTrackingRefs(t *testing.T) {
	f := newGCFixture(t)
	live := api.DeliveryRef(f.s.Info.ID, gcTask, 1, f.base)
	f.pushEvidence(live)
	gcGit(t, f.repo, "fetch", "origin", "+refs/heads/kander/*:refs/remotes/origin/kander/*")
	stale := evidenceTracking(f.s.Info.ID) + gcTask + "/g9/" + f.base
	gcGit(t, f.repo, "update-ref", stale, f.base)
	gcGit(t, f.repo, "update-ref", "refs/remotes/origin/feature", f.base)
	refs := gcGit(t, f.repo, "for-each-ref")
	if plan := f.run(true); plan.TrackingRefs.Count != 1 || refs != gcGit(t, f.repo, "for-each-ref") {
		t.Fatalf("plan: %+v", plan.TrackingRefs)
	}
	result := f.run(false)
	if result.TrackingRefs.Count != 1 || result.Incomplete() {
		t.Fatalf("result: %+v", result)
	}
	after := gcGit(t, f.repo, "for-each-ref", "--format=%(refname)")
	if strings.Contains(after, stale) || !strings.Contains(after, "refs/remotes/origin/kander/"+strings.TrimPrefix(live, "refs/heads/kander/")) || !strings.Contains(after, "refs/remotes/origin/feature") {
		t.Fatalf("only the stale tracking ref goes:\n%s", after)
	}
}

// A standalone card's integration source ref belongs to the card's scope: it
// goes when it is the only evidence ref left.
func TestGCDeletesTheIntegrationSourceOfAStandaloneCard(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	source := api.IntegrationSourceRef(f.s.Info.ID, "x-integration", f.base)
	f.pushEvidence(source)
	f.svc.taskScopes[gcTask] = api.EvidenceCleanup{Ready: true, Tasks: []string{gcTask}, Sources: []api.EvidenceSource{{ExecutionID: "x-integration", Commit: f.base}}}
	if plan := f.run(true); plan.EvidenceRefs.Count != 1 || !f.remoteHas(source) {
		t.Fatalf("plan: %+v", plan.EvidenceRefs)
	}
	if result := f.run(false); result.EvidenceRefs.Count != 1 || result.Incomplete() || f.remoteHas(source) {
		t.Fatalf("result: %+v", result)
	}
}

// Without a client project directory the bound project's evidence refs are
// still handled through the checkout; the directory is not created.
func TestGCHandlesEvidenceRefsWithoutAProjectDirectory(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	ref := api.DeliveryRef(f.s.Info.ID, gcTask, 1, f.base)
	f.pushEvidence(ref)
	f.svc.taskScopes[gcTask] = api.EvidenceCleanup{Ready: true, Tasks: []string{gcTask}}
	if err := os.RemoveAll(f.root); err != nil {
		t.Fatal(err)
	}
	if plan := f.run(true); plan.EvidenceRefs.Count != 1 || exists(t, f.root) {
		t.Fatalf("plan: %+v", plan)
	}
	if result := f.run(false); result.EvidenceRefs.Count != 1 || result.Incomplete() || f.remoteHas(ref) {
		t.Fatalf("result: %+v", result)
	}
}

// A plan whose only items are a deferred worktree or an emptied task
// directory still runs: the deferred worktree is judged after the fetch.
func TestGCCommandRunsAPlanWithOnlyDeferredOrDirectoryWork(t *testing.T) {
	t.Run("deferred", func(t *testing.T) {
		f := newGCFixture(t)
		f.card(gcTask, "done")
		w := f.worktree(gcTask, 1)
		f.moveTargetPast(w, f.commit(w, "delivered\n"))
		c := &gcCommand{f: f, skipped: "remote"}
		code, out, errOut := c.run(t, "--yes")
		if code != 0 || f.registered(w.Path) || !strings.Contains(out, config.Text("gc.result_header")) {
			t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
		}
	})
	t.Run("empty directory", func(t *testing.T) {
		f := newGCFixture(t)
		f.card(gcTask, "done")
		dir := filepath.Join(f.root, "worktrees", gcTask)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		c := &gcCommand{f: f, skipped: "remote"}
		if code, out, errOut := c.run(t, "--yes"); code != 0 || exists(t, dir) {
			t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
		}
	})
}

// interruptAfterWorktree leaves what a run interrupted right after the
// worktree removal leaves: the removal record, the branch and the intent.
func (f *gcFixture) interruptAfterWorktree(w ExecutionWorktree, tip string) string {
	f.t.Helper()
	record := gcRemoval{Schema: 1, Repository: f.repo, Path: w.Path, Branch: w.Branch, Tip: tip, ExecutionID: w.ExecutionID, Generation: w.Generation}
	data, _ := json.Marshal(record)
	path := filepath.Join(f.root, "gc-removals", gcTask, "g1.json")
	f.write(filepath.Dir(path), filepath.Base(path), string(data))
	gcGit(f.t, f.repo, "worktree", "remove", w.Path)
	return path
}

func TestGCFinishesARemovalInterruptedAfterTheWorktree(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	record := f.interruptAfterWorktree(w, f.base)
	intent := filepath.Join(f.root, "worktree-intents", gcTask, "g1.json")
	if plan := f.run(true); plan.Branches.Count != 1 || plan.Empty() || !f.branchExists(w.Branch) || !exists(t, record) {
		t.Fatalf("plan: %+v", plan)
	}
	result := f.run(false)
	if result.Branches.Count != 1 || result.Incomplete() || f.branchExists(w.Branch) || exists(t, intent) || exists(t, record) {
		t.Fatalf("result: %+v", result)
	}
}

func TestGCKeepsABranchThatMovedAfterAnInterruptedRemoval(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	moved := f.commit(w, "later work\n")
	record := f.interruptAfterWorktree(w, f.base)
	result := f.run(false)
	if keptReason(result.Branches, w.Branch) != config.Text("gc.client_branch_moved", f.base, moved) || !f.branchExists(w.Branch) || exists(t, record) {
		t.Fatalf("result: %+v", result)
	}
}

// usePartialClone makes the fixture work in a blobless partial clone of its
// origin, whose promisor remote would serve missing objects on demand.
func (f *gcFixture) usePartialClone() string {
	f.t.Helper()
	gcGit(f.t, f.bare, "config", "uploadpack.allowFilter", "true")
	gcGit(f.t, f.bare, "config", "uploadpack.allowAnySHA1InWant", "true")
	partial := filepath.Join(f.t.TempDir(), "partial")
	gcGit(f.t, filepath.Dir(partial), "clone", "--filter=blob:none", "--no-checkout", "file://"+f.bare, partial)
	gcGit(f.t, partial, "remote", "set-url", "origin", f.bare)
	gcGit(f.t, partial, "checkout", "-q", "-B", "main", f.base)
	f.repo, f.checkout = partial, partial
	return partial
}

// withoutLazyFetchControl stands for a Git older than 2.45, which cannot be
// kept from fetching lazily.
func withoutLazyFetchControl(t *testing.T) {
	previous := gcLazyFetchControl
	t.Cleanup(func() { gcLazyFetchControl = previous })
	gcLazyFetchControl = func() bool { return false }
}

// A Git that cannot be kept from fetching lazily runs no probe in a partial
// clone's dry run: the worktree is deferred before any of them, and the object
// store does not change. A full clone has nothing to fetch lazily.
func TestGCDryRunDefersAPartialCloneWhenGitCannotStopLazyFetch(t *testing.T) {
	withoutLazyFetchControl(t)
	f := newGCFixture(t)
	partial := f.usePartialClone()
	f.card(gcTask, "done")
	f.worktree(gcTask, 1)
	before := gcGit(t, partial, "count-objects", "-v")
	plan := f.run(true)
	if len(plan.Worktrees.Deferred) != 1 || plan.Worktrees.Deferred[0].Reason != config.Text("gc.client_lazy_fetch_deferred") || plan.Worktrees.Count != 0 {
		t.Fatalf("plan: %+v", plan.Worktrees)
	}
	if after := gcGit(t, partial, "count-objects", "-v"); after != before {
		t.Fatalf("the object store changed:\n%s\n%s", before, after)
	}
	full := newGCFixture(t)
	full.card(gcTask, "done")
	full.worktree(gcTask, 1)
	if plan := full.run(true); plan.Worktrees.Count != 1 || len(plan.Worktrees.Deferred) != 0 {
		t.Fatalf("a full clone is judged as before: %+v", plan.Worktrees)
	}
}

// A dry run in a partial clone never fetches a missing object lazily: the
// worktree is deferred and the object store does not change.
func TestGCDryRunNeverFetchesLazilyInAPartialClone(t *testing.T) {
	f := newGCFixture(t)
	partial := f.usePartialClone()
	f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	tip := f.commit(w, "delivered\n")
	f.moveTargetPast(w, tip)
	target := strings.Fields(gcGit(t, f.bare, "rev-parse", "refs/heads/v2"))[0]
	objects := func() string { return gcGit(t, partial, "count-objects", "-v") }
	before := objects()
	plan := f.run(true)
	if len(plan.Worktrees.Deferred) != 1 || plan.Worktrees.Count != 0 {
		t.Fatalf("plan: %+v", plan)
	}
	if objects() != before {
		t.Fatalf("the object store changed:\n%s\n%s", before, objects())
	}
	probe := exec.Command("git", "-C", partial, "cat-file", "-e", target+"^{commit}")
	probe.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1")
	if probe.Run() == nil {
		t.Fatal("the target commit must still be missing")
	}
}

// A journal record that cannot be read or decoded is a kept entry with the
// reason, never silently skipped.
func TestGCReportsJournalRecordsItCannotRead(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions that bind the user")
	}
	f := newGCFixture(t)
	f.card(gcTask, "done")
	f.write(filepath.Join(f.root, "launch-actions"), "damaged.json", "{not json")
	f.write(filepath.Join(f.root, "request-calls"), "locked.json", `{"schema":1,"task_id":"`+gcTask+`","phase":"complete"}`)
	locked := filepath.Join(f.root, "request-calls", "locked.json")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o600) })
	result := f.run(true)
	if keptReason(result.Journal, "launch-actions/damaged.json") == "" || keptReason(result.Journal, "request-calls/locked.json") == "" || !result.Incomplete() {
		t.Fatalf("result: %+v", result.Journal)
	}
}

// The working directory guard compares physical paths: a checkout entered
// through a symbolic link still protects its worktree.
func TestGCKeepsTheWorktreeTheCommandEnteredThroughALink(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(w.Path, link); err != nil {
		t.Skip("symbolic links are not available: ", err)
	}
	f.checkout = link
	result := f.run(false)
	if keptReason(result.Worktrees, gcTask+"/g1") != config.Text("gc.client_inside") || !f.registered(w.Path) {
		t.Fatalf("result: %+v", result.Worktrees)
	}
}

// Integration evidence counts only from the project's common remote: a clone
// whose origin is a fork keeps the worktree even when the fork integrated it.
func TestGCKeepsAWorktreeWhoseCloneTracksAnotherRemote(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	tip := f.commit(w, "delivered\n")
	fork := filepath.Join(t.TempDir(), "fork.git")
	gcGit(t, filepath.Dir(fork), "clone", "--bare", f.bare, fork)
	gcGit(t, f.repo, "remote", "set-url", "origin", fork)
	gcGit(t, f.repo, "push", "origin", tip+":refs/heads/v2")
	f.checkout = ""
	result := f.run(false)
	if !strings.HasPrefix(keptReason(result.Worktrees, gcTask+"/g1"), strings.SplitN(config.Text("gc.client_origin_unverified", f.repo, "x"), ":", 2)[0]) || !f.registered(w.Path) || !f.branchExists(w.Branch) {
		t.Fatalf("result: %+v", result.Worktrees)
	}
}

// gcCLI runs `kander gc` with the production open against a fake server and
// records the requests it makes.
func gcCLI(t *testing.T, home string, project api.Project) (run func(args ...string) (int, string, string), methods func() []string) {
	t.Helper()
	var mu sync.Mutex
	var seen []string
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/version":
			_ = json.NewEncoder(w).Encode(api.VersionInfo{Protocol: api.ProtocolVersion, MinProtocol: api.MinProtocolVersion, ServerVersion: "test"})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects":
			_ = json.NewEncoder(w).Encode(project)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/projects/"+project.ID+"/tasks":
			_ = json.NewEncoder(w).Encode(api.TaskList{})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeNotFound, Message: r.URL.Path})
		}
	}))
	t.Cleanup(hs.Close)
	t.Setenv(EnvClientHome, home)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv(EnvExecutionID, "")
	t.Setenv(EnvCoordinatorExecutionID, "")
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.Language = "en"
	cfg.Server.URL = hs.URL
	cfg.Project.Name = project.Name
	if _, err := config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	chdir(t, t.TempDir())
	return func(args ...string) (int, string, string) {
			return commandtest.Capture(func() int { return commandtest.Run(NewGCCommand(nil), args...) })
		}, func() []string {
			mu.Lock()
			defer mu.Unlock()
			return append([]string(nil), seen...)
		}
}

// On a host that never ran a client command, a dry run creates no client
// identity or directory and sends only reads; the server part is still
// planned, because a local server can hold data without a client here.
func TestGCCommandCreatesNothingOnAnEmptyHome(t *testing.T) {
	home := filepath.Join(t.TempDir(), "client")
	run, methods := gcCLI(t, home, api.Project{ID: "p-gcopen", Name: "demo"})
	previous := resolveServerGC
	t.Cleanup(func() { resolveServerGC = previous })
	planned := 0
	resolveServerGC = func(context.Context, string) (serverGC, string, error) {
		return func(_ context.Context, req api.ServerGCRequest) (api.ServerGCReport, error) {
			planned++
			return api.ServerGCReport{DryRun: req.DryRun}, nil
		}, "", nil
	}
	code, out, errOut := run("--dry-run", "--json")
	var report GCReport
	if err := json.Unmarshal([]byte(out), &report); err != nil || code != 0 || len(report.Projects) != 1 || exists(t, home) || planned != 1 {
		t.Fatalf("exit %d, planned %d\n%s\n%s", code, planned, out, errOut)
	}
	for _, request := range methods() {
		if !strings.HasPrefix(request, http.MethodGet+" ") || !strings.Contains(request, "/api/v1/") {
			t.Fatalf("an unidentified plan sends only reads: %v", methods())
		}
	}
}

// Without a client identity the local categories cannot tell this host's
// claims and credentials: local data of the project is reported and kept,
// while the evidence refs are still handled through the checkout.
func TestGCReportsLocalDataItCannotJudgeWithoutAnIdentity(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	ref := api.DeliveryRef(f.s.Info.ID, gcTask, 1, f.base)
	f.pushEvidence(ref)
	f.svc.taskScopes[gcTask] = api.EvidenceCleanup{Ready: true, Tasks: []string{gcTask}}
	before := snapshot(t, f.root)
	opts := f.options(false)
	opts.Unidentified = true
	result := f.s.CollectGarbage(t.Context(), opts)
	if len(result.Errors) != 1 || !strings.Contains(result.Errors[0], "client-id") || result.EvidenceRefs.Count != 1 || f.remoteHas(ref) {
		t.Fatalf("result: %+v", result)
	}
	if !f.registered(w.Path) || !sameSnapshot(before, snapshot(t, f.root)) {
		t.Fatal("the local data must stay")
	}
}

// With a client identity the plan only reads: it registers no project,
// starts no worker and writes nothing below the client directory.
func TestGCCommandOnlyReadsWhilePlanning(t *testing.T) {
	home := filepath.Join(t.TempDir(), "client")
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "client-id"), []byte("c-gc-open\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run, methods := gcCLI(t, home, api.Project{ID: "p-gcopen", Name: "demo"})
	before := snapshot(t, home)
	code, out, errOut := run("--dry-run", "--client-only")
	if code != 0 || !sameSnapshot(before, snapshot(t, home)) {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
	entries, _ := os.ReadDir(home)
	if len(entries) != 1 {
		t.Fatalf("the client directory gained entries: %v", entries)
	}
	for _, request := range methods() {
		if !strings.HasPrefix(request, http.MethodGet+" ") {
			t.Fatalf("a plan sends only reads: %v", methods())
		}
	}
}

// A failed `git worktree remove` whose worktree directory is gone and whose
// registration Git dropped finishes the removal: the branch, the intent and
// the record go in the same run, and the Git error is reported. A Git wrapper
// runs the real removal and then reports an error, the way a removal that
// finished before its process was reaped does.
func TestGCFinishesARemovalWhoseGitErrorLeftNoDirectory(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	w := f.worktree(gcTask, 1)
	wrapGit(t, `"$real" "$@"
code=$?
case "$*" in *"worktree remove"*) echo "`+gcRemoveFailure+`" >&2; exit 1;; esac
exit $code
`)
	result := f.run(false)
	if result.Worktrees.Count != 1 || !result.Incomplete() || len(result.Errors) != 1 || !strings.Contains(result.Errors[0], gcRemoveFailure) {
		t.Fatalf("the removal counts and its Git error is reported: %+v", result)
	}
	if f.registered(w.Path) || f.branchExists(w.Branch) || exists(t, filepath.Join(f.root, "worktree-intents", gcTask, "g1.json")) || exists(t, filepath.Join(f.root, "gc-removals", gcTask, "g1.json")) {
		t.Fatalf("worktree, branch, intent and record must go: %+v", result)
	}
}

// gcRemoveFailure is the stderr line the Git wrappers print for a failed
// removal.
const gcRemoveFailure = "fatal: simulated worktree removal failure"

// wrapGit puts a POSIX shell script named git first on PATH for the rest of
// the test. The script body sees the real Git as $real.
func wrapGit(t *testing.T, body string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the Git wrapper is a POSIX shell script")
	}
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte("#!/bin/sh\nreal='"+real+"'\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}
