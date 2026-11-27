package remote

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/config"
	kfs "github.com/dualface/kander/v2/internal/fs"
)

// runtimeDir creates the local runtime directory of a review run.
func (f *gcFixture) runtimeDir(run string, pending bool) string {
	f.t.Helper()
	dir := filepath.Join(f.root, "review-runtime", run)
	f.write(dir, "runtime.lock", "")
	f.write(dir, "writer-secret", strings.Repeat("a", 64))
	f.write(filepath.Join(dir, "staging"), "output.raw", "reviewer output\n")
	if pending {
		f.write(dir, "pending-finalization.json", "{}")
	}
	return dir
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return err == nil
}

func TestGCReviewRuntimeFollowsTheServerRun(t *testing.T) {
	f := newGCFixture(t)
	f.svc.runs["run-final"] = publishedRun()
	f.svc.runs["run-abandoned"] = map[string]any{"phase": "running", "abandonment": map[string]string{"decision": "d", "reason": "r"}}
	f.svc.runs["run-live"] = map[string]any{"phase": "running"}
	final, abandoned, live := f.runtimeDir("run-final", true), f.runtimeDir("run-abandoned", false), f.runtimeDir("run-live", true)
	gone, pending := f.runtimeDir("run-gone", false), f.runtimeDir("run-gone-pending", true)
	plan := f.run(true)
	if plan.ReviewRuntime.Count != 3 || !exists(t, final) {
		t.Fatalf("plan: %+v", plan.ReviewRuntime)
	}
	result := f.run(false)
	if result.ReviewRuntime.Count != 3 || result.ReviewRuntime.Bytes == 0 {
		t.Fatalf("result: %+v", result.ReviewRuntime)
	}
	for _, dir := range []string{final, abandoned, gone} {
		if exists(t, dir) {
			t.Fatalf("%s must be removed", dir)
		}
	}
	if !exists(t, live) || !exists(t, pending) {
		t.Fatal("an unfinished run and a pending finalization must stay")
	}
	if keptReason(result.ReviewRuntime, "run-gone-pending") != config.Text("gc.client_runtime_pending") || len(result.ReviewRuntime.Kept) != 1 {
		t.Fatalf("only the pending finalization is a kept candidate: %+v", result.ReviewRuntime.Kept)
	}
}

// publishedRun is a finalized run published into its only member.
func publishedRun() map[string]any {
	return map[string]any{"phase": "finalized", "task_ids": []string{gcTask}, "published": map[string]bool{gcTask: true}}
}

// A finalized run that still has to be published keeps its directory: a
// retry of the publication needs the writer secret the server bound.
func TestGCReviewRuntimeKeepsTheWriterOfAnUnpublishedRun(t *testing.T) {
	f := newGCFixture(t)
	f.svc.runs["run-unpublished"] = map[string]any{"phase": "finalized", "task_ids": []string{gcTask, "20261101-gc-other-task"}, "published": map[string]bool{gcTask: true}}
	dir := f.runtimeDir("run-unpublished", false)
	secret, err := os.ReadFile(filepath.Join(dir, "writer-secret"))
	if err != nil {
		t.Fatal(err)
	}
	for _, dryRun := range []bool{true, false} {
		result := f.run(dryRun)
		if result.ReviewRuntime.Count != 0 || keptReason(result.ReviewRuntime, "run-unpublished") != config.Text("gc.client_runtime_unpublished") {
			t.Fatalf("dry run %v: %+v", dryRun, result.ReviewRuntime)
		}
	}
	if kept, err := os.ReadFile(filepath.Join(dir, "writer-secret")); err != nil || string(kept) != string(secret) {
		t.Fatalf("the writer secret must stay as it was: %q %v", kept, err)
	}
	// Once the publication finished, the directory goes.
	f.svc.runs["run-unpublished"] = map[string]any{"phase": "finalized", "task_ids": []string{gcTask, "20261101-gc-other-task"}, "published": map[string]bool{gcTask: true, "20261101-gc-other-task": true}}
	if result := f.run(false); result.ReviewRuntime.Count != 1 || exists(t, dir) {
		t.Fatalf("a published run goes: %+v", result.ReviewRuntime)
	}
}

// The server state and the pending record are read under the runtime lock,
// the lock a reviewer holds while it prepares, finalizes and publishes.
func TestGCReviewRuntimeDecidesUnderTheRuntimeLock(t *testing.T) {
	f := newGCFixture(t)
	dir := f.runtimeDir("run-gone", false)
	held := 0
	f.svc.onRun = func(string) {
		file, err := kfs.OpenLockFile(f.s.Home, filepath.Join(dir, "runtime.lock"))
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		lock, free, err := kfs.TryLockExclusive(file)
		if err != nil {
			t.Fatal(err)
		}
		if free {
			_ = lock.Unlock()
			return
		}
		held++
	}
	if result := f.run(false); result.ReviewRuntime.Count != 1 || held != 1 {
		t.Fatalf("the run was read %d time(s) under the lock: %+v", held, result.ReviewRuntime)
	}
}

func TestGCReviewRuntimeStaysWhenTheServerFails(t *testing.T) {
	f := newGCFixture(t)
	dir := f.runtimeDir("run-final", false)
	f.svc.runErr = &client.TransportError{Err: errors.New("connection refused")}
	result := f.run(false)
	if !exists(t, dir) || len(result.Errors) == 0 || !result.Incomplete() {
		t.Fatalf("an unreachable server keeps the directory and is an error: %+v", result)
	}
}

