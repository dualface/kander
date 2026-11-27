package remote

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command"
	"github.com/dualface/kander/v2/internal/command/commandtest"
	"github.com/dualface/kander/v2/internal/config"
)

// gcCommand runs `kander gc` against the fixture. answer is the terminal's
// reply; empty means no terminal. server, when set, is the local server gc.
type gcCommand struct {
	f       *gcFixture
	answer  string
	server  serverGC
	skipped string
	opened  bool
	calls   []api.ServerGCRequest
	prompt  bytes.Buffer
	// promptIn and promptOut, when set, replace the answer and the prompt
	// buffer; promptIn alone makes a terminal.
	promptIn  io.Reader
	promptOut io.Writer
}

func (c *gcCommand) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	open, terminal, in, out, resolve := gcOpen, gcTerminal, gcPromptIn, gcPromptOut, resolveServerGC
	t.Cleanup(func() {
		gcOpen, gcTerminal, gcPromptIn, gcPromptOut, resolveServerGC = open, terminal, in, out, resolve
	})
	gcOpen = func(context.Context) (*Session, *config.Config, error) {
		c.opened = true
		cfg := &config.Config{}
		cfg.Git.BranchMode = config.GitBranchModeMainOnly
		return c.f.s, cfg, nil
	}
	gcTerminal = func() bool { return c.answer != "" || c.promptIn != nil }
	gcPromptIn, gcPromptOut = strings.NewReader(c.answer), &c.prompt
	if c.promptIn != nil {
		gcPromptIn = c.promptIn
	}
	if c.promptOut != nil {
		gcPromptOut = c.promptOut
	}
	resolveServerGC = func(context.Context, string) (serverGC, string, error) {
		if c.server == nil {
			return nil, c.skipped, nil
		}
		return func(ctx context.Context, req api.ServerGCRequest) (api.ServerGCReport, error) {
			c.calls = append(c.calls, req)
			return c.server(ctx, req)
		}, "", nil
	}
	chdir(t, c.f.repo)
	probe := c.f.options(true).Probe
	return commandtest.Capture(func() int { return commandtest.Run(NewGCCommand(probe), args...) })
}

func chdir(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(previous) })
}

// removable prepares one worktree the gc removes.
func removable(t *testing.T) (*gcFixture, ExecutionWorktree) {
	f := newGCFixture(t)
	f.card(gcTask, "done")
	return f, f.worktree(gcTask, 1)
}

func TestGCCommandRefusesWithoutConfirmation(t *testing.T) {
	f, w := removable(t)
	for _, args := range [][]string{{"--json"}, {}, {"--older-than", "30m", "--yes"}, {"--older-than", "soon", "--yes"}, {"extra"}} {
		c := &gcCommand{f: f}
		code, out, errOut := c.run(t, args...)
		if code != 2 || c.opened || !f.registered(w.Path) {
			t.Fatalf("%v: exit %d, opened %v: %s %s", args, code, c.opened, out, errOut)
		}
		if len(args) == 1 && args[0] == "--json" && !strings.Contains(errOut, "interactive_required") {
			t.Fatalf("--json without --yes or --dry-run must say interactive_required: %s", errOut)
		}
	}
}

func TestGCCommandAsksBeforeRemoving(t *testing.T) {
	for _, answer := range []string{"n\n", "\n"} {
		f, w := removable(t)
		c := &gcCommand{f: f, answer: answer}
		code, out, _ := c.run(t)
		if code != 1 || !f.registered(w.Path) {
			t.Fatalf("answer %q: exit %d: %s", answer, code, out)
		}
		if !strings.Contains(out, config.Text("gc.plan_header")) || !strings.Contains(c.prompt.String(), config.Text("gc.question")) {
			t.Fatalf("the plan goes to stdout and the question to stderr: %q %q", out, c.prompt.String())
		}
	}
	f, w := removable(t)
	c := &gcCommand{f: f, answer: "y\n"}
	if code, out, errOut := c.run(t); code != 0 || f.registered(w.Path) {
		t.Fatalf("yes must remove: %d %s %s", code, out, errOut)
	}
}

