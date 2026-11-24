package launch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
)

// wrapUpFixture is a wrap-up fact of a two-member group integrated into v2,
// with the service records served by a stub and the Git facts held by a real
// origin. The checkout runs the verification.
type wrapUpFixture struct {
	t                  *testing.T
	bare, checkout     string
	base, source       string
	members            []string
	session            *remote.Session
	fact               board.CoordinatorGitFacts
	mu                 sync.Mutex
	executions         int
	deliveries         map[string]int
	unexpectedRequests []string
}

func newWrapUpFixture(t *testing.T) *wrapUpFixture {
	t.Helper()
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	isolateTestGit(t)
	const project, execution, target = "wrapup-project", "integration-execution", "refs/heads/v2"
	f := &wrapUpFixture{t: t, deliveries: map[string]int{}, members: []string{"20261124-wrapup-one-task", "20261124-wrapup-two-task"}}
	root := t.TempDir()
	f.bare, f.checkout = filepath.Join(root, "origin.git"), filepath.Join(root, "checkout")
	work := filepath.Join(root, "work")
	runGit(t, root, "init", "-q", "--bare", f.bare)
	runGit(t, root, "clone", "-q", f.bare, work)
	commit := func(text string) string {
		if err := os.WriteFile(filepath.Join(work, "work.txt"), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		runGit(t, work, "add", "work.txt")
		runGit(t, work, "commit", "-q", "-m", text)
		return runGit(t, work, "rev-parse", "HEAD")
	}
	f.base = commit("base\n")
	runGit(t, work, "push", "-q", "origin", f.base+":"+target)
	deliveries := []string{commit("first member\n"), commit("second member\n")}
	f.source = deliveries[1]
	for i, id := range f.members {
		runGit(t, work, "push", "-q", "origin", deliveries[i]+":"+api.DeliveryRef(project, id, 1, deliveries[i]))
	}
	runGit(t, work, "push", "-q", "origin", f.source+":"+api.IntegrationSourceRef(project, execution, f.source))
	runGit(t, work, "push", "-q", "origin", f.source+":"+target)
	runGit(t, root, "clone", "-q", f.bare, f.checkout)

	id := api.IntegrationID(target)
	integration := api.Integration{SourceRef: api.IntegrationSourceRef(project, execution, f.source), ID: id, Generation: 1, Status: api.ClaimReleased, Phase: "integrated", ObservedTarget: f.base, Request: api.IntegrationRequest{Purpose: "deliver", TargetRef: target, Commit: f.source, ExecutionID: execution}}
	evidence := map[string]api.DeliveryEvidence{}
	for i, task := range f.members {
		integration.Request.Members = append(integration.Request.Members, api.IntegrationMember{TaskID: task, Generation: 1, DeliveryCommit: deliveries[i]})
		evidence[task] = api.DeliveryEvidence{Purpose: "delivery", TaskID: task, ExecutionID: "member-execution", Generation: 1, Commit: deliveries[i], Ref: api.DeliveryRef(project, task, 1, deliveries[i]), Remote: f.bare}
	}
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		segments := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/v1/projects/"+project+"/"), "/")
		f.mu.Lock()
		defer f.mu.Unlock()
		var value any
		switch {
		case r.Method == http.MethodGet && len(segments) == 4 && segments[0] == "integrations" && segments[1] == id && segments[2] == "executions":
			f.executions++
			value = integration
		case r.Method == http.MethodGet && len(segments) == 4 && segments[0] == "tasks" && segments[2] == "deliveries":
			f.deliveries[segments[1]]++
			value = evidence[segments[1]]
		default:
			f.unexpectedRequests = append(f.unexpectedRequests, r.Method+" "+r.URL.Path)
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(value)
	}))
	t.Cleanup(stub.Close)
	c, err := client.New(stub.URL, "wrapup-client")
	if err != nil {
		t.Fatal(err)
	}
	c.Retries = 0
	f.session = &remote.Session{Client: c, Project: c.Project(project), Info: api.Project{ID: project, Remote: f.bare}}
	f.fact = board.CoordinatorGitFacts{IntegrationTarget: "v2", Integration: board.DispatchIntegration{IntegrationID: id, IntegrationExecution: execution, IntegrationGeneration: 1, DispatchID: "wrapup-dispatch", TaskID: f.members[0], CWD: "project:" + project, SourceCommit: f.source, ReviewTarget: f.source, ReviewBase: f.base, TargetCommit: f.base, TargetRef: target, VerifiedAt: time.Now().UTC()}}
	return f
}

