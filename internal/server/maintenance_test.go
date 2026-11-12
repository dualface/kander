package server

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

type legacyMaintenanceFixture struct {
	source, data, projectID, board string
	idle, working, evidence        string
	original                       []byte
	before                         map[string][]byte
	sourceDigest                   string
}

func maintenanceOptions(dir string) Options {
	return Options{DataDir: dir, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}
func newLegacyMaintenanceFixture(t *testing.T) *legacyMaintenanceFixture {
	t.Helper()
	root := t.TempDir()
	if err := board.InitStorage(root); err != nil {
		t.Fatal(err)
	}
	templateRoot := t.TempDir()
	if err := board.InitStorage(templateRoot); err != nil {
		t.Fatal(err)
	}
	template, err := board.NewTask(templateRoot, "chore", "legacy-template", "Legacy file card", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(template, "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.ReplaceAll(string(raw), "<FILL_IN>", "filled")
	text = strings.Replace(text, "- SIZE: small\n", "", 1)
	text = strings.Replace(text, "- TASK_BRANCH:", "- TASK_BRANCH: legacy-task", 1)
	text = strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nSELF_REVIEW: PASS\nCARD_REVIEW: PASS\n", 1)
	fixture := &legacyMaintenanceFixture{source: root, data: t.TempDir(), idle: "20261112-legacy-idle-task", working: "20261112-legacy-working-task"}
	for _, item := range []struct{ id, state string }{{fixture.idle, "backlog"}, {fixture.working, "working"}} {
		body := text
		if item.state == "working" {
			for _, field := range [][2]string{{board.FieldOwner, "claude"}, {board.FieldSession, "claude predecessor"}, {board.FieldWindow, "tmux:$1:@1:%1"}, {board.FieldStartedAt, time.Now().Format("2006-01-02 15:04")}} {
				body = strings.Replace(body, "- "+field[0]+":", "- "+field[0]+": "+field[1], 1)
			}
		}
		if err := os.WriteFile(filepath.Join(root, item.state, item.id+".md"), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	evidence, err := board.NewTask(root, "chore", "retained-evidence", "Retain original report", "en", false)
	if err != nil {
		t.Fatal(err)
	}
	fixture.evidence = filepath.Base(evidence)
	snapshot, err := board.ReadSnapshot(root, fixture.evidence)
	if err != nil {
		t.Fatal(err)
	}
	report := "# Original report\r\n\r\nExact original bytes: 日本語; evidence must remain unchanged.\r\n"
	if err := board.UpdateDocument(root, fixture.evidence, board.UpdateOptions{Document: "report.md", Text: report, ExpectedRevision: snapshot.Revision}); err != nil {
		t.Fatal(err)
	}
	fixture.original, err = os.ReadFile(filepath.Join(evidence, "report.md"))
	if err != nil {
		t.Fatal(err)
	}
	if err := board.RecoverTransactions(root); err != nil {
		t.Fatal(err)
	}
	_, fixture.sourceDigest, err = snapshotTree(root)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := Open(maintenanceOptions(fixture.data))
	if err != nil {
		t.Fatal(err)
	}
	imported, err := srv.Import("legacy", "", root)
	if err != nil {
		_ = srv.Close()
		t.Fatal(err)
	}
	fixture.projectID = imported.Project.ID
	project, err := srv.project(imported.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	fixture.board = project.board
	fixture.before = readMaintenanceTree(t, fixture.board)
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	return fixture
}
func readMaintenanceTree(t *testing.T, root string) map[string][]byte {
	t.Helper()
	paths, _, err := snapshotTree(root)
	if err != nil {
		t.Fatal(err)
	}
	result := map[string][]byte{}
	for _, path := range paths {
		result[path], err = os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}
	}
	return result
}
func (f *legacyMaintenanceFixture) checkOriginals(t *testing.T) {
	t.Helper()
	for _, root := range []string{f.source, f.board} {
		raw, err := os.ReadFile(filepath.Join(root, "backlog", f.evidence, "report.md"))
		if err != nil || string(raw) != string(f.original) {
			t.Fatalf("original report changed in %s: %v", root, err)
		}
	}
	_, digest, err := snapshotTree(f.source)
	if err != nil || digest != f.sourceDigest {
		t.Fatalf("source backup changed: %s %v", digest, err)
	}
}
func TestOfflineMaintenanceConvertsImportedFileCardsWithoutChangingOriginals(t *testing.T) {
	fixture := newLegacyMaintenanceFixture(t)
	srv, httpServer := openTest(t, fixture.data, nil)
	connection, err := client.New(httpServer.URL, "maintenance-reader")
	if err != nil {
		t.Fatal(err)
	}
	project := connection.Project(fixture.projectID)
	before, err := project.Show(t.Context(), fixture.idle)
	if err != nil {
		t.Fatal(err)
	}
	_, err = project.PutDocument(t.Context(), "before-maintenance", fixture.idle, api.Document{Path: "progress.md", Text: "progress", ExpectedRevision: before.Revision}, nil)
	var failure *api.Error
	if !errors.As(err, &failure) || failure.BoardCode != "board.migration_required" {
		t.Fatalf("legacy card was writable or failed for another reason: %v", err)
	}
	if _, err := Maintain(maintenanceOptions(fixture.data), "legacy", true); err == nil {
		t.Fatal("maintenance ran beside the serving instance")
	}
	httpServer.Close()
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := Maintain(maintenanceOptions(fixture.data), "legacy", false); err == nil {
		t.Fatal("active file migration skipped the maintenance acknowledgement")
	}
	result, err := Maintain(maintenanceOptions(fixture.data), "legacy", true)
	if err != nil || result.Project.ID != fixture.projectID || result.ChangedTasks != 2 || result.Snapshot == "" {
		t.Fatalf("explicit maintenance: %+v %v", result, err)
	}
	fixture.checkOriginals(t)
	again, err := Maintain(maintenanceOptions(fixture.data), "legacy", false)
	if err != nil || again.ChangedTasks != 0 {
		t.Fatalf("completed maintenance was repeated: %+v %v", again, err)
	}
	reopened, endpoint := openTest(t, fixture.data, nil)
	defer reopened.Close()
	defer endpoint.Close()
	connection, err = client.New(endpoint.URL, "new-host")
	if err != nil {
		t.Fatal(err)
	}
	project = connection.Project(fixture.projectID)
	after, err := project.Show(t.Context(), fixture.idle)
	if err != nil || after.Revision != before.Revision+1 {
		t.Fatalf("form revision: %+v %v", after, err)
	}
	if _, err := project.PutDocument(t.Context(), "after-maintenance", fixture.idle, api.Document{Path: "progress.md", Text: "New REST progress", ExpectedRevision: after.Revision}, nil); err != nil {
		t.Fatal(err)
	}
	grant, err := project.Takeover(t.Context(), "first-current-claim", fixture.working, api.ClaimRequest{ClientID: "new-host", ExecutionID: "explicit-after-maintenance", Owner: "codex", Decision: "user: take over the migrated legacy executor"})
	if err != nil || grant.Claim.Generation != 1 {
		t.Fatalf("migration granted ownership or prevented explicit takeover: %+v %v", grant, err)
	}
	fixture.checkOriginals(t)
}

func TestOfflineMaintenanceRecoversARealMigrationRedoBeforeReadiness(t *testing.T) {
	fixture := newLegacyMaintenanceFixture(t)
	if _, err := Maintain(maintenanceOptions(fixture.data), "legacy", true); err != nil {
		t.Fatal(err)
	}
	paths, err := filepath.Glob(filepath.Join(fixture.board, ".kander", "operations", "committed", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	var migration board.OperationRecord
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var record board.OperationRecord
		if err := json.Unmarshal(raw, &record); err != nil {
			t.Fatal(err)
		}
		if record.Purpose == "migration" {
			migration = record
		}
	}
	if len(migration.Migrations) != 2 {
		t.Fatalf("no actual migration redo: %+v", migration)
	}
	// Restore the before image and one real source-to-stage effect while the
	// service is closed, representing interruption inside its existing redo.
	if err := os.RemoveAll(fixture.board); err != nil {
		t.Fatal(err)
	}
	for path, data := range fixture.before {
		target := filepath.Join(fixture.board, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(target, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	migration.Phase = "prepared"
	data, err := json.Marshal(migration)
	if err != nil {
		t.Fatal(err)
	}
	recordPath := filepath.Join(fixture.board, ".kander", "operations", "pending", migration.ID+".json")
	if err := os.MkdirAll(filepath.Dir(recordPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(recordPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	first := migration.Migrations[0]
	stage := filepath.Join(fixture.board, ".kander", "migrations", migration.ID, filepath.Base(first.To), "spec.md")
	if err := os.MkdirAll(filepath.Dir(stage), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(fixture.board, first.From), stage); err != nil {
		t.Fatal(err)
	}
	if server, err := Open(maintenanceOptions(fixture.data)); err == nil {
		_ = server.Close()
		t.Fatal("normal startup bypassed an unfinished active migration")
	}
	result, err := Maintain(maintenanceOptions(fixture.data), "legacy", true)
	if err != nil || result.ChangedTasks != 2 {
		t.Fatalf("resume the exact prepared migration: %+v %v", result, err)
	}
	raw, err := os.ReadFile(filepath.Join(fixture.board, ".kander", "operations", "committed", migration.ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var committed board.OperationRecord
	if err := json.Unmarshal(raw, &committed); err != nil || committed.Phase != "committed" {
		t.Fatalf("original redo did not settle: %+v %v", committed, err)
	}
	fixture.checkOriginals(t)
}

func TestMaintainCommandUsesTheOfflineProducerAndReleasesItsLock(t *testing.T) {
	fixture := newLegacyMaintenanceFixture(t)
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	previous := os.Stdout
	os.Stdout = write
	code := Main([]string{"maintain", "--data", fixture.data, "--project", "legacy", "--maintenance"})
	os.Stdout = previous
	_ = write.Close()
	raw, err := io.ReadAll(read)
	_ = read.Close()
	if err != nil {
		t.Fatal(err)
	}
	var result MaintenanceResult
	if err := json.Unmarshal(raw, &result); err != nil || code != 0 || result.ChangedTasks != 2 || result.Project.ID != fixture.projectID {
		t.Fatalf("maintenance command: %s code=%d err=%v", raw, code, err)
	}
	service, err := Open(maintenanceOptions(fixture.data))
	if err != nil {
		t.Fatal("maintenance kept the server data lock: ", err)
	}
	defer service.Close()
	fixture.checkOriginals(t)
}
func TestOfflineMaintenanceInstanceNeverEnablesDomainRequests(t *testing.T) {
	fixture := newLegacyMaintenanceFixture(t)
	options := maintenanceOptions(fixture.data)
	options.maintenance = &maintenanceRequest{Project: "legacy", Acknowledged: true}
	service, err := Open(options)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	if service.ready.Load() {
		t.Fatal("offline maintenance became a serving instance")
	}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+fixture.projectID+"/tasks", nil)
	request.Header.Set(api.HeaderProtocol, "1")
	response := httptest.NewRecorder()
	service.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("offline domain read was served: %d %s", response.Code, response.Body.String())
	}
	fixture.checkOriginals(t)
}
