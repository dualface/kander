package remote_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/remote"
)

// mergeIdentity isolates Git and gives the first clone the identity the merge
// commits of the client carry.
func (f *integrationFixture) mergeIdentity() {
	f.t.Helper()
	f.isolateGit()
	deliveryGit(f.t, f.first, "config", "user.name", "Merge Author")
	deliveryGit(f.t, f.first, "config", "user.email", "merge@example.invalid")
}

// localState is everything a generated merge must leave alone in the work tree.
func (f *integrationFixture) localState() string {
	f.t.Helper()
	return strings.Join([]string{
		deliveryGit(f.t, f.first, "rev-parse", "HEAD"),
		deliveryGit(f.t, f.first, "symbolic-ref", "HEAD"),
		deliveryGit(f.t, f.first, "status", "--porcelain"),
		deliveryGit(f.t, f.first, "for-each-ref", "--format=%(refname) %(objectname)", "refs/heads"),
	}, "|")
}

func (f *integrationFixture) privateRefs() string {
	f.t.Helper()
	return deliveryGit(f.t, f.first, "for-each-ref", "refs/kander")
}

func (f *integrationFixture) parents(commit string) []string {
	f.t.Helper()
	return strings.Fields(deliveryGit(f.t, f.first, "rev-list", "--parents", "-n1", commit))[1:]
}

func (f *integrationFixture) noClaim() {
	f.t.Helper()
	if _, err := f.a.Project.Integration(f.t.Context(), api.IntegrationID("refs/heads/v2")); !api.IsCode(err, api.CodeNotFound) {
		f.t.Fatalf("a rejected integration claimed the target: %v", err)
	}
}

type journalAttempt struct {
	Request      api.IntegrationRequest `json:"request"`
	Retired      bool                   `json:"retired"`
	RetiredFor   string                 `json:"retired_for"`
	PushAttempts int                    `json:"push_attempts"`
}

// attempts reads the private operation journal of the first host.
func (f *integrationFixture) attempts(operation string) []journalAttempt {
	f.t.Helper()
	journal, _, err := f.a.BeginAction("integration", operation)
	if err != nil {
		f.t.Fatal(err)
	}
	defer journal.Close()
	var state struct {
		Attempts []journalAttempt `json:"attempts"`
	}
	if err := json.Unmarshal(journal.Value.Result, &state); err != nil {
		f.t.Fatal(err)
	}
	return state.Attempts
}

// watch reports every service call about the v2 integration to onCall before the
// service sees it. A non-zero answer is returned to the client in place of the
// service's response.
func (f *integrationFixture) watch(onCall func(kind string) int) {
	target := "/" + api.IntegrationID("refs/heads/v2")
	var mu sync.Mutex
	f.intercept = func(w http.ResponseWriter, r *http.Request, next http.Handler) bool {
		kind := ""
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, target+"/observation"):
			kind = "observation"
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, target):
			kind = "integration"
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, target+"/renew"):
			kind = "renew"
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, target):
			data, err := io.ReadAll(r.Body)
			if err != nil {
				f.t.Error(err)
				return false
			}
			// Hand the same body to the service once the command is read.
			r.Body = io.NopCloser(bytes.NewReader(data))
			var command api.IntegrationCommand
			if err := json.Unmarshal(data, &command); err != nil {
				f.t.Error(err)
				return false
			}
			kind = "action:" + command.Action
		default:
			return false
		}
		mu.Lock()
		status := onCall(kind)
		mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return true
		}
		return false
	}
}