func (f *wrapUpFixture) checks() ([]api.CoordinatorGitCheck, error) {
	f.t.Helper()
	return reconcileGitChecks(f.t.Context(), f.session, f.checkout, "/request.json", []board.CoordinatorGitFacts{f.fact})
}

func (f *wrapUpFixture) noUnexpectedRequests() {
	f.t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.unexpectedRequests) != 0 {
		f.t.Fatalf("unexpected service requests: %v", f.unexpectedRequests)
	}
}

// A wrap-up fact is verified in full once: one read of the integration
// execution, one read of each member delivery, no fetch repeated with the
// same arguments, and one more observation of the target ref.
func TestReconcileVerifiesAWrapUpFactOnce(t *testing.T) {
	f := newWrapUpFixture(t)
	log := installFakeGit(t)
	checks, err := f.checks()
	if err != nil || len(checks) != 1 || checks[0].Error != "" {
		t.Fatalf("checks %+v, error %v", checks, err)
	}
	f.noUnexpectedRequests()
	if f.executions != 1 {
		t.Errorf("integration execution reads = %d, want 1", f.executions)
	}
	for _, id := range f.members {
		if n := f.deliveries[id]; n != 1 {
			t.Errorf("delivery reads of %s = %d, want 1", id, n)
		}
	}
	fetched := map[string]int{}
	for _, call := range fakeGitCalls(t, log, "fetch") {
		fetched[strings.Join(call, " ")]++
	}
	if len(fetched) == 0 {
		t.Fatal("the verification fetched nothing")
	}
	for call, n := range fetched {
		if n > 1 {
			t.Errorf("git %s ran %d times", call, n)
		}
	}
	targetObservations := 0
	for _, call := range fakeGitCalls(t, log, "ls-remote") {
		if call[len(call)-1] == "refs/heads/v2" {
			targetObservations++
		}
	}
	if targetObservations != 2 {
		t.Errorf("target observations = %d, want the verification and the recheck", targetObservations)
	}
}

// A target that was rewound after the full verification fails the fact, as
// the second full verification did before.
func TestReconcileWrapUpRecheckFindsARewoundTarget(t *testing.T) {
	f := newWrapUpFixture(t)
	installFakeGit(t)
	t.Setenv(envFakeGitRewind, strings.Join([]string{f.bare, "refs/heads/v2", f.base, "2"}, "|"))
	checks, err := f.checks()
	if err != nil || len(checks) != 1 {
		t.Fatalf("checks %+v, error %v", checks, err)
	}
	want := "git merge-base --is-ancestor " + f.source + " " + f.base + ": " + config.Text("remote.integration_recovery_required")
	if !strings.HasPrefix(checks[0].Error, want) {
		t.Fatalf("verdict %q, want %q", checks[0].Error, want)
	}
	f.noUnexpectedRequests()
}

// An observation of the target ref that exceeds its own deadline is no
// verdict: the verification fails before any reconcile request.
func TestReconcileWrapUpTargetDeadlineIsNoVerdict(t *testing.T) {
	f := newWrapUpFixture(t)
	installFakeGit(t)
	saved := remote.IntegrationTargetTimeout
	remote.IntegrationTargetTimeout = 300 * time.Millisecond
	t.Cleanup(func() { remote.IntegrationTargetTimeout = saved })
	t.Setenv(envFakeGitSlow, "ls-remote=1s")
	checks, err := f.checks()
	code, message := command.Describe(err)
	if checks != nil || code != codeReconcileVerifyInterrupted || !strings.Contains(message, f.members[0]) || !strings.Contains(message, "wrapup-dispatch") {
		t.Fatalf("checks %+v, code %q, message %q", checks, code, message)
	}
}
