package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/command/commandtest"
)

func openIdentityServer(t *testing.T, dir string, logs io.Writer) *Server {
	t.Helper()
	if logs == nil {
		logs = io.Discard
	}
	s, err := Open(Options{DataDir: dir, Logger: slog.New(slog.NewTextHandler(logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func registerName(t *testing.T, s *Server, name string) api.Project {
	t.Helper()
	info, _, err := s.register(api.RegisterProject{Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return info
}

// treeSnapshot maps every regular file below root to its bytes.
func treeSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := os.ReadFile(path)
		files[path] = string(data)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func TestProjectIdentityIsTheSameOnEveryServer(t *testing.T) {
	var infos []api.Project
	for range 2 {
		dir := t.TempDir()
		s := openIdentityServer(t, dir, nil)
		info := registerName(t, s, "golf")
		s.Close()
		rel, err := filepath.Rel(dir, info.BoardPath)
		if err != nil {
			t.Fatal(err)
		}
		if want := filepath.Join("projects", "golf-625fe74cad4600b5", "board"); rel != want {
			t.Fatalf("board %s, want %s below the data directory", rel, want)
		}
		infos = append(infos, info)
	}
	if infos[0].ID != "p-625fe74cad4600b5" || infos[1].ID != infos[0].ID {
		t.Fatalf("IDs %s and %s", infos[0].ID, infos[1].ID)
	}
}

func TestRegistrationRefusesInvalidNamesWithTheRule(t *testing.T) {
	s := openIdentityServer(t, t.TempDir(), nil)
	defer s.Close()
	_, _, err := s.register(api.RegisterProject{Name: "go.lf"})
	if err == nil || !strings.Contains(err.Error(), api.ProjectNameRule) {
		t.Fatalf("err = %v", err)
	}
}

func TestMaintenanceRefusesInvalidNamesWithTheRule(t *testing.T) {
	_, err := Maintain(Options{DataDir: t.TempDir(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}, "1golf", false)
	if err == nil || !strings.Contains(err.Error(), api.ProjectNameRule) {
		t.Fatalf("err = %v", err)
	}
}

func TestRegistrationReusesAnEmptyLeftoverDirectory(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	first := registerName(t, s, "golf")
	s.Close()
	if err := os.Remove(filepath.Join(dir, "projects.json")); err != nil {
		t.Fatal(err)
	}
	s = openIdentityServer(t, dir, nil)
	defer s.Close()
	again := registerName(t, s, "golf")
	if again.ID != first.ID || again.BoardPath != first.BoardPath {
		t.Fatalf("leftover reuse gave %+v, want %+v", again, first)
	}
}

func TestRegistrationRefusesALeftoverDirectoryWithTasks(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	info := registerName(t, s, "golf")
	s.Close()
	if _, err := board.NewTask(info.BoardPath, "chore", "left", "Left behind", "en", false); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "projects.json")); err != nil {
		t.Fatal(err)
	}
	root := filepath.Dir(info.BoardPath)
	before := treeSnapshot(t, root)
	s = openIdentityServer(t, dir, nil)
	defer s.Close()
	_, _, err := s.register(api.RegisterProject{Name: "golf"})
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeProjectNotEmpty || !strings.Contains(err.Error(), root) {
		t.Fatalf("err = %v", err)
	}
	after := treeSnapshot(t, root)
	if len(after) != len(before) {
		t.Fatalf("directory changed: %d files before, %d after", len(before), len(after))
	}
	for path, data := range before {
		if after[path] != data {
			t.Fatalf("%s changed", path)
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, "projects.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("registry written after a refusal: %v", err)
	}
}

func TestRegistrationRefusesAnIDOwnedByAnotherName(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	defer s.Close()
	registerName(t, s, "golf")
	registry, err := os.ReadFile(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	original := projectIDFor
	projectIDFor = func(string) string { return api.ProjectID("golf") }
	t.Cleanup(func() { projectIDFor = original })
	_, _, err = s.register(api.RegisterProject{Name: "tennis"})
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeProjectMismatch {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "projects", api.ProjectDirName("tennis"))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("directory created for a refused name: %v", err)
	}
	after, err := os.ReadFile(filepath.Join(dir, "projects.json"))
	if err != nil || !bytes.Equal(after, registry) {
		t.Fatalf("registry changed: %v", err)
	}
}

func TestOpenRefusesRegistriesFromOlderBuilds(t *testing.T) {
	for name, entry := range map[string]api.Project{
		"random ID":    {ID: "p-945270e12a51f03a", Name: "golf"},
		"invalid name": {ID: api.ProjectID("go.lf"), Name: "go.lf"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := writeJSON(dir, "projects.json", registry{Projects: []api.Project{entry}}); err != nil {
				t.Fatal(err)
			}
			_, err := Open(Options{DataDir: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
			if err == nil || !strings.Contains(err.Error(), "remove the data directory and import the boards again") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRegistryNeverStoresTheBoardPath(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	defer s.Close()
	if info := registerName(t, s, "golf"); info.BoardPath == "" {
		t.Fatal("registration answered without a board path")
	}
	data, err := os.ReadFile(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "board_path") {
		t.Fatalf("projects.json stores the board path:\n%s", data)
	}
}

func TestProjectResponsesCarryTheBoardPath(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	defer s.Close()
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	c, err := client.New(h.URL, "test-client")
	if err != nil {
		t.Fatal(err)
	}
	c.Backoff = time.Millisecond
	want := filepath.Join(dir, "projects", "golf-625fe74cad4600b5", "board")
	registered, err := c.Register(t.Context(), "golf", "")
	if err != nil || registered.BoardPath != want {
		t.Fatalf("register: %+v, %v", registered, err)
	}
	found, err := c.ProjectByName(t.Context(), "golf")
	if err != nil || found.BoardPath != want || found.ID != registered.ID {
		t.Fatalf("lookup: %+v, %v", found, err)
	}
	_, err = c.ProjectByName(t.Context(), "tennis")
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeNotFound {
		t.Fatalf("lookup of a missing project: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "projects", api.ProjectDirName("tennis"))); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("lookup created a project directory: %v", err)
	}
}

func TestReadyLogNamesEveryProjectBoard(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	golf := registerName(t, s, "golf")
	tennis := registerName(t, s, "Tennis")
	s.Close()
	var logs bytes.Buffer
	openIdentityServer(t, dir, &logs).Close()
	for _, info := range []api.Project{golf, tennis} {
		line := "name=" + info.Name + " id=" + info.ID + " board=" + info.BoardPath
		if !strings.Contains(logs.String(), line) {
			t.Fatalf("log lacks %q:\n%s", line, logs.String())
		}
	}
}

func runProjectsCommand(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	// With --json the command layer writes the result to the process stdout.
	code, printed, reported := commandtest.Capture(func() int { return commandtest.Run(newProjectsCommand(&stdout, &stderr), args...) })
	return code, stdout.String() + printed, stderr.String() + reported
}

func TestProjectsCommandListsWhileServingAndStopped(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	info, _, err := s.register(api.RegisterProject{Name: "golf", Remote: "https://example.invalid/golf.git"})
	if err != nil {
		t.Fatal(err)
	}
	// Capture all bytes with no exclusive OS lock held. Windows refuses reads
	// of server.lock while serving; no file is excluded from the comparison.
	s.Close()
	before := treeSnapshot(t, dir)
	s = openIdentityServer(t, dir, nil)
	t.Cleanup(func() { _ = s.Close() })
	check := func(state string) {
		code, out, errOut := runProjectsCommand(t, "--data", dir, "--json")
		if code != 0 {
			t.Fatalf("%s: exit %d: %s", state, code, errOut)
		}
		var list projectList
		if err := json.Unmarshal([]byte(out), &list); err != nil || len(list.Projects) != 1 {
			t.Fatalf("%s: %v\n%s", state, err, out)
		}
		got := list.Projects[0]
		if got.Name != "golf" || got.ID != info.ID || got.Remote != info.Remote || !got.CreatedAt.Equal(info.CreatedAt) || got.BoardPath != info.BoardPath {
			t.Fatalf("%s: listed %+v, registered %+v", state, got, info)
		}
		code, out, errOut = runProjectsCommand(t, "--data", dir)
		if code != 0 {
			t.Fatalf("%s: exit %d: %s", state, code, errOut)
		}
		for _, field := range []string{"golf", info.ID, info.Remote, info.CreatedAt.Format(time.RFC3339), info.BoardPath} {
			if !strings.Contains(out, field) {
				t.Fatalf("%s: output lacks %q:\n%s", state, field, out)
			}
		}
	}
	check("serving")
	s.Close()
	check("stopped")
	after := treeSnapshot(t, dir)
	for path, data := range after {
		if old, ok := before[path]; !ok || old != data {
			t.Fatalf("listing changed %s", path)
		}
	}
	if len(after) != len(before) {
		t.Fatalf("listing changed the file set: %d before, %d after", len(before), len(after))
	}
}

func TestProjectsCommandWithoutDataListsNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	code, out, errOut := runProjectsCommand(t, "--data", dir, "--json")
	if code != 0 || strings.TrimSpace(out) != `{"projects":[]}` {
		t.Fatalf("exit %d: %q %q", code, out, errOut)
	}
	if _, err := os.Lstat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("listing created the data directory: %v", err)
	}
}

func TestProjectsCommandRefusesOlderRegistries(t *testing.T) {
	dir := t.TempDir()
	if err := writeJSON(dir, "projects.json", registry{Projects: []api.Project{{ID: "p-945270e12a51f03a", Name: "golf"}}}); err != nil {
		t.Fatal(err)
	}
	code, _, errOut := runProjectsCommand(t, "--data", dir)
	if code == 0 || !strings.Contains(errOut, "remove the data directory and import the boards again") {
		t.Fatalf("exit %d: %s", code, errOut)
	}
}

// A registration interrupted inside board initialization leaves a partial
// board; with no task in it, the next registration reuses the directory.
func TestRegistrationReusesAPartlyInitializedDirectory(t *testing.T) {
	dir := t.TempDir()
	partial := filepath.Join(dir, "projects", api.ProjectDirName("golf"), "board", "backlog")
	if err := os.MkdirAll(partial, 0o700); err != nil {
		t.Fatal(err)
	}
	s := openIdentityServer(t, dir, nil)
	defer s.Close()
	if info := registerName(t, s, "golf"); info.BoardPath != filepath.Dir(partial) {
		t.Fatalf("board %s, want %s", info.BoardPath, filepath.Dir(partial))
	}
}

// A restored board without control records is refused before any write, so
// the refusal creates no lock file or control directory in it.
func TestRegistrationRefusesARestoredBoardWithoutTouchingIt(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "projects", api.ProjectDirName("golf"))
	card := filepath.Join(root, "board", "todo", "20261114-left-task", "spec.md")
	if err := os.MkdirAll(filepath.Dir(card), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(card, []byte("# Left behind\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := openIdentityServer(t, dir, nil)
	defer s.Close()
	before := treeSnapshot(t, root)
	_, _, err := s.register(api.RegisterProject{Name: "golf"})
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeProjectNotEmpty {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(root, "board", ".kander")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refusal created control records: %v", err)
	}
	after := treeSnapshot(t, root)
	if len(after) != len(before) || after[card] != before[card] {
		t.Fatalf("refusal changed the directory: %v -> %v", before, after)
	}
}

const boundRemote = "https://github.com/example/golf.git"

func readRegistryText(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "projects.json"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestRegistrationBindsTheFirstRemoteOfAnUnboundProject(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	registerName(t, s, "golf")
	info, created, err := s.register(api.RegisterProject{Name: "golf", Remote: boundRemote})
	if err != nil || created || info.Remote != boundRemote {
		t.Fatalf("bind: %+v created=%v err=%v", info, created, err)
	}
	if !strings.Contains(readRegistryText(t, dir), boundRemote) {
		t.Fatal("projects.json does not store the bound remote")
	}
	s.Close()
	reopened := openIdentityServer(t, dir, nil)
	defer reopened.Close()
	found, err := reopened.projectByName("golf")
	if err != nil || found.Remote != boundRemote {
		t.Fatalf("after restart: %+v, %v", found, err)
	}
}

func TestRegistrationKeepsABoundRemote(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	defer s.Close()
	if _, _, err := s.register(api.RegisterProject{Name: "golf", Remote: boundRemote}); err != nil {
		t.Fatal(err)
	}
	before := readRegistryText(t, dir)
	if info, _, err := s.register(api.RegisterProject{Name: "golf", Remote: boundRemote}); err != nil || info.Remote != boundRemote {
		t.Fatalf("same remote: %+v, %v", info, err)
	}
	_, _, err := s.register(api.RegisterProject{Name: "golf", Remote: "https://github.com/example/tennis.git"})
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeProjectMismatch {
		t.Fatalf("another remote: %v", err)
	}
	if after := readRegistryText(t, dir); after != before {
		t.Fatalf("projects.json changed:\n%s\nwant\n%s", after, before)
	}
}

func TestRemoteBindingFailureKeepsTheProjectUnbound(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	defer s.Close()
	registerName(t, s, "golf")
	before := readRegistryText(t, dir)
	s.registryFault = func() error { return errors.New("injected registry write failure") }
	if _, _, err := s.register(api.RegisterProject{Name: "golf", Remote: boundRemote}); err == nil {
		t.Fatal("binding succeeded although projects.json could not be written")
	}
	found, err := s.projectByName("golf")
	if err != nil || found.Remote != "" {
		t.Fatalf("project after a failed binding: %+v, %v", found, err)
	}
	if after := readRegistryText(t, dir); after != before {
		t.Fatalf("projects.json changed:\n%s", after)
	}
}

func TestRemoteBindingRefusesAnUnsafeRemote(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	defer s.Close()
	registerName(t, s, "golf")
	_, _, err := s.register(api.RegisterProject{Name: "golf", Remote: "https://user:secret@github.com/example/golf.git"})
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeInvalidRequest {
		t.Fatalf("unsafe remote: %v", err)
	}
	if found, _ := s.projectByName("golf"); found.Remote != "" {
		t.Fatalf("unsafe remote bound: %+v", found)
	}
}

// Every client session registers the checkout's remote (remote.Open), so the
// next command run in a checkout binds the remote of an unbound project.
func TestClientRegistrationBindsTheRemoteOfAnUnboundProject(t *testing.T) {
	dir := t.TempDir()
	s := openIdentityServer(t, dir, nil)
	defer s.Close()
	h := httptest.NewServer(s.Handler())
	defer h.Close()
	c, err := client.New(h.URL, "test-client")
	if err != nil {
		t.Fatal(err)
	}
	c.Backoff = time.Millisecond
	if _, err := c.Register(t.Context(), "golf", ""); err != nil {
		t.Fatal(err)
	}
	bound, err := c.Register(t.Context(), "golf", boundRemote)
	if err != nil || bound.Remote != boundRemote {
		t.Fatalf("register with a remote: %+v, %v", bound, err)
	}
	found, err := c.ProjectByName(t.Context(), "golf")
	if err != nil || found.Remote != boundRemote {
		t.Fatalf("lookup: %+v, %v", found, err)
	}
}