func TestGCCommandMergesTheLocalServerGC(t *testing.T) {
	f, w := removable(t)
	c := &gcCommand{f: f}
	c.server = func(_ context.Context, req api.ServerGCRequest) (api.ServerGCReport, error) {
		if !req.DryRun && f.registered(w.Path) {
			t.Error("the client part must run before the server gc")
		}
		return api.ServerGCReport{DryRun: req.DryRun, Projects: []api.ServerGCProject{{Name: "demo", Receipts: api.ServerGCReceipts{Files: 3, Bytes: 300}}}}, nil
	}
	code, out, errOut := c.run(t, "--json", "--yes")
	var report GCReport
	if err := json.Unmarshal([]byte(out), &report); err != nil || code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	if report.Server.Report == nil || report.Server.Report.Projects[0].Receipts.Files != 3 || report.Projects[0].Worktrees.Count != 1 {
		t.Fatalf("the server result must be part of the output: %s", out)
	}
	if len(c.calls) != 2 || !c.calls[0].DryRun || c.calls[1].DryRun || c.calls[0].Project != "demo" || c.calls[0].OlderThan != "168h0m0s" {
		t.Fatalf("the server is asked for its plan, then runs: %+v", c.calls)
	}
}

func TestGCCommandRunsTheClientPartWhenTheServerRefuses(t *testing.T) {
	f, w := removable(t)
	c := &gcCommand{f: f}
	c.server = func(context.Context, api.ServerGCRequest) (api.ServerGCReport, error) {
		return api.ServerGCReport{}, &api.Error{Status: http.StatusForbidden, Code: api.CodeInvalidRequest}
	}
	code, out, _ := c.run(t, "--json", "--yes")
	var report GCReport
	if err := json.Unmarshal([]byte(out), &report); err != nil || code != 1 {
		t.Fatalf("a refused server part exits 1: %d %s", code, out)
	}
	if report.Server.Refused != config.Text("gc.refused") || f.registered(w.Path) || len(c.calls) != 1 {
		t.Fatalf("the client part still runs and the server is not asked again: %s %d", out, len(c.calls))
	}
}

func TestGCCommandSkipsTheServer(t *testing.T) {
	f, _ := removable(t)
	c := &gcCommand{f: f, server: func(context.Context, api.ServerGCRequest) (api.ServerGCReport, error) {
		return api.ServerGCReport{}, nil
	}}
	code, out, _ := c.run(t, "--dry-run", "--client-only")
	if code != 0 || len(c.calls) != 0 || !strings.Contains(out, config.Text("gc.client_server_skipped", config.Text("gc.client_server_client_only"))) {
		t.Fatalf("--client-only never calls the server: %d %d %s", code, len(c.calls), out)
	}
	c = &gcCommand{f: f, skipped: "remote"}
	if code, out, _ := c.run(t, "--dry-run"); code != 0 || !strings.Contains(out, config.Text("gc.client_server_skipped", "remote")) {
		t.Fatalf("a skipped server part is reported: %d %s", code, out)
	}
}

func TestResolveServerGCSkipsRemoteServersAndUnreadableKeys(t *testing.T) {
	call, skipped, err := resolveServerGC(t.Context(), "http://kander.example.invalid:7878")
	if call != nil || err != nil || skipped != config.Text("gc.client_server_remote", "http://kander.example.invalid:7878") || !strings.Contains(skipped, "kander server gc") {
		t.Fatalf("a remote server is skipped: %q %v", skipped, err)
	}
	data := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(api.ServerInfo{DataDir: data})
	}))
	t.Cleanup(server.Close)
	call, skipped, err = resolveServerGC(t.Context(), server.URL)
	if call != nil || err != nil || !strings.HasPrefix(skipped, strings.SplitN(config.Text("gc.client_server_no_key", data, "x"), "(", 2)[0]) || !strings.Contains(skipped, "kander server gc") {
		t.Fatalf("an unreadable key skips the server part: %q %v", skipped, err)
	}
}

