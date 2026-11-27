package remote

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/client"
)

// identityOf reads the identity marker of a client project directory.
func identityOf(t *testing.T, dir string) (projectIdentity, bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, projectIdentityName))
	if os.IsNotExist(err) {
		return projectIdentity{}, false
	}
	if err != nil {
		t.Fatal(err)
	}
	var identity projectIdentity
	if err := json.Unmarshal(data, &identity); err != nil {
		t.Fatal(err)
	}
	return identity, true
}

// The first LocalDataRoot of a project writes its identity marker; later and
// concurrent calls never rewrite it.
func TestLocalDataRootWritesTheIdentityMarkerOnce(t *testing.T) {
	home := t.TempDir()
	s := &Session{ServerURL: "http://127.0.0.1:1", Info: api.Project{ID: "p-identity"}, Home: home}
	root, err := s.LocalDataRoot()
	if err != nil {
		t.Fatal(err)
	}
	identity, found := identityOf(t, root)
	if !found || identity != (projectIdentity{Schema: 1, ServerURL: s.ServerURL, ProjectID: s.Info.ID}) {
		t.Fatalf("marker: %+v %v", identity, found)
	}
	path := filepath.Join(root, projectIdentityName)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			if _, err := s.LocalDataRoot(); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	after, _ := os.ReadFile(path)
	if err != nil || !info.ModTime().Equal(old) || string(after) != string(before) {
		t.Fatalf("the marker must not be rewritten: %v %v %q", err, info.ModTime(), after)
	}
	// Concurrent first calls of another project all succeed with one marker.
	other := &Session{ServerURL: s.ServerURL, Info: api.Project{ID: "p-identity-race"}, Home: home}
	errs = make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			if _, err := other.LocalDataRoot(); err != nil {
				errs <- err
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	if identity, found := identityOf(t, filepath.Join(home, "projects", projectNamespace(s.ServerURL, other.Info.ID))); !found || identity.ProjectID != other.Info.ID {
		t.Fatalf("marker: %+v %v", identity, found)
	}
}

// The identity marker names the directory's project only when its namespace
// is the directory's own.
func TestDirectoryProjectsReadsTheIdentityMarker(t *testing.T) {
	home := t.TempDir()
	const server = "http://127.0.0.1:1"
	mark := func(project, namespace string) {
		dir := filepath.Join(home, "projects", namespace)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(projectIdentity{Schema: 1, ServerURL: server, ProjectID: project})
		if err := os.WriteFile(filepath.Join(dir, projectIdentityName), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	own := projectNamespace(server, "p-marked")
	mark("p-marked", own)
	if ids, foreign, err := directoryProjects(home, server, own); err != nil || foreign || !slices.Equal(ids, []string{"p-marked"}) {
		t.Fatalf("a valid marker names its project: %v %v %v", ids, foreign, err)
	}
	misplaced := projectNamespace(server, "p-elsewhere")
	mark("p-marked", misplaced)
	if ids, foreign, err := directoryProjects(home, server, misplaced); err != nil || foreign || len(ids) != 0 {
		t.Fatalf("a marker of another namespace names nothing: %v %v %v", ids, foreign, err)
	}
}

// gcServedProject is a project of the fake server bindToServer starts. A
// status answers the project's read with that HTTP error.
type gcServedProject struct {
	info   api.Project
	status int
}

// bindToServer starts a server that serves the projects, each with one done
// card gcTask, and answers anything else not_found; then it binds the
// fixture's session to that server. requests lists the paths it was asked.
func (f *gcFixture) bindToServer(projects ...gcServedProject) (requests func() []string) {
	f.t.Helper()
	var mu sync.Mutex
	var seen []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		for _, p := range projects {
			base := "/api/v1/projects/" + p.info.ID
			switch {
			case r.URL.Path == base && p.status != 0:
				w.WriteHeader(p.status)
				_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeForbidden, Message: r.URL.Path})
				return
			case r.URL.Path == base:
				_ = json.NewEncoder(w).Encode(p.info)
				return
			case r.URL.Path == base+"/tasks":
				_ = json.NewEncoder(w).Encode(api.TaskList{Tasks: []api.TaskSummary{{TaskID: gcTask, State: "done", CompletedAt: gcOld}}})
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(api.Error{Code: api.CodeNotFound, Message: r.URL.Path})
	}))
	f.t.Cleanup(server.Close)
	cl, err := client.New(server.URL, "c-gc-host")
	if err != nil {
		f.t.Fatal(err)
	}
	cl.Retries = 0
	if err := os.RemoveAll(f.root); err != nil {
		f.t.Fatal(err)
	}
	f.s.ServerURL, f.s.Client = server.URL, cl
	if f.root, err = f.s.LocalDataRoot(); err != nil {
		f.t.Fatal(err)
	}
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(seen)
	}
}

// projectDirectory is the client project directory of project on the bound
// server, created with or without its identity marker.
func (f *gcFixture) projectDirectory(project string, marked bool) string {
	f.t.Helper()
	dir := filepath.Join(f.s.Home, "projects", projectNamespace(f.s.ServerURL, project))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		f.t.Fatal(err)
	}
	if marked {
		if err := writeProjectIdentity(f.s.Home, dir, f.s.ServerURL, project); err != nil {
			f.t.Fatal(err)
		}
	}
	return dir
}

