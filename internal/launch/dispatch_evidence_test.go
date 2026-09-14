package launch

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/board"
)

func integrationGit(t *testing.T) (string, string) {
	t.Helper()
	cwd := t.TempDir()
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", cwd}, args...)...)
		b, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("git %v: %s %v", args, b, e)
		}
		return strings.TrimSpace(string(b))
	}
	git("init", "-b", "develop")
	git("-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "fixture")
	return cwd, git("rev-parse", "HEAD")
}

func TestDispatchIntegrationUsesActualGitAncestry(t *testing.T) {
	cwd, head := integrationGit(t)
	g := board.DispatchIntegration{CWD: cwd, SourceCommit: head, ReviewTarget: head, ReviewBase: head, TargetCommit: head, TargetRef: "refs/heads/develop"}
	if err := verifyDispatchIntegration(context.Background(), g); err != nil {
		t.Fatal(err)
	}
	other, foreign := integrationGit(t)
	cmd := exec.Command("git", "-C", other, "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "commit", "--allow-empty", "-m", "unrelated")
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatal(string(data), err)
	}
	data, err := exec.Command("git", "-C", other, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	foreign = strings.TrimSpace(string(data))
	g.SourceCommit = foreign
	if err = verifyDispatchIntegration(context.Background(), g); err == nil {
		t.Fatal("foreign source claimed as integrated")
	}
	g.SourceCommit = head
	g.TargetRef = "refs/heads/task"
	if err = verifyDispatchIntegration(context.Background(), g); err == nil {
		t.Fatal("task ref substituted for develop")
	}
	g.TargetRef = "refs/heads/develop"
	g.TargetCommit = strings.Repeat("a", 40)
	if err = verifyDispatchIntegration(context.Background(), g); err == nil {
		t.Fatal("unavailable target claimed as integrated")
	}
}

func launchWrapFixture(t *testing.T) (string, string, string, board.DispatchInput) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX fake terminal observation; Git-only test runs natively")
	}
	root, _, bin := setupBoard(t)
	task, path := makeTodo(t, root, "wrap-binding")
	startThenReview(t, root, "claude", task, path)
	cwd, head := integrationGit(t)
	roles := map[string]string{"PM": "N/A: lifecycle fixture", "QA": "N/A: lifecycle fixture", "CSA": "N/A: fixture", "Hacker": "N/A: fixture"}
	plan := board.ReviewPlan{Schema: 1, Sealed: true, PlanID: "wrap-plan", Author: "fixture", Basis: "lifecycle fixture", CWD: cwd, ReportLanguage: "zh-CN", TaskIDs: []string{task}, Batches: []board.ReviewPlanBatch{{BatchID: "wrap-batch", TaskIDs: []string{task}, Base: head, TargetCommit: head, Requirements: roles}}}
	if err := board.CreateReviewPlan(root, plan); err != nil {
		t.Fatal(err)
	}
	view, err := board.ReadReviewBatchView(root, "wrap-batch")
	if err != nil {
		t.Fatal(err)
	}
	request := board.ReviewCloseRequest{BatchID: "wrap-batch", ExpectedRevision: view.Batch.Revision, ViewHash: board.ReviewViewDigest(view), Author: "fixture", Roles: map[string]board.ReviewRoleConclusion{}}
	edges, _, err := board.ReviewClosureEdges(view, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = board.CloseReviewBatch(root, request, board.ReviewGitEvidence{CWD: cwd, Head: head, Edges: edges, VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		t.Fatal(err)
	}
	in := board.DispatchInput{ID: "launch-wrap", TaskID: task, Kind: "wrap-up", Message: "只清理和记录", Base: head, Evidence: board.DispatchEvidence{WrapUp: &board.DispatchWrapUpBinding{Git: board.DispatchIntegration{CWD: cwd, SourceCommit: head, ReviewTarget: head, ReviewBase: head, TargetCommit: head, TargetRef: "refs/heads/develop", Author: "coordinator", Basis: "本地 develop 实际祖先验证"}}}}
	return root, task, bin, in
}

func TestDispatchWrapUpPublicAuthorizationReconcilesAndObserves(t *testing.T) {
	for _, mode := range []string{"no-session", "unknown", "alive", "stopped"} {
		t.Run(mode, func(t *testing.T) {
			root, task, bin, in := launchWrapFixture(t)
			s, err := board.ReadSnapshot(root, task)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "no-session" {
				next := strings.ReplaceAll(s.Text, "- SESSION: "+board.MetadataFrom(s.Text, board.FieldSession), "- SESSION:")
				if err = board.WriteManagedDocument(root, s.Entry, next); err != nil {
					t.Fatal(err)
				}
			}
			script := "#!/bin/sh\n"
			switch mode {
			case "stopped", "no-session":
				script += "case \"$1\" in\n display-message) printf 'claude\\t0\\t1\\n';;\n show-options) printf '%s\\n' '" + board.MetadataFrom(s.Text, board.FieldSession) + "';;\n list-panes) printf '%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\t%s\\n' '%8' '$8' 'other' '@8' 'sh' '0' 'other-session' '';;\n *) exit 1;;\nesac\n"
			case "unknown":
				script += "echo observation-failed >&2\nexit 1\n"
			case "alive":
				// A valid live pane cannot be granted to an on-behalf author.
				script += "case \"$1\" in\n display-message) printf 'claude\\t0\\t0\\n';;\n show-options) printf '%s\\n' '" + board.MetadataFrom(s.Text, board.FieldSession) + "';;\n *) exit 1;;\nesac\n"
			}
			if err = os.WriteFile(filepath.Join(bin, "tmux"), []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			r := WrapUpRequest{TaskID: task, DispatchID: in.ID, Intent: &in, Author: "coordinator", Reason: "原执行者退出后代收尾"}
			d, err := AuthorizeWrapUp(context.Background(), root, r)
			if mode != "stopped" {
				if err == nil {
					t.Fatal("unproven exit granted", mode, d)
				}
				stored, e := board.ReadDispatch(root, task, in.ID)
				if e != nil {
					t.Fatal("missing durable intent", e)
				}
				if stored.WrapUpAuthority != nil {
					t.Fatal("failed observation changed authority")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if d.WrapUpAuthority == nil || d.Authorization.Epoch != 2 {
				t.Fatal(d)
			}
			fresh, e := board.ReadSnapshot(root, task)
			if e != nil {
				t.Fatal(e)
			}
			// Same-ID recovery remains usable after cleanup removes the Git workspace.
			if e = os.RemoveAll(in.Evidence.WrapUp.Git.CWD); e != nil {
				t.Fatal(e)
			}
			replay, e := AuthorizeWrapUp(context.Background(), root, r)
			if e != nil {
				t.Fatal(e)
			}
			if replay.Authorization != d.Authorization {
				t.Fatal("retry issued another grant")
			}
			after, e := board.ReadSnapshot(root, task)
			if e != nil {
				t.Fatal(e)
			}
			if fresh.Revision != after.Revision {
				t.Fatal("retry wrote card")
			}
		})
	}
}

func TestDispatchEvidenceFlagsAndRejectedFixHaveNoIntent(t *testing.T) {
	root, _, _ := setupBoard(t)
	task, path := makeTodo(t, root, "missing-fix")
	startThenReview(t, root, "claude", task, path)
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "evidence.json")
	data, _ := json.Marshal(board.DispatchEvidence{Fix: &board.DispatchFixBinding{BatchID: "missing", Findings: []board.DispatchFindingReference{{FindingRef: board.FindingRef{RunID: "missing", FindingID: "PM-01"}}}}})
	if err = os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	rest, options, err := ParseDispatchOptions([]string{task, "--evidence-file", file, "--kind", "fix", "--base", strings.Repeat("a", 40), "--dispatch-id", "bad-fix"})
	if err != nil || len(rest) != 1 || options.EvidenceFile != file {
		t.Fatal(rest, options, err)
	}
	_, _, err = capture(t, func() error { _, _, e := PrepareAction(root, s, "修复", options, 61); return e })
	if err == nil {
		t.Fatal("missing review accepted")
	}
	after, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != s.Revision {
		t.Fatal("invalid fix changed card")
	}
}

func TestDispatchExplicitNewIDPreparesAndReusesIntent(t *testing.T) {
	root, _, _ := setupBoard(t)
	task, path := makeTodo(t, root, "explicit-dispatch")
	startThenReview(t, root, "claude", task, path)
	s, err := board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	options := DispatchOptions{ID: "explicit-new", Kind: "sync", Base: strings.Repeat("a", 40)}
	var first, second board.Dispatch
	_, _, err = capture(t, func() error { var e error; first, _, e = PrepareAction(root, s, "同步", options, 61); return e })
	if err != nil {
		t.Fatal(err)
	}
	s, err = board.ReadSnapshot(root, task)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = capture(t, func() error { var e error; second, _, e = PrepareAction(root, s, "同步", options, 61); return e })
	if err != nil {
		t.Fatal(err)
	}
	if first.Input.ID != options.ID || second.Revision != first.Revision || !first.Input.ConfirmBy.Equal(second.Input.ConfirmBy) {
		t.Fatal("explicit same-ID preparation changed intent")
	}
}