// --all-projects matches each client project directory by the project ID its
// own records name, confirmed by a read of that project, also without a
// server gc: a directory of another server or of a project the server does
// not have is unmatched, one that names no project is unverified (exit 1),
// and neither changes.
func TestGCCommandAllProjectsMatchesDirectoriesByTheirRecords(t *testing.T) {
	f := newGCFixture(t)
	other := api.Project{ID: api.ProjectID("other"), Name: "other", Remote: f.bare}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/projects/" + other.ID:
			_ = json.NewEncoder(w).Encode(other)
		case "/api/v1/projects/" + other.ID + "/tasks":
			_ = json.NewEncoder(w).Encode(api.TaskList{})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeNotFound, Message: r.URL.Path})
		}
	}))
	t.Cleanup(server.Close)
	cl, err := client.New(server.URL, "c-gc-host")
	if err != nil {
		t.Fatal(err)
	}
	cl.Retries = 0
	// Bind the fixture to this server before anything is created below its
	// namespace.
	if err := os.Remove(f.root); err != nil {
		t.Fatal(err)
	}
	f.s.ServerURL, f.s.Client = server.URL, cl
	if f.root, err = f.s.LocalDataRoot(); err != nil {
		t.Fatal(err)
	}
	f.card(gcTask, "done")
	f.worktree(gcTask, 1)
	retained := func(serverURL, project string) string {
		namespace := projectNamespace(serverURL, project)
		data, _ := json.Marshal(RetainedExecution{ServerURL: serverURL, ProjectID: project, TaskID: gcTask, ExecutionID: "x-1", Generation: 1})
		f.write(filepath.Join(f.s.Home, "retained-executions", namespace, gcTask), "x-1.json", string(data))
		dir := filepath.Join(f.s.Home, "projects", namespace)
		f.write(dir, "launch-actions/a.json", `{"schema":1,"phase":"complete","task_id":"`+gcTask+`"}`)
		return dir
	}
	retained(server.URL, other.ID)
	foreign := retained("http://kander.example.invalid:7878", "p-elsewhere")
	gone := retained(server.URL, "p-gone")
	stray := filepath.Join(f.s.Home, "projects", strings.Repeat("ab", 32))
	f.write(stray, "launch-actions/a.json", `{"schema":1,"phase":"complete","task_id":"`+gcTask+`"}`)
	before := map[string]map[string]string{}
	for _, dir := range []string{foreign, gone, stray} {
		before[dir] = snapshot(t, dir)
	}
	c := &gcCommand{f: f}
	code, out, errOut := c.run(t, "--json", "--yes", "--all-projects", "--client-only")
	var report GCReport
	if err := json.Unmarshal([]byte(out), &report); err != nil || code != 1 {
		t.Fatalf("an unverified directory exits 1: %d %s %s", code, out, errOut)
	}
	unmatched := []string{filepath.Base(foreign), filepath.Base(gone)}
	slices.Sort(unmatched)
	if len(report.Projects) != 2 || report.Projects[1].Name != "other" || !slices.Equal(report.Unmatched, unmatched) {
		t.Fatalf("the matched project is covered and the others are unmatched: %s", out)
	}
	if len(report.Unverified) != 1 || report.Unverified[0].ID != filepath.Base(stray) || report.Unverified[0].Reason == "" {
		t.Fatalf("a directory that names no project is unverified, with the reason: %s", out)
	}
	for dir, files := range before {
		if !sameSnapshot(files, snapshot(t, dir)) {
			t.Fatalf("%s must not change", dir)
		}
	}
}

