package remote

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/config"
)

// stubProjectService answers the lookup of a project by name: "known" resolves, anything else is
// not_found. It counts every request, and fails the test on a registration.
func stubProjectService(t *testing.T) (url string, requests *atomic.Int32) {
	t.Helper()
	requests = new(atomic.Int32)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			t.Errorf("the read-only lookup sent %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("name") == "known" {
			_, _ = w.Write([]byte(`{"id":"project-a","name":"known"}`))
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"code":"not_found","message":"no such project"}`))
	}))
	t.Cleanup(server.Close)
	return server.URL, requests
}

func listAll(t *testing.T, dir string) []string {
	t.Helper()
	var names []string
	_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err == nil {
			names = append(names, strings.TrimPrefix(path, dir))
		}
		return nil
	})
	return names
}

// LookupHost only reads. A host with no client identity has nothing; an unknown project has
// nothing, however many receipts other projects hold; a known project is read under its own
// namespace by the exact key. Nothing is created and nothing is registered.
func TestLookupHostReadsOnlyAndScopesToTheProject(t *testing.T) {
	url, requests := stubProjectService(t)
	home := filepath.Join(t.TempDir(), "client")
	t.Setenv(EnvClientHome, home)
	cfg := &config.Config{Server: config.Server{URL: url}, Project: config.Project{Name: "known"}}

	if _, nothing, err := LookupHost(t.Context(), cfg); err != nil || !nothing {
		t.Fatalf("no client directory: nothing=%v %v", nothing, err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("the lookup created the client directory: %v", err)
	}
	if requests.Load() != 0 {
		t.Fatal("the lookup asked the service although the host has no identity")
	}

	// An existing client with a receipt in project A (the project of the service called "known").
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "client-id"), []byte("c-existing\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	session := &Session{Home: home, ServerURL: url, Info: api.Project{ID: "project-a"}}
	journal, _, err := session.BeginAction("task-launch", "input-a")
	if err != nil {
		t.Fatal(err)
	}
	_ = journal.Close()
	before := listAll(t, home)

	host, nothing, err := LookupHost(t.Context(), cfg)
	if err != nil || nothing {
		t.Fatalf("known project: nothing=%v %v", nothing, err)
	}
	if existing, found, err := host.PeekAction("task-launch", "input-a"); err != nil || !found || existing.Phase != "prepared" {
		t.Fatalf("the receipt of the project: %+v %v %v", existing, found, err)
	}
	if _, found, err := host.PeekAction("task-launch", "input-b"); err != nil || found {
		t.Fatalf("another key: %v %v", found, err)
	}

	// The same client, another project the service does not know: the receipt of project A is not
	// looked at, and the project stays unregistered.
	cfg.Project.Name = "other"
	if host, nothing, err := LookupHost(t.Context(), cfg); err != nil || !nothing || host != nil {
		t.Fatalf("unknown project: nothing=%v %v", nothing, err)
	}
	if after := listAll(t, home); strings.Join(before, "|") != strings.Join(after, "|") {
		t.Fatalf("the lookup changed the client directory: %v -> %v", before, after)
	}
}

// The gate of BeginActionGated runs under the lock with the receipt as it stands and before the
// receipt is written: a refusal leaves a receipt, and the absence of one, exactly as it was, and
// releases the lock. The directories and the lock file are created before the gate runs.
func TestBeginActionGatedRefusesBeforeTheReceiptIsWritten(t *testing.T) {
	session := &Session{Home: t.TempDir(), ServerURL: "http://127.0.0.1:1", Info: api.Project{ID: "p"}}
	refuse := errors.New("refused")
	var seen []*LocalAction
	gate := func(existing *LocalAction) error {
		seen = append(seen, existing)
		return refuse
	}
	if _, _, err := session.BeginActionGated("task-launch", "input", true, gate); !errors.Is(err, refuse) {
		t.Fatalf("fresh: %v", err)
	}
	if seen[0] != nil {
		t.Fatalf("fresh receipt seen as %+v", seen[0])
	}
	if _, found, err := session.PeekAction("task-launch", "input"); err != nil || found {
		t.Fatalf("a refused fresh call wrote a receipt: %v %v", found, err)
	}
	// An existing failed receipt is kept as it is, although BeginAction would start over it.
	journal, _, err := session.BeginAction("task-launch", "input")
	if err != nil {
		t.Fatal(err)
	}
	journal.Value.Phase = "failed"
	if err := journal.Save(); err != nil {
		t.Fatal(err)
	}
	_ = journal.Close()
	before, _ := os.ReadFile(journal.Path)
	if _, _, err := session.BeginActionGated("task-launch", "input", true, gate); !errors.Is(err, refuse) {
		t.Fatalf("failed: %v", err)
	}
	if seen[1] == nil || seen[1].Phase != "failed" {
		t.Fatalf("failed receipt seen as %+v", seen[1])
	}
	if after, _ := os.ReadFile(journal.Path); string(after) != string(before) {
		t.Fatalf("the refusal rewrote the failed receipt:\n%s\n%s", before, after)
	}
	// The lock was released: a call without the gate proceeds.
	again, fresh, err := session.BeginAction("task-launch", "input")
	if err != nil || !fresh {
		t.Fatalf("after the refusal: %v %v", fresh, err)
	}
	_ = again.Close()
}

// PeekAction and BeginAction take the name and the checks of a receipt from one place: a damaged
// receipt is refused by both, and Peek creates nothing.
func TestPeekActionCreatesNothingAndChecksLikeBegin(t *testing.T) {
	session := &Session{Home: t.TempDir(), ServerURL: "http://127.0.0.1:1", Info: api.Project{ID: "p"}}
	if _, found, err := session.PeekAction("task-launch", "input"); err != nil || found {
		t.Fatalf("%v %v", found, err)
	}
	if names := listAll(t, session.Home); len(names) > 1 {
		t.Fatalf("the peek created %v", names)
	}
	journal, _, err := session.BeginAction("task-launch", "input")
	if err != nil {
		t.Fatal(err)
	}
	_ = journal.Close()
	// A receipt without a valid execution identity is damaged for both readers.
	if err := os.WriteFile(journal.Path, []byte(`{"schema":1,"kind":"task-launch","execution_id":"","phase":"complete"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := session.PeekAction("task-launch", "input"); err == nil {
		t.Fatal("peek accepted a receipt without an execution identity")
	}
	if _, _, err := session.BeginAction("task-launch", "input"); err == nil {
		t.Fatal("begin accepted a receipt without an execution identity")
	}
}