func TestGCReviewRuntimeSkipsALockedRun(t *testing.T) {
	f := newGCFixture(t)
	f.svc.runs["run-final"] = publishedRun()
	dir := f.runtimeDir("run-final", false)
	f.holdLock(filepath.Join(dir, "runtime.lock"))
	result := f.run(false)
	if !exists(t, filepath.Join(dir, "writer-secret")) || keptReason(result.ReviewRuntime, "run-final") == "" {
		t.Fatalf("a locked run stays: %+v", result.ReviewRuntime)
	}
}

func TestGCJournalRemovesOnlyTerminalRecordsOfTerminalCards(t *testing.T) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	active := "20261101-gc-active-task"
	f.card(active, "working")
	old := time.Now().Add(-30 * 24 * time.Hour)
	record := func(dir, name, text string, at time.Time) string {
		f.write(filepath.Join(f.root, dir), name, text)
		path := filepath.Join(f.root, dir, name)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatal(err)
		}
		return path
	}
	launch := `{"schema":1,"kind":"task-launch","execution_id":"x-1","phase":"%s","task_id":"%s"}`
	request := `{"schema":1,"task_id":"%s","key":"k","phase":"%s","updated_at":"%s"}`
	stamp := func(at time.Time) string { return at.UTC().Format(time.RFC3339) }
	removed := []string{
		record("launch-actions", "a.json", fmt.Sprintf(launch, "complete", gcTask), old),
		record("request-calls", "b.json", fmt.Sprintf(request, gcTask, "failed", stamp(old)), time.Now()),
		record("request-calls", "c.json", fmt.Sprintf(request, gcTask, "complete", stamp(old)), time.Now()),
	}
	kept := []string{
		record("launch-actions", "d.json", fmt.Sprintf(launch, "complete", active), old),
		record("launch-actions", "e.json", fmt.Sprintf(launch, "prepared", gcTask), old),
		record("launch-actions", "f.json", fmt.Sprintf(launch, "complete", gcTask), time.Now()),
		record("request-calls", "g.json", fmt.Sprintf(request, gcTask, "pending", stamp(old)), old),
		record("request-calls", "h.json", fmt.Sprintf(request, gcTask, "complete", stamp(time.Now())), old),
		record("review-operations", "i.json", `["publication-1"]`, old),
		record("continuations/"+gcTask+"/x-1", "j.json", `{"base":"x"}`, old),
	}
	if plan := f.run(true); plan.Journal.Count != len(removed) {
		t.Fatalf("plan: %+v", plan.Journal)
	}
	result := f.run(false)
	if result.Journal.Count != len(removed) || result.Incomplete() {
		t.Fatalf("result: %+v", result)
	}
	for _, path := range removed {
		if exists(t, path) {
			t.Fatalf("%s must be removed", path)
		}
	}
	for _, path := range kept {
		if !exists(t, path) {
			t.Fatalf("%s must stay", path)
		}
	}
}

// pushEvidence pushes the base commit to ref on the common remote.
func (f *gcFixture) pushEvidence(ref string) {
	f.t.Helper()
	gcGit(f.t, f.repo, "push", "origin", f.base+":"+ref)
}

func (f *gcFixture) remoteHas(ref string) bool {
	return strings.Contains(gcGit(f.t, f.repo, "ls-remote", "origin"), ref)
}

func TestGCDeletesEvidenceRefsOfReadyScopesOnly(t *testing.T) {
	f := newGCFixture(t)
	group := "20261101-gc-sample-group"
	f.card(gcTask, "done").group = group
	waiting := "20261101-gc-waiting-task"
	f.card(waiting, "done")
	member := api.DeliveryRef(f.s.Info.ID, gcTask, 1, f.base)
	other := api.DeliveryRef(f.s.Info.ID, waiting, 1, f.base)
	source := api.IntegrationSourceRef(f.s.Info.ID, "x-integration", f.base)
	for _, ref := range []string{member, other, source} {
		f.pushEvidence(ref)
	}
	f.svc.taskScopes[gcTask] = api.EvidenceCleanup{Tasks: []string{gcTask}}
	f.svc.taskScopes[waiting] = api.EvidenceCleanup{Tasks: []string{waiting}}
	f.svc.groupScopes[group] = api.EvidenceCleanup{Ready: true, Tasks: []string{gcTask}, Sources: []api.EvidenceSource{{ExecutionID: "x-integration", Commit: f.base}}}
	gcGit(t, f.repo, "fetch", "origin", "+refs/heads/kander/*:refs/remotes/origin/kander/*")
	if plan := f.run(true); plan.EvidenceRefs.Count != 2 || !f.remoteHas(member) {
		t.Fatalf("plan: %+v", plan.EvidenceRefs)
	}
	result := f.run(false)
	if result.EvidenceRefs.Count != 2 || result.Incomplete() {
		t.Fatalf("result: %+v", result)
	}
	if f.remoteHas(member) || f.remoteHas(source) || !f.remoteHas(other) {
		t.Fatal("only the refs of a Ready scope go")
	}
	tracking := gcGit(t, f.repo, "for-each-ref", "--format=%(refname)", "refs/remotes/origin/kander/")
	if strings.Contains(tracking, gcTask) || !strings.Contains(tracking, waiting) {
		t.Fatalf("the prune must drop only the deleted refs' tracking refs:\n%s", tracking)
	}
}
