package launch

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/config"
	"github.com/dualface/kander/internal/remote"
	"github.com/dualface/kander/internal/server"
)

// releaseFixture is a claimed group on an in-process service, with the client
// configuration the command reads and a counter of the requests that reach it.
type releaseFixture struct {
	session  *remote.Session
	group    string
	grant    api.GroupGrant
	path     string
	requests *atomic.Int64
}

func newReleaseFixture(t *testing.T) *releaseFixture {
	t.Helper()
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	t.Setenv(remote.EnvExecutionID, "")
	t.Setenv(remote.EnvCoordinatorExecutionID, "")
	srv, err := server.Open(server.Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	requests := &atomic.Int64{}
	httpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		srv.Handler().ServeHTTP(w, r)
	}))
	t.Cleanup(func() { httpServer.Close(); _ = srv.Close() })
	cwd := t.TempDir()
	t.Chdir(cwd)
	t.Setenv(board.EnvBoardDir, filepath.Join(cwd, "forbidden-board"))
	t.Setenv(remote.EnvClientHome, filepath.Join(t.TempDir(), "client"))
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	cfg := config.DefaultConfig()
	cfg.Server.URL = httpServer.URL
	cfg.Project.Name = "release-guard"
	if _, err = config.Save(cfg); err != nil {
		t.Fatal(err)
	}
	session, err := remote.Open(t.Context(), cfg, cwd)
	if err != nil {
		t.Fatal(err)
	}
	f := &releaseFixture{session: session, group: "20261118-release-group", requests: requests}
	var members []string
	for _, slug := range []string{"release-one", "release-two"} {
		task, err := session.Project.NewTask(t.Context(), "", api.NewTask{Type: "chore", Slug: slug, Title: "Release a group", Language: "en"})
		if err != nil {
			t.Fatal(err)
		}
		text := strings.ReplaceAll(task.Text, "<FILL_IN>", "filled")
		text = strings.Replace(text, "- TASK_GROUP:", "- TASK_GROUP: "+f.group, 1)
		text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
		if task, err = session.Project.PutDocument(t.Context(), "", task.TaskID, api.Document{Path: "spec.md", Text: text, ExpectedRevision: task.Revision}, nil); err != nil {
			t.Fatal(err)
		}
		if task, err = session.Project.Move(t.Context(), "", task.TaskID, api.Move{State: "todo"}, nil); err != nil {
			t.Fatal(err)
		}
		members = append(members, task.TaskID)
	}
	if f.grant, err = session.AcquireCoordinator(t.Context(), board.CoordinatorClaim{GroupID: f.group, Owner: "codex", Token: "label", Basis: "release fixture", Members: members}, "release fixture"); err != nil {
		t.Fatal(err)
	}
	f.path = filepath.Join(t.TempDir(), "release.json")
	data, _ := json.Marshal(api.GroupRelease{GroupID: f.group, Reason: "the group is done", Decision: "decision-release"})
	if err = os.WriteFile(f.path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	requests.Store(0)
	return f
}

// run executes the command and returns its status, standard output and
// standard error.
func (f *releaseFixture) run(t *testing.T, args ...string) (int, []byte, string) {
	t.Helper()
	return captureBoth(t, func() int { return RunCoordinatorRemote(args) })
}

func captureBoth(t *testing.T, run func() int) (int, []byte, string) {
	t.Helper()
	savedErr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	status, out := captureStdout(t, run)
	os.Stderr = savedErr
	w.Close()
	var diagnostics bytes.Buffer
	if _, err = io.Copy(&diagnostics, r); err != nil {
		t.Fatal(err)
	}
	return status, out, diagnostics.String()
}

func (f *releaseFixture) lease(t *testing.T) *api.ClaimView {
	t.Helper()
	view, err := f.session.Project.Group(t.Context(), f.group)
	if err != nil || view.Claim == nil {
		t.Fatalf("%+v %v", view, err)
	}
	return view.Claim
}

func TestReleaseWithoutConfirmationSendsNothing(t *testing.T) {
	f := newReleaseFixture(t)
	before := *f.lease(t)
	f.requests.Store(0)
	status, out, diagnostics := f.run(t, "release", f.path)
	if status != 2 || len(out) != 0 {
		t.Fatalf("status %d, output %q", status, out)
	}
	if !strings.Contains(diagnostics, config.Text("remote.coordinator_release_needs_end_execution")) {
		t.Fatalf("the consequence is not explained: %q", diagnostics)
	}
	if !slices.Contains(strings.Split(diagnostics, "\n"), "kander coordinator release --end-execution <absolute-release.json>") {
		t.Fatalf("the new syntax is not given on a line of its own: %q", diagnostics)
	}
	if n := f.requests.Load(); n != 0 {
		t.Fatalf("the command sent %d requests", n)
	}
	// The lease is unchanged. Reading it above sent requests, so compare it.
	if after := *f.lease(t); after.Status != api.ClaimActive || after.Generation != before.Generation || after.ExecutionID != before.ExecutionID {
		t.Fatalf("the lease changed: %+v -> %+v", before, after)
	}
}