// foreignPush lands one more foreign commit on v2 from the second clone, the
// way a user pushing the target directly does. It runs inside service calls, so
// it reports through Error and never stops the test goroutine.
func (f *integrationFixture) foreignPush(name string) {
	f.t.Helper()
	run := func(args ...string) {
		cmd := exec.CommandContext(f.t.Context(), "git", append([]string{"-C", f.second}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
		if out, err := cmd.CombinedOutput(); err != nil {
			f.t.Errorf("Git %v: %s: %v", args, out, err)
		}
	}
	run("fetch", "-q", "origin")
	run("checkout", "-q", "-B", "hook", "origin/v2")
	if err := os.WriteFile(filepath.Join(f.second, name), []byte("foreign "+name+"\n"), 0600); err != nil {
		f.t.Error(err)
	}
	run("add", name)
	run("commit", "-m", "Foreign "+name)
	run("push", "origin", "HEAD:refs/heads/v2")
}

func TestIntegrationMergesADivergedTargetWithoutAVia(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	foreign := f.advanceTarget("other.txt")
	before := f.localState()
	result, err := f.a.Integrate(t.Context(), f.first, f.options("auto-merge"))
	if err != nil {
		t.Fatal(err)
	}
	request, merged := result.Request, f.targetHead()
	if request.Containment != api.IntegrationVia || request.ViaCommit != merged || request.AlreadyContained || !request.ViaAutoMerge || request.ViaSource != "" || request.Commit != f.commit || result.ObservedTarget != merged {
		t.Fatalf("receipt: %+v", result)
	}
	if got := f.parents(merged); len(got) != 2 || got[0] != foreign || got[1] != f.commit {
		t.Fatalf("parents of the merge: %v, want target %s then reviewed %s", got, foreign, f.commit)
	}
	want := strings.Fields(deliveryGit(t, f.first, "merge-tree", "--write-tree", foreign, f.commit))[0]
	if got := deliveryGit(t, f.first, "rev-parse", merged+"^{tree}"); got != want {
		t.Fatalf("tree of the merge %s, want the merge-tree result %s", got, want)
	}
	message := deliveryGit(t, f.first, "log", "-1", "--format=%B", merged)
	for _, part := range []string{"v2", f.commit, "auto-merge"} {
		if !strings.Contains(message, part) {
			t.Fatalf("merge message lacks %q: %s", part, message)
		}
	}
	if got := deliveryGit(t, f.first, "log", "-1", "--format=%an <%ae>|%cn <%ce>", merged); got != "Merge Author <merge@example.invalid>|Merge Author <merge@example.invalid>" {
		t.Fatalf("identity of the merge: %s", got)
	}
	if after := f.localState(); after != before {
		t.Fatalf("the merge touched the work tree:\nbefore %s\nafter  %s", before, after)
	}
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("private ref left behind: %s", refs)
	}
	shown, err := f.b.Project.Integration(t.Context(), api.IntegrationID("refs/heads/v2"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(shown)
	if !strings.Contains(string(data), `"via_auto_merge":true`) || strings.Contains(string(data), "via_source") {
		t.Fatalf("receipt JSON: %s", data)
	}
	f.complete(result)
}

func TestIntegrationMergesADivergedGroupTarget(t *testing.T) {
	f, ids, owners, options, tip := linearReviewedGroup(t)
	f.mergeIdentity()
	foreign := f.advanceTarget("other.txt")
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	merged := f.targetHead()
	if result.Request.Containment != api.IntegrationVia || !result.Request.ViaAutoMerge || result.Request.ViaCommit != merged || result.Request.Commit != tip {
		t.Fatalf("receipt: %+v", result.Request)
	}
	if got := f.parents(merged); len(got) != 2 || got[0] != foreign || got[1] != tip {
		t.Fatalf("parents of the merge: %v", got)
	}
	f.verifyGroupReceipts(ids, result)
	f.completeGroupMembers(ids, owners)
}

// carrierAbove commits on top of the reviewed commit and publishes it as a
// branch of the common remote, the way a caller's own carrier is.
func (f *integrationFixture) carrierAbove(name string) string {
	f.t.Helper()
	deliveryGit(f.t, f.first, "checkout", "-q", "-b", "carrier-"+name, f.commit)
	carrier := commitFile(f.t, f.first, name, "carrier work\n", "Carrier "+name)
	deliveryGit(f.t, f.first, "push", "origin", carrier+":refs/heads/carrier-"+name)
	deliveryGit(f.t, f.first, "checkout", "-q", "-")
	return carrier
}

func TestIntegrationMergesTheTargetThatPassedTheCallersVia(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	carrier := f.carrierAbove("carrier.txt")
	foreign := f.advanceTarget("other.txt")
	options := f.options("via-passed")
	options.ViaCommit = carrier
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	merged, request := f.targetHead(), result.Request
	if request.Containment != api.IntegrationVia || request.ViaCommit != merged || request.AlreadyContained || !request.ViaAutoMerge || request.ViaSource != carrier {
		t.Fatalf("receipt: %+v", request)
	}
	if got := f.parents(merged); len(got) != 2 || got[0] != foreign || got[1] != carrier {
		t.Fatalf("parents of the merge: %v, want target %s then carrier %s", got, foreign, carrier)
	}
	if message := deliveryGit(t, f.first, "log", "-1", "--format=%B", merged); !strings.Contains(message, carrier) || !strings.Contains(message, f.commit) {
		t.Fatalf("merge message names neither the carrier nor the reviewed commit: %s", message)
	}
	if len(request.Edges) < 3 || !containsEdge(request.Edges, api.IntegrationEdge{Ancestor: carrier, Descendant: merged}) {
		t.Fatalf("edges lack the carrier: %+v", request.Edges)
	}
	f.complete(result)
}

func containsEdge(edges []api.IntegrationEdge, edge api.IntegrationEdge) bool {
	for _, candidate := range edges {
		if candidate == edge {
			return true
		}
	}
	return false
}

func TestIntegrationTargetThatContainsTheCallersViaPushesNothing(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	carrier := f.carrierAbove("carrier.txt")
	deliveryGit(t, f.first, "push", "origin", carrier+":refs/heads/v2")
	ahead := f.advanceTarget("ahead.txt")
	options := f.options("via-behind-target")
	options.ViaCommit = carrier
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	request := result.Request
	if request.Containment != api.IntegrationVia || request.ViaCommit != ahead || !request.AlreadyContained || request.ViaAutoMerge || request.ViaSource != carrier || result.ObservedTarget != ahead {
		t.Fatalf("receipt: %+v", result)
	}
	if got := f.targetHead(); got != ahead {
		t.Fatalf("the target moved to %s", got)
	}
	f.complete(result)
}

func TestIntegrationMergeConflictIsRefusedBeforeAnyClaim(t *testing.T) {
	cases := map[string]bool{"without a via": false, "with a via passed by the target": true}
	for name, withVia := range cases {
		t.Run(name, func(t *testing.T) {
			f := newIntegrationFixture(t)
			f.mergeIdentity()
			options := f.options("merge-conflict")
			if withVia {
				options.ViaCommit = f.carrierAbove("carrier.txt")
			}
			// The foreign commit rewrites the line the reviewed commit changed.
			deliveryGit(t, f.second, "fetch", "-q", "origin")
			deliveryGit(t, f.second, "checkout", "-q", "-B", "advance", "origin/v2")
			if err := os.WriteFile(filepath.Join(f.second, "work.txt"), []byte("foreign line\n"), 0600); err != nil {
				t.Fatal(err)
			}
			deliveryGit(t, f.second, "commit", "-am", "Foreign line")
			deliveryGit(t, f.second, "push", "origin", "HEAD:refs/heads/v2")
			foreign := f.targetHead()
			before := f.localState()
			_, err := f.a.Integrate(t.Context(), f.first, options)
			if err == nil || !strings.Contains(err.Error(), "work.txt") || !strings.Contains(err.Error(), "via_commit") {
				t.Fatalf("a conflict must name its path and the via_commit way out: %v", err)
			}
			if got := f.targetHead(); got != foreign {
				t.Fatal("the refused merge moved the target")
			}
			f.noClaim()
			if refs := f.privateRefs(); refs != "" {
				t.Fatalf("private ref left behind: %s", refs)
			}
			if after := f.localState(); after != before {
				t.Fatal("the refused merge touched the work tree")
			}
		})
	}
}

func TestIntegrationMergeConflictListsAtMostTenPaths(t *testing.T) {
	f := newIntegrationGitFixture(t)
	f.mergeIdentity()
	for i := range 12 {
		name := "p" + string(rune('a'+i)) + ".txt"
		if err := os.WriteFile(filepath.Join(f.first, name), []byte("reviewed\n"), 0600); err != nil {
			t.Fatal(err)
		}
		deliveryGit(t, f.first, "add", name)
		if err := os.WriteFile(filepath.Join(f.second, name), []byte("foreign\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	deliveryGit(t, f.first, "commit", "-m", "Twelve reviewed files")
	f.commit = deliveryGit(t, f.first, "rev-parse", "HEAD")
	f.reviewedTask()
	deliveryGit(t, f.second, "add", ".")
	deliveryGit(t, f.second, "commit", "-m", "Twelve foreign files")
	deliveryGit(t, f.second, "push", "origin", "HEAD:refs/heads/v2")
	_, err := f.a.Integrate(t.Context(), f.first, f.options("many-conflicts"))
	if err == nil || !strings.Contains(err.Error(), "12 path") || !strings.Contains(err.Error(), "pa.txt") || strings.Contains(err.Error(), "pl.txt") {
		t.Fatalf("a long conflict list must give the total and cut after ten: %v", err)
	}
	f.noClaim()
}

// gitWrapper puts a fake git first on PATH. The script runs with the real
// git's path in KANDER_TEST_REAL_GIT.
func (f *integrationFixture) gitWrapper(script string) {
	f.t.Helper()
	if runtime.GOOS == "windows" {
		f.t.Skip("POSIX Git wrapper fixture")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		f.t.Fatal(err)
	}
	wrappers := filepath.Join(f.root, "bin")
	if err := os.MkdirAll(wrappers, 0700); err != nil {
		f.t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wrappers, "git"), []byte("#!/bin/sh\n"+script), 0700); err != nil {
		f.t.Fatal(err)
	}
	f.t.Setenv("KANDER_TEST_REAL_GIT", git)
	f.t.Setenv("PATH", wrappers+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestIntegrationMergeNeedsGitWithMergeTreeWriteTree(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	foreign := f.advanceTarget("other.txt")
	f.gitWrapper(`for a do case "$a" in merge-tree) exit 129;; esac; done
exec "$KANDER_TEST_REAL_GIT" "$@"
`)
	_, err := f.a.Integrate(t.Context(), f.first, f.options("old-git"))
	if err == nil || !strings.Contains(err.Error(), "2.38") || !strings.Contains(err.Error(), "via_commit") {
		t.Fatalf("an old Git must name the version and the via_commit way out: %v", err)
	}
	if got := f.targetHead(); got != foreign {
		t.Fatal("the refused merge moved the target")
	}
	f.noClaim()
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("private ref left behind: %s", refs)
	}
}

func TestIntegrationMergeNeedsAConfiguredGitIdentity(t *testing.T) {
	f := newIntegrationFixture(t)
	f.isolateGit()
	foreign := f.advanceTarget("other.txt")
	_, err := f.a.Integrate(t.Context(), f.first, f.options("no-identity"))
	if err == nil || !strings.Contains(err.Error(), "user.name") || !strings.Contains(err.Error(), "via_commit") {
		t.Fatalf("a missing identity must be named: %v", err)
	}
	if got := f.targetHead(); got != foreign {
		t.Fatal("the refused merge moved the target")
	}
	f.noClaim()
}

func TestIntegrationDeletesTheMergeRefWhenPreparationFailsAfterTheMerge(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	foreign := f.advanceTarget("other.txt")
	f.watch(func(kind string) int {
		if kind == "observation" {
			return http.StatusServiceUnavailable
		}
		return 0
	})
	if _, err := f.a.Integrate(t.Context(), f.first, f.options("prepare-fails")); err == nil {
		t.Fatal("the failed observation was not reported")
	}
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("a failed preparation kept the merge ref: %s", refs)
	}
	if got := f.targetHead(); got != foreign {
		t.Fatal("the failed preparation moved the target")
	}
}

// interruptBeforePush stops the first push intent of the operation, so the
// attempt holds a generated merge that nothing has pushed.
func (f *integrationFixture) interruptBeforePush() {
	interrupted := false
	f.watch(func(kind string) int {
		if kind == "action:pushing" && !interrupted {
			interrupted = true
			return http.StatusServiceUnavailable
		}
		return 0
	})
}

func TestIntegrationRetryReusesTheMergeOfAnInterruptedAttempt(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	f.advanceTarget("other.txt")
	f.interruptBeforePush()
	options := f.options("interrupted-merge")
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
		t.Fatal("the interrupted push intent was not reported")
	}
	first := f.attempts(options.OperationID)
	if len(first) != 1 || !first[0].Request.ViaAutoMerge || !strings.Contains(f.privateRefs(), first[0].Request.ViaCommit) {
		t.Fatalf("the interrupted attempt must keep its merge ref: %+v %s", first, f.privateRefs())
	}
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.ViaCommit != first[0].Request.ViaCommit || f.targetHead() != first[0].Request.ViaCommit || len(f.attempts(options.OperationID)) != 1 {
		t.Fatalf("the retry did not reuse the merge: %+v", result.Request)
	}
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("private ref left behind after completion: %s", refs)
	}
	f.complete(result)
}

func TestIntegrationRetryStartsOverWhenTheMergeRefIsLost(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	f.advanceTarget("other.txt")
	f.interruptBeforePush()
	options := f.options("lost-merge-ref")
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
		t.Fatal("the interrupted push intent was not reported")
	}
	first := f.attempts(options.OperationID)
	deliveryGit(t, f.first, "update-ref", "-d", "refs/kander/auto-merge/"+first[0].Request.ExecutionID)
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	attempts := f.attempts(options.OperationID)
	if len(attempts) != 2 || !attempts[0].Retired || attempts[0].RetiredFor != "auto_merge_lost" || attempts[1].Request.ExecutionID == attempts[0].Request.ExecutionID {
		t.Fatalf("attempts: %+v", attempts)
	}
	if result.Request.ExecutionID != attempts[1].Request.ExecutionID || f.targetHead() != result.Request.ViaCommit {
		t.Fatalf("receipt: %+v", result.Request)
	}
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("private ref left behind after completion: %s", refs)
	}
	f.complete(result)
}

// dropIntegrationCredentials removes the private credentials of the host, which
// is what a restarted command finds when the process that held the lease is gone.
func (f *integrationFixture) dropIntegrationCredentials() {
	f.t.Helper()
	credentials, err := remote.Credentials(f.a.Home)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, credential := range credentials {
		if credential.ResourceKind == "integration" {
			if err := remote.RemoveCredential(f.a.Home, credential); err != nil {
				f.t.Fatal(err)
			}
		}
	}
}

func TestIntegrationRestartedCommandReusesTheMergeOfTheInterruptedOne(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	f.advanceTarget("other.txt")
	t.Setenv("GIT_COMMITTER_DATE", "2030-01-01T00:00:00Z")
	t.Setenv("GIT_AUTHOR_DATE", "2030-01-01T00:00:00Z")
	f.interruptBeforePush()
	options := f.options("restarted-command")
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
		t.Fatal("the interrupted push intent was not reported")
	}
	f.dropIntegrationCredentials()
	// A new merge of the same inputs would carry another time stamp.
	t.Setenv("GIT_COMMITTER_DATE", "2031-01-01T00:00:00Z")
	t.Setenv("GIT_AUTHOR_DATE", "2031-01-01T00:00:00Z")
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	attempts := f.attempts(options.OperationID)
	if len(attempts) != 2 || attempts[0].RetiredFor != "identity_changed" || attempts[1].Request.ExecutionID == attempts[0].Request.ExecutionID {
		t.Fatalf("attempts: %+v", attempts)
	}
	if result.Request.ViaCommit != attempts[0].Request.ViaCommit || f.targetHead() != attempts[0].Request.ViaCommit {
		t.Fatalf("a restarted command made a new merge: %s, first %s", result.Request.ViaCommit, attempts[0].Request.ViaCommit)
	}
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("private ref left behind: %s", refs)
	}
	f.complete(result)
}