// requestCall writes a terminal request-call record of gcTask older than the
// retention period into a client project directory.
func (f *gcFixture) requestCall(dir, project string) string {
	f.t.Helper()
	data, _ := json.Marshal(requestCall{Schema: 1, ServerURL: f.s.ServerURL, ProjectID: project, TaskID: gcTask, Phase: "complete", UpdatedAt: time.Now().Add(-30 * 24 * time.Hour)})
	f.write(filepath.Join(dir, "request-calls"), "a.json", string(data))
	return filepath.Join(dir, "request-calls", "a.json")
}

// allProjects runs `kander gc --json --all-projects --client-only` with the
// extra arguments and decodes its report.
func (f *gcFixture) allProjects(c *gcCommand, args ...string) (int, GCReport) {
	f.t.Helper()
	code, out, errOut := c.run(f.t, append([]string{"--json", "--all-projects", "--client-only"}, args...)...)
	var report GCReport
	if err := json.Unmarshal([]byte(out), &report); err != nil {
		f.t.Fatalf("exit %d: %v\n%s\n%s", code, err, out, errOut)
	}
	return code, report
}

func projectNames(report GCReport) []string {
	var names []string
	for _, project := range report.Projects {
		names = append(names, project.Name)
	}
	slices.Sort(names)
	return names
}

// A directory whose last record a run removes is matched again by its
// identity marker in the next run, also when the run itself wrote the marker
// because the directory predates markers.
func TestGCAllProjectsMatchesADirectoryAfterItsLastRecordWent(t *testing.T) {
	for _, marked := range []bool{true, false} {
		f := newGCFixture(t)
		peer := api.Project{ID: api.ProjectID("peer"), Name: "peer"}
		f.bindToServer(gcServedProject{info: peer})
		dir := f.projectDirectory(peer.ID, marked)
		record := f.requestCall(dir, peer.ID)
		for round := 1; round <= 2; round++ {
			code, report := f.allProjects(&gcCommand{f: f}, "--yes")
			if code != 0 || len(report.Unverified)+len(report.Unmatched) != 0 || !slices.Equal(projectNames(report), []string{"demo", "peer"}) {
				t.Fatalf("marked %v, round %d: exit %d: %+v", marked, round, code, report)
			}
			if identity, found := identityOf(t, dir); exists(t, record) || !found || identity.ProjectID != peer.ID || identity.ServerURL != f.s.ServerURL {
				t.Fatalf("marked %v, round %d: the record goes and the marker names the project: %+v %v", marked, round, identity, found)
			}
		}
	}
}

// A plan and a declined run write no marker.
func TestGCAllProjectsWritesNoMarkerWithoutARun(t *testing.T) {
	f := newGCFixture(t)
	peer := api.Project{ID: api.ProjectID("peer"), Name: "peer"}
	f.bindToServer(gcServedProject{info: peer})
	dir := f.projectDirectory(peer.ID, false)
	record := f.requestCall(dir, peer.ID)
	if code, report := f.allProjects(&gcCommand{f: f}, "--dry-run"); code != 0 || !slices.Equal(projectNames(report), []string{"demo", "peer"}) {
		t.Fatalf("exit %d: %+v", code, report)
	}
	if code, out, _ := (&gcCommand{f: f, answer: "n\n"}).run(t, "--all-projects", "--client-only"); code != 1 {
		t.Fatalf("a declined run exits 1: %d %s", code, out)
	}
	if _, found := identityOf(t, dir); found || !exists(t, record) {
		t.Fatal("neither a plan nor a declined run changes the directory")
	}
}

// When the marker cannot be written the run removes nothing below the
// directory and reports the failure.
func TestGCAllProjectsKeepsTheRecordsWhenTheMarkerCannotBeWritten(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions that bind the user")
	}
	f := newGCFixture(t)
	peer := api.Project{ID: api.ProjectID("peer"), Name: "peer"}
	f.bindToServer(gcServedProject{info: peer})
	dir := f.projectDirectory(peer.ID, false)
	record := f.requestCall(dir, peer.ID)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	code, report := f.allProjects(&gcCommand{f: f}, "--yes")
	if code != 1 || !exists(t, record) {
		t.Fatalf("exit %d: %+v", code, report)
	}
	i := slices.IndexFunc(report.Projects, func(p GCProject) bool { return p.ID == peer.ID })
	if i < 0 || len(report.Projects[i].Errors) != 1 || !strings.Contains(report.Projects[i].Errors[0], projectIdentityName) || report.Projects[i].Journal.Count != 0 {
		t.Fatalf("the marker failure is the project's error: %+v", report)
	}
}

