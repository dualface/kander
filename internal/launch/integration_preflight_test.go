package launch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/command/commandtest"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

func TestDistributedIntegrationPreflight(t *testing.T) {
	dir, _ := integrationGit(t)
	_, session, _, forbidden, _ := remoteLifecycleFixture(t, "preflight-neighbor")
	task := preflightTask(t, session)
	git := func(args ...string) string {
		t.Helper()
		return gitIn(t, dir, append([]string{"-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid"}, args...)...)
	}
	commit := func(content, message string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "file"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		git("add", "file")
		git("commit", "-qm", message)
		return git("rev-parse", "HEAD")
	}
	text := "one\ntwo\nthree\nfour\nfive\nsix\nseven\neight\n"
	base := commit(text, "base")
	git("switch", "-qc", "group/topic")
	reviewed := commit(strings.Replace(text, "five", "FIVE", 1), "delivery")
	closeIntegrationReview(t, session.Info.BoardPath, task.TaskID, "en", dir, base, reviewed)
	task, err := session.Project.Show(t.Context(), task.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	git("switch", "-qc", "v2", base)
	upstream := commit(strings.Replace(text, "seven", "SEVEN", 1), "upstream")
	git("switch", "-qc", "candidate", "v2")
	git("merge", "--no-edit", "group/topic")
	candidate := git("rev-parse", "HEAD")

	g := board.DispatchIntegration{CWD: dir, SourceCommit: reviewed, TargetCommit: candidate, TargetRef: "refs/heads/v2"}
	input := board.DispatchInput{ID: "preflight-only", TaskID: task.TaskID, Kind: "check-integration", Evidence: board.DispatchEvidence{WrapUp: &board.DispatchWrapUpBinding{Git: g}}}
	file := filepath.Join(t.TempDir(), "preflight.json")
	run := func(request board.DispatchInput) (int, string, string) {
		t.Helper()
		raw, err := json.Marshal(request)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, raw, 0o600); err != nil {
			t.Fatal(err)
		}
		return commandtest.Capture(func() int { return RunDispatchRemote([]string{"prepare", file, "--json"}) })
	}
	withGit := func(git board.DispatchIntegration) board.DispatchInput {
		request := input
		request.Evidence.WrapUp = &board.DispatchWrapUpBinding{Git: git}
		return request
	}
	foreignRef := g
	foreignRef.TargetRef = "refs/heads/develop"
	wrongBase := g
	wrongBase.ReviewBase = upstream
	wrongTarget := g
	wrongTarget.ReviewTarget = candidate
	stale := g
	stale.TargetCommit = reviewed
	for _, rejection := range []struct {
		name    string
		request board.DispatchInput
		want    string
	}{
		{"missing Git evidence", withGit(board.DispatchIntegration{}), "wrap_up.git"},
		{"wrong review base", withGit(wrongBase), config.Text("launch.integration_preflight_range")},
		{"wrong review target", withGit(wrongTarget), config.Text("launch.integration_preflight_range")},
		{"another branch", withGit(foreignRef), "target ref"},
		{"candidate misses current tip", withGit(stale), "git merge-base --is-ancestor " + upstream + " " + reviewed},
	} {
		t.Run(rejection.name, func(t *testing.T) {
			code, out, stderr := run(rejection.request)
			if code != 1 || out != "" || !strings.Contains(stderr, rejection.want) {
				t.Fatalf("preflight: %d %q %q, want %q", code, out, stderr, rejection.want)
			}
		})
	}

	git("switch", "-qc", "replayed", "group/topic")
	git("rebase", "v2")
	replayed := g
	replayed.SourceCommit = git("rev-parse", "HEAD")
	replayed.TargetCommit, replayed.RebasedBase = replayed.SourceCommit, upstream
	if code, out, stderr := run(withGit(replayed)); code != 1 || out != "" || stderr == "" {
		t.Fatalf("rebase that changed patch context: %d %q %q", code, out, stderr)
	}

	clientHome := filepath.Join(t.TempDir(), "readonly-client")
	t.Setenv(remote.EnvClientHome, clientHome)
	code, out, stderr := run(input)
	want, err := json.Marshal(map[string]string{"task_id": task.TaskID, "review_base": base, "review_target": reviewed, "source_commit": reviewed, "target_commit": candidate, "target_ref": g.TargetRef})
	if code != 0 || out != string(want)+"\n" || stderr != "" || err != nil {
		t.Fatalf("preflight: %d %q %q, want %s (%v)", code, out, stderr, want, err)
	}
	if head := git("rev-parse", "v2"); head != upstream {
		t.Fatalf("preflight moved v2 to %s", head)
	}
	for _, path := range []string{forbidden, clientHome} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("preflight touched %s: %v", path, err)
		}
	}
	current, err := session.Project.Show(t.Context(), task.TaskID)
	if err != nil || current.Revision != task.Revision || current.State != task.State {
		t.Fatalf("preflight changed the card: %+v, %v", current, err)
	}
	if requests, err := session.Project.Requests(t.Context(), task.TaskID); err != nil || len(requests) != 0 {
		t.Fatalf("preflight left proposals: %+v, %v", requests, err)
	}
	if _, err := session.Project.Dispatch(t.Context(), task.TaskID, input.ID); !api.IsCode(err, api.CodeNotFound) {
		t.Fatalf("preflight left a dispatch: %v", err)
	}

	g.ReviewBase, g.ReviewTarget = base, reviewed
	if err := verifyIntegrationTarget(t.Context(), g, "v2"); err == nil || !strings.Contains(err.Error(), "git merge-base --is-ancestor "+candidate+" "+upstream) {
		t.Fatalf("wrap-up before publication: %v", err)
	}
	git("update-ref", g.TargetRef, candidate, upstream)
	if err := verifyIntegrationTarget(t.Context(), g, "v2"); err != nil {
		t.Fatalf("wrap-up after publication: %v", err)
	}
}

func preflightTask(t *testing.T, session *remote.Session) api.Task {
	t.Helper()
	project := session.Project
	task, err := project.NewTask(t.Context(), "", api.NewTask{Type: "feature", Slug: "preflight-target", Title: "Check integration before publication", Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: candidate\n- INTEGRATION_TARGET: v2", 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if _, err := project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	grant, err := project.Claim(t.Context(), "", task.TaskID, api.ClaimRequest{ClientID: session.Client.ClientID(), ExecutionID: "preflight-fixture", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	return grant.Task
}
