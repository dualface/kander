package launch

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/remote"
	"github.com/dualface/kander/v2/internal/server"
)

// reconcileFixture is a distributed project with a two-member group whose
// first member delivered a commit through a real origin. Its reconcile
// request asks the client to verify that delivery in Git.
type reconcileFixture struct {
	t       *testing.T
	cfg     *config.Config
	session *remote.Session
	clone   string
	group   string
	member  string
	path    string
	request board.CoordinatorReconcile

	mu       sync.Mutex
	requests map[string]int
	// delayReconcile holds the answer of the next reconcile request after the
	// service applied it.
	delayReconcile time.Duration
}

// isolateTestGit keeps the developer's Git configuration and identity out of
// the fixture repositories.
func isolateTestGit(t *testing.T) {
	t.Helper()
	global := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(global, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", global)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	for name, value := range map[string]string{"GIT_AUTHOR_NAME": "Test", "GIT_AUTHOR_EMAIL": "test@example.invalid", "GIT_COMMITTER_NAME": "Test", "GIT_COMMITTER_EMAIL": "test@example.invalid"} {
		t.Setenv(name, value)
	}
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	data, err := exec.CommandContext(t.Context(), "git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %s: %v", args, data, err)
	}
	return strings.TrimSpace(string(data))
}