// With a server gc the server covers every project.
func TestGCCommandAllProjectsAsksTheServerForEveryProject(t *testing.T) {
	f, _ := removable(t)
	c := &gcCommand{f: f, server: func(_ context.Context, req api.ServerGCRequest) (api.ServerGCReport, error) {
		return api.ServerGCReport{DryRun: req.DryRun}, nil
	}}
	if code, out, errOut := c.run(t, "--json", "--dry-run", "--all-projects"); code != 0 || len(c.calls) != 1 || c.calls[0].Project != "" {
		t.Fatalf("exit %d, calls %+v: %s %s", code, c.calls, out, errOut)
	}
}

func TestGCCommandExitsOneAfterHandlingTheRest(t *testing.T) {
	f, w := removable(t)
	dirty := "20261101-gc-dirty-task"
	f.card(dirty, "done")
	kept := f.worktree(dirty, 1)
	f.write(kept.Path, "notes.txt", "draft\n")
	c := &gcCommand{f: f, skipped: "remote"}
	code, out, _ := c.run(t, "--yes")
	if code != 1 || f.registered(w.Path) || !f.registered(kept.Path) || !strings.Contains(out, config.Text("gc.client_kept_worktree", dirty+"/g1", config.Text("gc.client_dirty"))) {
		t.Fatalf("a kept candidate exits 1 after the others were removed: %d %s", code, out)
	}
}

// With --yes the text output shows the plan before the result.
func TestGCCommandPrintsThePlanBeforeAYesRun(t *testing.T) {
	f, w := removable(t)
	c := &gcCommand{f: f, skipped: "remote"}
	code, out, errOut := c.run(t, "--yes")
	plan, result := strings.Index(out, config.Text("gc.plan_header")), strings.Index(out, config.Text("gc.result_header"))
	if code != 0 || plan < 0 || result < plan || f.registered(w.Path) {
		t.Fatalf("exit %d\n%s\n%s", code, out, errOut)
	}
}

// The tail an interrupted server.log trim saved is a plan item of its own:
// the command asks for it and runs the server gc, also when nothing else is
// due.
func TestGCCommandOffersTheRecoveryOfASavedServerLogTail(t *testing.T) {
	f := newGCFixture(t)
	c := &gcCommand{f: f, answer: "y\n"}
	c.server = func(_ context.Context, req api.ServerGCRequest) (api.ServerGCReport, error) {
		return api.ServerGCReport{DryRun: req.DryRun, Projects: []api.ServerGCProject{{Name: "demo"}}, Log: &api.ServerGCLog{Path: "server.log", Recovered: 2048}}, nil
	}
	code, out, errOut := c.run(t)
	if code != 0 || len(c.calls) != 2 || c.calls[1].DryRun || !strings.Contains(c.prompt.String(), config.Text("gc.question")) {
		t.Fatalf("a pending recovery must be confirmed and run: exit %d, calls %+v\n%s\n%s", code, c.calls, out, errOut)
	}
	if !strings.Contains(out, config.Text("gc.log_recover", command.FormatBytes(2048))) || !strings.Contains(out, config.Text("gc.log_recovered", command.FormatBytes(2048))) {
		t.Fatalf("the plan and the result name the recovered tail:\n%s", out)
	}
}