func TestIntegrationRestartedCommandMergesAgainWhenTheTargetMoved(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	f.advanceTarget("other.txt")
	f.interruptBeforePush()
	options := f.options("restarted-after-move")
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
		t.Fatal("the interrupted push intent was not reported")
	}
	first := f.attempts(options.OperationID)[0].Request
	f.dropIntegrationCredentials()
	moved := f.advanceTarget("moved.txt")
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	if result.Request.ViaCommit == first.ViaCommit {
		t.Fatal("a merge of the old target was reused")
	}
	if got := f.parents(result.Request.ViaCommit); len(got) != 2 || got[0] != moved {
		t.Fatalf("the new merge does not start from the moved target: %v", got)
	}
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("private ref left behind: %s", refs)
	}
	f.complete(result)
}

func TestIntegrationRetryStartsOverWhenTheMergeAndItsRefAreBothGone(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	f.advanceTarget("other.txt")
	f.interruptBeforePush()
	options := f.options("lost-merge-object")
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
		t.Fatal("the interrupted push intent was not reported")
	}
	lost := f.attempts(options.OperationID)[0].Request
	deliveryGit(t, f.first, "update-ref", "-d", "refs/kander/auto-merge/"+lost.ExecutionID)
	deliveryGit(t, f.first, "reflog", "expire", "--expire=now", "--all")
	deliveryGit(t, f.first, "prune", "--expire=now")
	if exec.CommandContext(t.Context(), "git", "-C", f.first, "cat-file", "-e", lost.ViaCommit).Run() == nil {
		t.Fatal("the merge object survived the prune")
	}
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	attempts := f.attempts(options.OperationID)
	if len(attempts) != 2 || attempts[0].RetiredFor != "auto_merge_lost" || f.targetHead() != result.Request.ViaCommit {
		t.Fatalf("attempts: %+v", attempts)
	}
	f.complete(result)
}