func TestReleaseWithConfirmationEndsTheExecution(t *testing.T) {
	f := newReleaseFixture(t)
	status, out, diagnostics := f.run(t, "release", "--end-execution", f.path)
	if status != 0 {
		t.Fatalf("status %d: %s %s", status, out, diagnostics)
	}
	var printed map[string]json.RawMessage
	if err := json.Unmarshal(out, &printed); err != nil || len(printed) != 1 || printed["claim"] == nil {
		t.Fatalf("the output shape changed: %s %v", out, err)
	}
	var renewal api.Renewal
	if err := json.Unmarshal(out, &renewal); err != nil || renewal.Claim.Status != api.ClaimReleased || renewal.Claim.Generation != f.grant.Claim.Generation {
		t.Fatalf("%+v %v", renewal, err)
	}
	want := config.Text("remote.coordinator_release_final", f.grant.Claim.ExecutionID, f.group, f.grant.Claim.Generation)
	if !strings.Contains(diagnostics, want) {
		t.Fatalf("diagnostics %q lack %q", diagnostics, want)
	}
	for _, part := range []string{f.grant.Claim.ExecutionID, f.group, "dismiss"} {
		if !strings.Contains(diagnostics, part) {
			t.Fatalf("diagnostics lack %q: %q", part, diagnostics)
		}
	}
}

func TestEndExecutionBelongsToReleaseOnly(t *testing.T) {
	f := newReleaseFixture(t)
	for _, args := range [][]string{
		{"claim", "--end-execution", f.path},
		{"reconcile", "--end-execution", f.path},
		{"show", "--end-execution", f.group},
		{"remove-member", "--end-execution", f.path},
		{"claim", "--end-execution"},
		{"release", "--end-execution"},
		{"release", f.path, "--end-execution"},
		{"release", "--end-execution", "--end-execution"},
		{"release", "--end-execution", f.path, f.path},
	} {
		status, out, _ := f.run(t, args...)
		if status != 2 || len(out) != 0 {
			t.Errorf("%v: status %d, output %q", args, status, out)
		}
	}
	_, _, diagnostics := f.run(t, "release", f.path, "--end-execution")
	if !strings.Contains(diagnostics, config.Text("remote.coordinator_end_execution_position")) {
		t.Fatalf("a flag after the path is not explained: %q", diagnostics)
	}
	if n := f.requests.Load(); n != 0 {
		t.Fatalf("the commands sent %d requests", n)
	}
	if status := f.lease(t).Status; status != api.ClaimActive {
		t.Fatalf("the lease is %s", status)
	}
}

// The command tree checks the arguments before the project binding. The refusal of a malformed
// or unconfirmed command comes first, so a coordinator that has no project binding still learns
// the confirmed syntax.
func TestCoordinatorArgumentRefusalPrecedesTheBindingCheck(t *testing.T) {
	t.Setenv(config.EnvLang, "en")
	t.Setenv(config.EnvLangCLI, "1")
	t.Setenv(config.EnvConfig, filepath.Join(t.TempDir(), "config.json"))
	dir := t.TempDir()
	t.Chdir(dir)
	releasePath := filepath.Join(dir, "release.json")
	claimPath := filepath.Join(dir, "claim.json")
	if _, err := config.Save(config.DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	run := RunCoordinatorRemote
	status, _, diagnostics := captureBoth(t, func() int { return run([]string{"release", releasePath}) })
	if status != 2 || !slices.Contains(strings.Split(diagnostics, "\n"), releaseSyntax) {
		t.Fatalf("status %d: %q", status, diagnostics)
	}
	if status, _, _ = captureBoth(t, func() int { return run([]string{"claim", "--end-execution", claimPath}) }); status != 2 {
		t.Fatalf("status %d", status)
	}
	// A well-formed command still needs the binding.
	if status, _, diagnostics = captureBoth(t, func() int { return run([]string{"release", "--end-execution", releasePath}) }); status != 1 || !strings.Contains(diagnostics, config.Text("remote.not_configured")) {
		t.Fatalf("status %d: %q", status, diagnostics)
	}
}