// A directory that holds only a review runtime and its marker is matched
// without an administration plan, and its finished run is removed.
func TestGCAllProjectsMatchesADirectoryWithOnlyAReviewRuntime(t *testing.T) {
	f := newGCFixture(t)
	peer := api.Project{ID: api.ProjectID("peer"), Name: "peer"}
	f.bindToServer(gcServedProject{info: peer})
	dir := f.projectDirectory(peer.ID, true)
	run := filepath.Join(dir, "review-runtime", "run-gone")
	f.write(run, "runtime.lock", "")
	f.write(run, "writer-secret", strings.Repeat("a", 64))
	code, report := f.allProjects(&gcCommand{f: f}, "--yes")
	i := slices.IndexFunc(report.Projects, func(p GCProject) bool { return p.ID == peer.ID })
	if code != 0 || i < 0 || report.Projects[i].ReviewRuntime.Count != 1 || exists(t, run) {
		t.Fatalf("exit %d: %+v", code, report)
	}
}

// A marker is only a lead: a project the server refuses to show is unverified
// (exit 1) and one it does not have is unmatched, and neither directory
// changes. The marker is the only lead in both directories: a review runtime
// names no project.
func TestGCAllProjectsConfirmsTheMarkerWithTheServer(t *testing.T) {
	f := newGCFixture(t)
	refused, missing := api.Project{ID: api.ProjectID("refused"), Name: "refused"}, api.ProjectID("gone")
	requests := f.bindToServer(gcServedProject{info: refused, status: http.StatusForbidden})
	denied, gone := f.projectDirectory(refused.ID, true), f.projectDirectory(missing, true)
	for _, dir := range []string{denied, gone} {
		f.write(filepath.Join(dir, "review-runtime", "run-gone"), "writer-secret", strings.Repeat("a", 64))
	}
	before := map[string]map[string]string{denied: snapshot(t, denied), gone: snapshot(t, gone)}
	code, report := f.allProjects(&gcCommand{f: f}, "--yes")
	if code != 1 || !slices.Equal(projectNames(report), []string{"demo"}) {
		t.Fatalf("exit %d: %+v", code, report)
	}
	if len(report.Unverified) != 1 || report.Unverified[0].ID != filepath.Base(denied) || !slices.Equal(report.Unmatched, []string{filepath.Base(gone)}) {
		t.Fatalf("a refused project is unverified and a missing one unmatched: %+v", report)
	}
	for _, path := range []string{"/api/v1/projects/" + refused.ID, "/api/v1/projects/" + missing} {
		if !slices.Contains(requests(), path) {
			t.Fatalf("the marker's project must be read by ID (%s): %v", path, requests())
		}
	}
	for dir, files := range before {
		if !sameSnapshot(files, snapshot(t, dir)) {
			t.Fatalf("%s must not change", dir)
		}
	}
}

// An existing marker that names another project is left as it is, and
// neither the plan nor the run counts or removes anything below its
// directory, so the record that still names the project keeps the directory
// matchable.
func TestGCAllProjectsKeepsTheRecordsBehindAForeignMarker(t *testing.T) {
	f := newGCFixture(t)
	peer := api.Project{ID: api.ProjectID("peer"), Name: "peer"}
	f.bindToServer(gcServedProject{info: peer})
	dir := f.projectDirectory(peer.ID, false)
	if err := writeProjectIdentity(f.s.Home, dir, f.s.ServerURL, api.ProjectID("other")); err != nil {
		t.Fatal(err)
	}
	record := f.requestCall(dir, peer.ID)
	marker := filepath.Join(dir, projectIdentityName)
	before, _ := os.ReadFile(marker)
	files := snapshot(t, dir)
	code, plan := f.allProjects(&gcCommand{f: f}, "--dry-run")
	i := slices.IndexFunc(plan.Projects, func(p GCProject) bool { return p.ID == peer.ID })
	if code != 1 || i < 0 || len(plan.Projects[i].Errors) != 1 || !strings.Contains(plan.Projects[i].Errors[0], marker) || plan.Projects[i].Journal.Count != 0 || !sameSnapshot(files, snapshot(t, dir)) {
		t.Fatalf("the plan reports the marker and plans nothing below it: exit %d: %+v", code, plan)
	}
	for round := 1; round <= 2; round++ {
		code, report := f.allProjects(&gcCommand{f: f}, "--yes")
		i := slices.IndexFunc(report.Projects, func(p GCProject) bool { return p.ID == peer.ID })
		if code != 1 || i < 0 || len(report.Projects[i].Errors) != 1 || !strings.Contains(report.Projects[i].Errors[0], marker) || len(report.Unverified) != 0 {
			t.Fatalf("round %d: exit %d: %+v", round, code, report)
		}
		if after, _ := os.ReadFile(marker); !exists(t, record) || string(after) != string(before) {
			t.Fatalf("round %d: the record and the marker must stay", round)
		}
	}
}

// Without a client identity a directory that holds only its marker has no
// data of the project to report.
func TestGCWithoutAnIdentityIgnoresTheMarkerAlone(t *testing.T) {
	f := newGCFixture(t)
	opts := f.options(false)
	opts.Unidentified = true
	if result := f.s.CollectGarbage(t.Context(), opts); len(result.Errors) != 0 {
		t.Fatalf("result: %+v", result)
	}
}