func TestIntegrationCancelledCommandStillDeletesItsMergeRef(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	f.advanceTarget("other.txt")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	f.watch(func(kind string) int {
		if kind == "observation" {
			cancel()
		}
		return 0
	})
	if _, err := f.a.Integrate(ctx, f.first, f.options("cancelled-preparation")); err == nil {
		t.Fatal("a cancelled command succeeded")
	}
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("a cancelled preparation kept the merge ref: %s", refs)
	}
}

func TestIntegrationReplayedCompletionFinishesAnInterruptedCleanup(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	f.advanceTarget("other.txt")
	options := f.options("cleanup-replay")
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	// A completion saved its result and stopped before the cleanup finished.
	deliveryGit(t, f.first, "update-ref", "refs/kander/auto-merge/"+result.Request.ExecutionID, result.Request.ViaCommit)
	if _, err := f.a.Integrate(t.Context(), f.first, options); err != nil {
		t.Fatal(err)
	}
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("the replayed completion kept the merge ref: %s", refs)
	}
}

func TestIntegrationFailedDeletionOfTheCarriedRefLeavesNoRefAfterCompletion(t *testing.T) {
	f := newIntegrationFixture(t)
	f.mergeIdentity()
	f.advanceTarget("other.txt")
	f.interruptBeforePush()
	options := f.options("carried-ref-stuck")
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
		t.Fatal("the interrupted push intent was not reported")
	}
	f.dropIntegrationCredentials()
	// The first deletion of a merge ref fails, as it does with a stale lock file.
	f.gitWrapper(`case "$*" in *"update-ref -d refs/kander/auto-merge/"*)
 if [ ! -e "$KANDER_TEST_FAILED_ONCE" ]; then : > "$KANDER_TEST_FAILED_ONCE"; exit 1; fi;;
esac
exec "$KANDER_TEST_REAL_GIT" "$@"
`)
	t.Setenv("KANDER_TEST_FAILED_ONCE", filepath.Join(f.root, "failed-once"))
	if _, err := f.a.Integrate(t.Context(), f.first, options); err == nil {
		t.Fatal("the failed deletion was not reported")
	}
	result, err := f.a.Integrate(t.Context(), f.first, options)
	if err != nil {
		t.Fatal(err)
	}
	if refs := f.privateRefs(); refs != "" {
		t.Fatalf("a stuck deletion left a ref after completion: %s", refs)
	}
	f.complete(result)
}