// An interrupt while the prompt waits ends the command: nothing is removed.
func TestGCCommandStopsAtAnInterruptDuringThePrompt(t *testing.T) {
	f, w := removable(t)
	in, writer := io.Pipe()
	t.Cleanup(func() { writer.Close() })
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	previous := gcInterrupt
	t.Cleanup(func() { gcInterrupt = previous })
	gcInterrupt = func() (context.Context, context.CancelFunc) { return ctx, cancel }
	c := &gcCommand{f: f, skipped: "remote", promptIn: in, promptOut: writerFunc(func(p []byte) (int, error) {
		if bytes.Contains(p, []byte(config.Text("gc.question"))) {
			cancel()
		}
		return len(p), nil
	})}
	finished := make(chan int, 1)
	go func() {
		code, _, _ := c.run(t)
		finished <- code
	}()
	select {
	case code := <-finished:
		if code != 1 {
			t.Fatalf("an interrupted prompt: exit %d, want 1", code)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the command kept waiting for an answer after the interrupt")
	}
	if !f.registered(w.Path) {
		t.Fatal("an interrupted prompt must not remove the worktree")
	}
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

// A project that only sent collaboration requests from here is matched by its
// request-call records, and one whose directory holds no record at all by the
// administration's server gc plan.
func TestGCCommandAllProjectsMatchesRequestCallsAndThePlan(t *testing.T) {
	f := newGCFixture(t)
	requester := api.Project{ID: api.ProjectID("requester"), Name: "requester"}
	planned := api.Project{ID: api.ProjectID("planned"), Name: "planned"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/v1/projects" && r.URL.Query().Get("name") == planned.Name:
			_ = json.NewEncoder(w).Encode(planned)
		case r.URL.Path == "/api/v1/projects/"+requester.ID:
			_ = json.NewEncoder(w).Encode(requester)
		case r.URL.Path == "/api/v1/projects/"+planned.ID:
			_ = json.NewEncoder(w).Encode(planned)
		case strings.HasSuffix(r.URL.Path, "/tasks"):
			_ = json.NewEncoder(w).Encode(api.TaskList{})
		default:
			w.WriteHeader(http.StatusNotFound)
			_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeNotFound, Message: r.URL.Path})
		}
	}))
	t.Cleanup(server.Close)
	cl, err := client.New(server.URL, "c-gc-host")
	if err != nil {
		t.Fatal(err)
	}
	cl.Retries = 0
	if err := os.Remove(f.root); err != nil {
		t.Fatal(err)
	}
	f.s.ServerURL, f.s.Client = server.URL, cl
	if f.root, err = f.s.LocalDataRoot(); err != nil {
		t.Fatal(err)
	}
	call, _ := json.Marshal(requestCall{Schema: 1, ServerURL: server.URL, ProjectID: requester.ID, TaskID: gcTask, Phase: "complete"})
	f.write(filepath.Join(f.s.Home, "projects", projectNamespace(server.URL, requester.ID), "request-calls"), "a.json", string(call))
	f.write(filepath.Join(f.s.Home, "projects", projectNamespace(server.URL, planned.ID), "launch-actions"), "a.json", `{"schema":1,"phase":"complete","task_id":"`+gcTask+`"}`)
	c := &gcCommand{f: f, server: func(_ context.Context, req api.ServerGCRequest) (api.ServerGCReport, error) {
		return api.ServerGCReport{DryRun: req.DryRun, Projects: []api.ServerGCProject{{Name: "demo"}, {Name: planned.Name}}}, nil
	}}
	code, out, errOut := c.run(t, "--json", "--dry-run", "--all-projects")
	var report GCReport
	if err := json.Unmarshal([]byte(out), &report); err != nil || code != 0 {
		t.Fatalf("exit %d: %s %s", code, out, errOut)
	}
	var names []string
	for _, project := range report.Projects {
		names = append(names, project.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"demo", "planned", "requester"}) || len(report.Unmatched)+len(report.Unverified) != 0 {
		t.Fatalf("both directories are matched: %s", out)
	}
}

// The text plan gives every category its bytes, 0 B for the ref categories.
func TestGCCommandTextPlanShowsTheBytesOfEveryCategory(t *testing.T) {
	f, _ := removable(t)
	c := &gcCommand{f: f, skipped: "remote"}
	code, out, _ := c.run(t, "--dry-run")
	line := config.Text("gc.client_project_line", "demo", 1, "X", 1, command.FormatBytes(0), 0, command.FormatBytes(0), 0, command.FormatBytes(0), 0, command.FormatBytes(0), 0, command.FormatBytes(0))
	prefix, _, _ := strings.Cut(line, "X")
	suffix := line[strings.Index(line, "X")+1:]
	if code != 0 || !strings.Contains(out, prefix) || !strings.Contains(out, suffix) || strings.Contains(out, "{{") {
		t.Fatalf("the line must carry every byte count:\n%s", out)
	}
}
