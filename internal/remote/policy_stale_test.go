package remote_test

import (
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/remote"
)

// bumpPolicy writes the policy again unchanged, which moves its revision.
func bumpPolicy(t *testing.T, url string) {
	t.Helper()
	c, err := client.New(url, "policy-bumper")
	if err != nil {
		t.Error(err)
		return
	}
	info, err := c.Register(t.Context(), "golf", "")
	if err != nil {
		t.Error(err)
		return
	}
	p := c.Project(info.ID)
	current, err := p.Policy(t.Context())
	if err == nil {
		_, err = p.PutPolicy(t.Context(), client.NewKey(), api.PolicyWrite{ExpectedRevision: current.Revision, Values: current.Values})
	}
	if err != nil {
		t.Error(err)
	}
}

// todoCard creates a ready card through the session.
func todoCard(t *testing.T, session *remote.Session, slug string) string {
	t.Helper()
	task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: "Claim " + slug, Language: "en"})
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\n", 1)
	if task, err = session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = session.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
		t.Fatal(err)
	}
	return task.TaskID
}

// A claim the server refuses as stale reads the policy again, with the cache,
// and is sent once more; a second stale refusal ends the command with the
// server's revision and kander policy show.
func TestClaimReadsAStalePolicyAgainAndRetriesOnce(t *testing.T) {
	var bumps, limit atomic.Int32
	var url string
	srv := startPolicyServer(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// The policy changes between the command's read and its claim.
			if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/claim") && bumps.Load() < limit.Load() {
				bumps.Add(1)
				bumpPolicy(t, url)
			}
			next.ServeHTTP(w, r)
		})
	})
	url = srv.url()
	a := newServiceClient(t, url, nil)
	a.use(t)
	if code, _, errOut := runService(remote.NewPolicyCommand, "init", "--yes"); code != 0 {
		t.Fatalf("init: %d %s", code, errOut)
	}
	session := openSession(t)
	first, second := todoCard(t, session, "stale-once"), todoCard(t, session, "stale-twice")

	limit.Store(1)
	if code, _, errOut := runService(remote.NewMoveCommand, first, "working", "--owner", "claude"); code != 0 {
		t.Fatalf("claim after one stale refusal: %d %s", code, errOut)
	}
	shown, err := session.Project.Show(t.Context(), first)
	if err != nil || shown.Claim == nil || shown.Claim.BranchSnapshot == nil || shown.Claim.BranchSnapshot.PolicyRevision != 2 {
		t.Fatalf("claim: %+v %v", shown.Claim, err)
	}
	if revision := loadedConfig(t).Policy.Status().Revision; revision != 2 {
		t.Fatalf("the refresh did not reach the cache: revision %d", revision)
	}

	limit.Store(bumps.Load() + 2)
	code, _, errOut := runService(remote.NewMoveCommand, second, "working", "--owner", "claude")
	if code == 0 || !strings.Contains(errOut, "revision 4") || !strings.Contains(errOut, "kander policy show") {
		t.Fatalf("second stale refusal: %d %q", code, errOut)
	}
	if shown, err := session.Project.Show(t.Context(), second); err != nil || shown.State != "todo" {
		t.Fatalf("card after two stale refusals: %+v %v", shown, err)
	}
}