func newReconcileFixture(t *testing.T) *reconcileFixture {
	t.Helper()
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	t.Setenv(remote.EnvExecutionID, "")
	isolateTestGit(t)
	f := &reconcileFixture{t: t, requests: map[string]int{}, group: "20261124-reconcile-deadline-group"}
	root := t.TempDir()
	bare := filepath.Join(root, "origin.git")
	f.clone = filepath.Join(root, "work")
	runGit(t, root, "init", "-q", "--bare", bare)
	runGit(t, root, "clone", "-q", bare, f.clone)
	if err := os.WriteFile(filepath.Join(f.clone, "work.txt"), []byte("base\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, f.clone, "add", "work.txt")
	runGit(t, f.clone, "commit", "-q", "-m", "Base")
	runGit(t, f.clone, "push", "-q", "origin", "HEAD:refs/heads/develop")
	if err := os.WriteFile(filepath.Join(f.clone, "work.txt"), []byte("delivery\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, f.clone, "commit", "-q", "-am", "Deliver")
	commit := runGit(t, f.clone, "rev-parse", "HEAD")

	srv, err := server.Open(server.Options{DataDir: filepath.Join(root, "server"), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	handler := srv.Handler()
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { f.serve(w, r, handler) }))
	t.Cleanup(func() { hs.Close(); _ = srv.Close() })
	t.Chdir(f.clone)
	t.Setenv(board.EnvBoardDir, filepath.Join(root, "forbidden-board"))
	t.Setenv(remote.EnvClientHome, filepath.Join(root, "client"))
	f.cfg = config.DefaultConfig()
	f.cfg.Server.URL = hs.URL
	f.cfg.Project.Name = "reconcile-deadline"
	ctx := t.Context()
	if f.session, err = remote.Open(ctx, f.cfg, f.clone); err != nil {
		t.Fatal(err)
	}
	initTestPolicy(t, f.session)
	p := f.session.Project
	var members []string
	for _, slug := range []string{"reconcile-one", "reconcile-two"} {
		task, err := p.NewTask(ctx, "", api.NewTask{Type: "chore", Slug: slug, Title: "Deliver to a group", Language: "en"})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+f.group, 1)
		text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: task/"+slug, 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
		if task, err = p.PutDocument(ctx, "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
			t.Fatal(err)
		}
		if task, err = p.Move(ctx, "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
			t.Fatal(err)
		}
		members = append(members, task.TaskID)
	}
	f.member = members[0]
	coordinator, err := f.session.AcquireCoordinator(ctx, board.CoordinatorClaim{GroupID: f.group, Owner: "codex", Token: "public-label", Basis: "confirmed reconcile fixture", Members: members}, "confirmed reconcile fixture")
	if err != nil {
		t.Fatal(err)
	}
	grant, err := p.Claim(ctx, "", f.member, api.ClaimRequest{ClientID: f.session.Client.ClientID(), ExecutionID: "reconcile-member", Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	credential := remote.Credential{ServerURL: f.session.ServerURL, ClientID: f.session.Client.ClientID(), ProjectID: f.session.Info.ID, TaskID: f.member, ExecutionID: grant.Claim.ExecutionID, Generation: grant.Claim.Generation, Token: grant.Token}
	if err := f.session.SaveCredential(credential); err != nil {
		t.Fatal(err)
	}
	if _, err := f.session.PublishCommit(ctx, f.clone, f.member, commit, credential); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Move(ctx, "", f.member, api.Move{State: "review", DeliveryCommit: commit}, credential.Claim()); err != nil {
		t.Fatal(err)
	}
	f.request = board.CoordinatorReconcile{GroupID: f.group, ExpectedRevision: coordinator.Checkpoint.Revision, Authority: coordinator.Checkpoint.Authority, Members: map[string]board.CoordinatorObservation{}}
	for _, id := range members {
		task, err := p.Show(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		observation := board.CoordinatorObservation{Revision: task.Revision}
		if id == f.member {
			observation.DeliveryCommit = commit
		}
		f.request.Members[id] = observation
	}
	f.path = filepath.Join(t.TempDir(), "reconcile.json")
	data, _ := json.Marshal(f.request)
	if err := os.WriteFile(f.path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

// serve counts the requests by method and the last path segment, and holds a
// reconcile answer when the test asks for it.
func (f *reconcileFixture) serve(w http.ResponseWriter, r *http.Request, handler http.Handler) {
	segments := strings.Split(r.URL.Path, "/")
	f.mu.Lock()
	f.requests[r.Method+" "+segments[len(segments)-1]]++
	delay := time.Duration(0)
	if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/reconcile") {
		delay, f.delayReconcile = f.delayReconcile, 0
	}
	f.mu.Unlock()
	if delay == 0 {
		handler.ServeHTTP(w, r)
		return
	}
	recorded := httptest.NewRecorder()
	handler.ServeHTTP(recorded, r)
	select {
	case <-time.After(delay):
	case <-r.Context().Done():
	}
	for name, values := range recorded.Header() {
		w.Header()[name] = values
	}
	w.WriteHeader(recorded.Code)
	_, _ = w.Write(recorded.Body.Bytes())
}

func (f *reconcileFixture) reconcileRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests["POST reconcile"]
}

func (f *reconcileFixture) checkpointRevision() uint64 {
	f.t.Helper()
	view, err := f.session.Project.Group(f.t.Context(), f.group)
	if err != nil || view.Checkpoint == nil {
		f.t.Fatalf("group: %+v %v", view, err)
	}
	return view.Checkpoint.Revision
}

// run runs the command as the CLI does and returns its exit status, standard
// output and standard error.
func (f *reconcileFixture) run() (int, string, string) {
	f.t.Helper()
	var status int
	var stdout []byte
	stderr := captureStderr(f.t, func() {
		status, stdout = captureStdout(f.t, func() int {
			return runRemoteCoordinator(context.Background(), f.cfg, []string{"reconcile", f.path})
		})
	})
	return status, string(stdout), stderr
}

// reconcile runs the reconcile phases directly and returns the error with its
// stable code.
func (f *reconcileFixture) reconcile(parent context.Context) (board.CoordinatorCheckpoint, error, string) {
	f.t.Helper()
	ctx, cancel := context.WithTimeout(parent, coordinatorTimeout)
	defer cancel()
	checkpoint, err := remoteReconcile(parent, ctx, f.session, f.clone, f.path, f.request)
	code := ""
	if err != nil {
		code, _ = command.Describe(err)
	}
	return checkpoint, err, code
}

// notUnreachable fails when a message reports an unreachable server, in the
// language the catalog uses.
func notUnreachable(t *testing.T, message string) {
	t.Helper()
	prefix, _, _ := strings.Cut(config.Text("remote.unreachable", "\x00"), "\x00")
	if strings.Contains(message, prefix) || strings.Contains(message, "server unreachable") {
		t.Errorf("message %q reports an unreachable server", message)
	}
}

func captureStderr(t *testing.T, run func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stderr
	os.Stderr = w
	done := make(chan []byte)
	go func() {
		var out bytes.Buffer
		_, _ = io.Copy(&out, r)
		done <- out.Bytes()
	}()
	func() {
		defer func() { os.Stderr = saved }()
		run()
	}()
	w.Close()
	return string(<-done)
}

// setReconcileTimeouts replaces the deadlines of the coordinator commands for
// one test.
func setReconcileTimeouts(t *testing.T, coordinator, verify, request time.Duration) {
	t.Helper()
	savedCoordinator, savedVerify, savedRequest := coordinatorTimeout, reconcileVerifyTimeout, reconcileRequestTimeout
	coordinatorTimeout, reconcileVerifyTimeout, reconcileRequestTimeout = coordinator, verify, request
	t.Cleanup(func() {
		coordinatorTimeout, reconcileVerifyTimeout, reconcileRequestTimeout = savedCoordinator, savedVerify, savedRequest
	})
}

// A verification longer than the request deadline and the deadline of the
// other requests still sends the reconcile request: the request deadline
// starts after the verification.
func TestReconcileRequestDeadlineStartsAfterVerification(t *testing.T) {
	f := newReconcileFixture(t)
	installFakeGit(t)
	setReconcileTimeouts(t, 2*time.Second, time.Minute, 2*time.Second)
	t.Setenv(envFakeGitSlow, "ls-remote=1500ms,fetch=1500ms")
	started := time.Now()
	status, stdout, stderr := f.run()
	if status != 0 {
		t.Fatalf("status %d: %s %s", status, stdout, stderr)
	}
	if elapsed := time.Since(started); elapsed < 3*time.Second {
		t.Fatalf("the slow verification took only %s", elapsed)
	}
	var printed reconcileOutput
	if err := json.Unmarshal([]byte(stdout), &printed); err != nil || printed.Result != reconcileComplete || printed.Members[f.member].DeliveryCommit == "" {
		t.Fatalf("output %s: %v", stdout, err)
	}
	if n := f.reconcileRequests(); n != 1 {
		t.Fatalf("reconcile requests = %d, want 1", n)
	}
}

// A verification that outlasts its own deadline sends no reconcile request
// and no verdict, and the failure names the phase, the member, the deadline
// and the next step.
func TestReconcileVerificationDeadlineSendsNothing(t *testing.T) {
	f := newReconcileFixture(t)
	installFakeGit(t)
	setReconcileTimeouts(t, 30*time.Second, 700*time.Millisecond, 30*time.Second)
	t.Setenv(envFakeGitSlow, "ls-remote=5s")
	before := f.checkpointRevision()
	status, stdout, stderr := f.run()
	if status != 1 {
		t.Fatalf("status %d: %s %s", status, stdout, stderr)
	}
	// The cause is the local Git error; the text around it names the phase,
	// the member, the deadline and the next step.
	fact := board.CoordinatorGitFacts{Delivery: &board.CoordinatorDelivery{TaskID: f.member, Commit: f.request.Members[f.member].DeliveryCommit}}
	around := strings.Split(config.Text("remote.reconcile_verify_interrupted", describeCoordinatorFact(fact), "\x00", 700*time.Millisecond, f.path), "\x00")
	if len(around) != 2 || !strings.HasPrefix(stderr, "kander: "+around[0]) || !strings.HasSuffix(stderr, around[1]+"\n") {
		t.Errorf("message %q is not %q", stderr, around)
	}
	for _, want := range []string{f.member, "700ms", "kander coordinator reconcile " + f.path} {
		if !strings.Contains(stderr, want) {
			t.Errorf("message %q lacks %q", stderr, want)
		}
	}
	notUnreachable(t, stderr)
	if _, err, code := f.reconcile(t.Context()); err == nil || code != codeReconcileVerifyInterrupted {
		t.Fatalf("code %q, error %v", code, err)
	}
	if n := f.reconcileRequests(); n != 0 {
		t.Fatalf("reconcile requests = %d, want 0", n)
	}
	if after := f.checkpointRevision(); after != before {
		t.Fatalf("checkpoint revision %d -> %d", before, after)
	}
}

// A Git step that exceeds its own deadline inside a longer verification
// deadline is no verdict either.
func TestReconcileStepDeadlineIsNoVerdict(t *testing.T) {
	f := newReconcileFixture(t)
	installFakeGit(t)
	setReconcileTimeouts(t, 30*time.Second, time.Minute, 30*time.Second)
	saved := remote.GitCommandTimeout
	remote.GitCommandTimeout = 500 * time.Millisecond
	t.Cleanup(func() { remote.GitCommandTimeout = saved })
	t.Setenv(envFakeGitSlow, "ls-remote=5s")
	_, err, code := f.reconcile(t.Context())
	if err == nil || code != codeReconcileVerifyInterrupted || !strings.Contains(err.Error(), f.member) {
		t.Fatalf("code %q, error %v", code, err)
	}
	if n := f.reconcileRequests(); n != 0 {
		t.Fatalf("reconcile requests = %d, want 0", n)
	}
	view, err := f.session.Project.Group(t.Context(), f.group)
	if err != nil || len(view.Checkpoint.BlockedMembers()) != 0 {
		t.Fatalf("a member is blocked: %+v %v", view.Checkpoint, err)
	}
}

// A reconcile request whose answer comes after its deadline reports an
// unknown result with the next steps, and the same request file then
// completes as a replay without a new checkpoint revision.
func TestReconcileUnknownResultRerunsAsReplay(t *testing.T) {
	f := newReconcileFixture(t)
	setReconcileTimeouts(t, 30*time.Second, time.Minute, time.Second)
	f.delayReconcile = 3 * time.Second
	before := f.checkpointRevision()
	status, stdout, stderr := f.run()
	if status != 1 {
		t.Fatalf("status %d: %s %s", status, stdout, stderr)
	}
	if want := "kander: " + config.Text("remote.reconcile_result_unknown", f.group, time.Second, f.path) + "\n"; stderr != want {
		t.Errorf("message %q, want %q", stderr, want)
	}
	for _, want := range []string{"kander coordinator show " + f.group, "kander coordinator reconcile " + f.path} {
		if !strings.Contains(stderr, want) {
			t.Errorf("message %q lacks %q", stderr, want)
		}
	}
	notUnreachable(t, stderr)
	applied := f.checkpointRevision()
	if applied != before+1 {
		t.Fatalf("checkpoint revision %d -> %d, want the held request applied", before, applied)
	}
	status, stdout, stderr = f.run()
	if status != 0 {
		t.Fatalf("rerun status %d: %s %s", status, stdout, stderr)
	}
	var printed reconcileOutput
	if err := json.Unmarshal([]byte(stdout), &printed); err != nil || printed.Result != reconcileComplete || printed.Revision != applied {
		t.Fatalf("rerun output %s: %v", stdout, err)
	}
	if after := f.checkpointRevision(); after != applied {
		t.Fatalf("the rerun advanced the checkpoint %d -> %d", applied, after)
	}
	if n := f.reconcileRequests(); n != 2 {
		t.Fatalf("reconcile requests = %d, want 2", n)
	}
}

// A command whose context ended before the reconcile request sends nothing
// and does not blame the server.
func TestReconcileRequestIsNotSentAfterTheCommandEnded(t *testing.T) {
	f := newReconcileFixture(t)
	proof, err := f.session.CoordinatorAuthority(f.group, nil)
	if err != nil {
		t.Fatal(err)
	}
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	expired, stop := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
	defer stop()
	for name, parent := range map[string]context.Context{"canceled": canceled, "expired": expired} {
		t.Run(name, func(t *testing.T) {
			_, err := postReconcile(parent, f.session, f.path, api.GroupReconcile{Request: f.request}, proof)
			code, message := command.Describe(err)
			if code != codeReconcileNotSent || !strings.Contains(message, "kander coordinator reconcile "+f.path) {
				t.Fatalf("code %q, message %q", code, message)
			}
			notUnreachable(t, message)
		})
	}
	if n := f.reconcileRequests(); n != 0 {
		t.Fatalf("reconcile requests = %d, want 0", n)
	}
}

// A server that refuses the connection is still unreachable, with the text of
// before.
func TestReconcileUnreachableServerKeepsItsCode(t *testing.T) {
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	closed := httptest.NewServer(http.NotFoundHandler())
	closed.Close()
	c, err := client.New(closed.URL, "reconcile-unreachable")
	if err != nil {
		t.Fatal(err)
	}
	c.Retries = 0
	s := &remote.Session{Client: c, Project: c.Project("project"), Info: api.Project{ID: "project"}}
	_, err = postReconcile(t.Context(), s, "/request.json", api.GroupReconcile{Request: board.CoordinatorReconcile{GroupID: "20261124-unreachable-group"}}, client.Claim{})
	code, message := command.Describe(err)
	if code != remote.CodeUnreachable || message != config.Text("remote.unreachable", err.Error()) || !strings.HasPrefix(err.Error(), "server unreachable: ") {
		t.Fatalf("code %q, message %q", code, message)
	}
}
