package board

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func legacyMigrationCard(t *testing.T, root, state, id string, directory bool, text string) string {
	t.Helper()
	path := filepath.Join(root, state, id+".md")
	if directory {
		path = filepath.Join(root, state, id, "spec.md")
		if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestDirectorySizeAndCompletionContract(t *testing.T) {
	root := tempBoard(t)
	for _, large := range []bool{false, true} {
		s := transactionCard(t, root, fmt.Sprintf("size-%v", large), large)
		want := "small"
		if large {
			want = "large"
		}
		if !s.Entry.IsDirectory() || s.Entry.Kind != want || !strings.Contains(s.Text, "- TYPE: Chore\n- SIZE: "+want+"\n") {
			t.Fatalf("wrong form/size: %+v", s)
		}
		if _, ok := SectionBody(s.Text, SectionSummary); ok == large {
			t.Fatal("template completion sections disagree with SIZE")
		}
		text := strings.Replace(readyText(s), "CARD_REVIEW: 通过\n", "", 1)
		err := validateTarget(s.Entry, "todo", text)
		if (err != nil) != large {
			t.Fatalf("independent card review size=%s err=%v", want, err)
		}
		text = strings.Replace(text, "- TASK_GROUP:\n", "- TASK_GROUP: 20260907-size-group\n", 1)
		if err := validateTarget(s.Entry, "todo", text); err == nil {
			t.Fatal("group card omitted independent review")
		}
		text = strings.Replace(readyText(s), "- RESULT:\n", "- RESULT: completed\n", 1)
		text = strings.ReplaceAll(text, Placeholder, "完成")
		err = validateTarget(s.Entry, "done", text)
		if (err != nil) != large {
			t.Fatalf("completion size=%s err=%v", want, err)
		}
		if large {
			updateSnapshot(t, root, s, readyText(s))
			s = transactionSnapshot(t, root, s.Entry.TaskID)
			if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "report.md", Text: "完成报告", ExpectedRevision: s.Revision}); err != nil {
				t.Fatal(err)
			}
			if err := validateTarget(s.Entry, "done", text); err != nil {
				t.Fatal(err)
			}
		}
	}
	s := transactionCard(t, root, "freeze-size", false)
	updateSnapshot(t, root, s, readyText(s))
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	e, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, e.TaskID)
	if err := UpdateDocument(root, e.TaskID, UpdateOptions{Document: "spec.md", Text: strings.Replace(s.Text, "SIZE: small", "SIZE: large", 1), ExpectedRevision: s.Revision}); err == nil {
		t.Fatal("SIZE changed after todo")
	}
}

func TestMigrationAllStatesAndIdempotence(t *testing.T) {
	root := tempBoard(t)
	original := map[string]string{}
	for i, state := range States {
		id := fmt.Sprintf("20260907-legacy-%d-task", i)
		text := "# 原文\r\n\r\n- 类型: Chore\r\n- 任务组:\r\n\r\n## 讨论与决策\r\n\r\n[附件](notes.txt)\r\n"
		legacyMigrationCard(t, root, state, id, false, text)
		original[id] = text
	}
	dirID := "20260907-legacy-directory-task"
	path := legacyMigrationCard(t, root, "done", dirID, true, "# Directory\n- TYPE: Feature\n[notes](notes.txt)\n")
	attachment := filepath.Join(filepath.Dir(path), "notes.txt")
	if err := os.WriteFile(attachment, []byte("附件不可丢失"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateCards(root, InitOptions{}); err == nil {
		t.Fatal("active execution silently migrated")
	}
	for id, text := range original {
		s := transactionSnapshot(t, root, id)
		if s.Text != text || s.Entry.IsDirectory() || s.Entry.Kind != "small" {
			t.Fatal("preflight changed original")
		}
	}
	n, err := MigrateCards(root, InitOptions{Maintenance: true})
	if err != nil || n != 8 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	mtimes := map[string]time.Time{}
	for id, text := range original {
		s := transactionSnapshot(t, root, id)
		if !s.Entry.IsDirectory() || s.Entry.Kind != "small" || s.Text != addSize(strings.ReplaceAll(text, "(notes.txt)", "(../notes.txt)"), "small") {
			t.Fatalf("lost content %+v", s)
		}
		if strings.Replace(strings.Replace(s.Text, "- SIZE: small\r\n", "", 1), "(../notes.txt)", "(notes.txt)", 1) != text {
			t.Fatal("migration changed bytes outside SIZE and destination")
		}
		info, err := os.Stat(s.Entry.Document)
		if err != nil {
			t.Fatal(err)
		}
		mtimes[s.Entry.Document] = info.ModTime()
		if verdict, err := GuardWrite(root, s.Entry.Path+".md"); err != nil || verdict.Allowed || !strings.Contains(verdict.Reason, id) {
			t.Fatalf("stale spelling: %+v %v", verdict, err)
		}
		if verdict, err := GuardWrite(root, filepath.Join(s.Entry.Path, "new-note.md")); err != nil || !verdict.Allowed {
			t.Fatalf("attachment: %+v %v", verdict, err)
		}
	}
	d := transactionSnapshot(t, root, dirID)
	if d.Entry.Kind != "large" {
		t.Fatal("legacy directory size")
	}
	data, err := os.ReadFile(attachment)
	if err != nil || string(data) != "附件不可丢失" {
		t.Fatal("attachment lost")
	}
	info, _ := os.Stat(path)
	mtimes[path] = info.ModTime()
	n, err = MigrateCards(root, InitOptions{})
	if err != nil || n != 0 {
		t.Fatalf("second init %d %v", n, err)
	}
	for path, mtime := range mtimes {
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Equal(mtime) {
			t.Fatalf("mtime changed: %s", path)
		}
	}
}

func TestLegacyReadOnlyAndSizeDiagnostics(t *testing.T) {
	root := tempBoard(t)
	fileID := "20260907-read-legacy-task"
	legacyMigrationCard(t, root, "backlog", fileID, false, "# Legacy\n- TYPE: Feature\n")
	s := transactionSnapshot(t, root, fileID)
	if err := UpdateDocument(root, fileID, UpdateOptions{Document: "spec.md", Text: s.Text + "edit", ExpectedRevision: s.Revision}); err == nil || !strings.Contains(err.Error(), "init") {
		t.Fatalf("legacy update %v", err)
	}
	if _, err := MoveWithOptions(s.Entry, root, "trash", MoveOptions{Result: "trashed", Reason: "test", Decision: "test"}); err == nil || !strings.Contains(err.Error(), "init") {
		t.Fatalf("legacy move %v", err)
	}
	if _, err := FormatList(mustScan(t, root), "", false); err != nil {
		t.Fatal(err)
	}
	if code, _, _, err := CheckBoard(root, []string{fileID}, false); err != nil || code != 0 {
		t.Fatalf("legacy read check %d %v", code, err)
	}
	dirID := "20260907-missing-size-task"
	legacyMigrationCard(t, root, "done", dirID, true, "# Legacy directory\n")
	if code, _, _, err := CheckBoard(root, nil, false); err != nil || code != 0 {
		t.Fatalf("default scope expanded %d %v", code, err)
	}
	if code, _, messages, err := CheckBoard(root, []string{dirID}, false); err != nil || code != 1 || !strings.Contains(strings.Join(messages, ""), "SIZE") {
		t.Fatalf("missing diagnostic %d %v %v", code, messages, err)
	}
	for i, size := range []string{"", "medium", "Small", "small\n- SIZE: large"} {
		id := fmt.Sprintf("20260907-invalid-%d-task", i)
		legacyMigrationCard(t, root, "backlog", id, true, "# Invalid\n- SIZE: "+size+"\n")
		s := transactionSnapshot(t, root, id)
		if s.Entry.Kind != "invalid" {
			t.Fatalf("invalid accepted %+v", s)
		}
		if err := ValidateMutable(s.Entry, s.Text); err == nil {
			t.Fatal("invalid mutable")
		}
		if err := UpdateDocument(root, id, UpdateOptions{Document: "plan.md", Text: "x", ExpectedRevision: s.Revision}); err == nil {
			t.Fatal("invalid attachment mutation")
		}
	}
	if _, err := MigrateCards(root, InitOptions{}); err == nil {
		t.Fatal("invalid SIZE migrated")
	}
}
func mustScan(t *testing.T, root string) Board {
	t.Helper()
	b, err := Scan(root)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

var migrationStages = []string{"prepared", "migration-stage", "migration-source", "migration-write-created", "migration-write-synced", "migration-original-saved", "migration-write-published", "migration-original-removed", "migration-size", "migration-publish", "revision", "committed"}

func migrationRecord(t *testing.T, root string) OperationRecord {
	t.Helper()
	id := "20260907-migration-crash-task"
	before := "# Crash\n- TYPE: Chore\nbody\n"
	legacyMigrationCard(t, root, "backlog", id, false, before)
	if err := ensureControl(root); err != nil {
		t.Fatal(err)
	}
	r := OperationRecord{Schema: 1, Purpose: "migration", ID: "migration-crash", Phase: "prepared", Revisions: map[string]uint64{id: 1}, Migrations: []FormMigration{{From: filepath.Join("backlog", id+".md"), To: filepath.Join("backlog", id), Before: before, After: addSize(before, "small"), Rewrite: "spec.write-migration-crash", Original: "spec.original-migration-crash"}}}
	if err := writeOperation(root, control(root, "operations", r.ID+".json"), r, false); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestMigrationFailureRecovery(t *testing.T) {
	for _, stage := range migrationStages {
		t.Run(stage, func(t *testing.T) {
			root := tempBoard(t)
			r := migrationRecord(t, root)
			injected := errors.New("injected migration failure")
			err := applyRecordWithCheckpoint(root, control(root, "operations", r.ID+".json"), &r, func(at string) error {
				if at == stage {
					return injected
				}
				return nil
			})
			if !errors.Is(err, injected) {
				t.Fatalf("checkpoint %s: %v", stage, err)
			}
			assertMigrationRecovery(t, root, stage)
		})
	}
}
func assertMigrationRecovery(t *testing.T, root, stage string) {
	t.Helper()
	id := "20260907-migration-crash-task"
	if stage != "committed" {
		if _, err := Scan(root); err == nil || !strings.Contains(err.Error(), "init") {
			t.Fatalf("scan did not diagnose recovery: %v", err)
		}
		if _, err := ScanTargets(root, []string{id}); err == nil || !strings.Contains(err.Error(), "init") {
			t.Fatalf("target did not diagnose recovery: %v", err)
		}
	}
	if _, err := MigrateCards(root, InitOptions{}); err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, id)
	if !s.Entry.IsDirectory() || s.Entry.Kind != "small" || s.Text != "# Crash\n- TYPE: Chore\n- SIZE: small\nbody\n" || s.Revision != 1 {
		t.Fatalf("bad recovered snapshot %+v", s)
	}
	b := mustScan(t, root)
	if len(b.Entries) != 1 || len(b.Problems) != 0 {
		t.Fatal("duplicate or incomplete recovery")
	}
	if n, err := MigrateCards(root, InitOptions{}); err != nil || n != 0 {
		t.Fatalf("non-idempotent %d %v", n, err)
	}
}

func TestMigrationCrashChild(t *testing.T) {
	root := os.Getenv("KANDER_TEST_MIGRATION_ROOT")
	if root == "" {
		return
	}
	stage := os.Getenv("KANDER_TEST_MIGRATION_STAGE")
	locks, err := acquire(root, LockScope{ExclusiveBoard: true})
	if err != nil {
		t.Fatal(err)
	}
	defer locks.close()
	records, err := operationRecords(root)
	if err != nil {
		t.Fatal(err)
	}
	r := records[0]
	if err := applyRecordWithCheckpoint(root, control(root, "operations", r.ID+".json"), &r, func(at string) error {
		if at == stage {
			crashBoundary()
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
func TestMigrationKillRestart(t *testing.T) {
	for _, stage := range migrationStages {
		t.Run(stage, func(t *testing.T) {
			root := tempBoard(t)
			migrationRecord(t, root)
			cmd := exec.Command(os.Args[0], "-test.run=^TestMigrationCrashChild$")
			cmd.Env = append(os.Environ(), "KANDER_TEST_MIGRATION_ROOT="+root, "KANDER_TEST_MIGRATION_STAGE="+stage)
			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = cmd.Process.Kill() })
			ready := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				ready <- scanner.Scan() && scanner.Text() == "crash-boundary"
			}()
			select {
			case ok := <-ready:
				if !ok {
					t.Fatal("child failed before checkpoint")
				}
			case <-time.After(10 * time.Second):
				t.Fatal("child checkpoint timeout")
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			_ = cmd.Wait()
			assertMigrationRecovery(t, root, stage)
		})
	}
}

func TestMigrationUnknownAndConflictingArtifactsPreserved(t *testing.T) {
	root := tempBoard(t)
	r := migrationRecord(t, root)
	target := filepath.Join(root, r.Migrations[0].To)
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateCards(root, InitOptions{}); err == nil {
		t.Fatal("true duplicate recovered")
	}
	if _, err := os.Stat(filepath.Join(root, r.Migrations[0].From)); err != nil {
		t.Fatal("source lost")
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("conflict removed")
	}
	root = tempBoard(t)
	if err := ensureControl(root); err != nil {
		t.Fatal(err)
	}
	unknown := control(root, "migrations", "unrecorded")
	if err := os.Mkdir(unknown, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := MigrateCards(root, InitOptions{}); err == nil {
		t.Fatal("unknown stage ignored")
	}
	if _, err := os.Stat(unknown); err != nil {
		t.Fatal("unknown stage removed")
	}
}

func TestMigrationSerializesReadersAndWriters(t *testing.T) {
	root := tempBoard(t)
	legacyMigrationCard(t, root, "backlog", "20260907-concurrent-legacy-task", false, "# Legacy\n")
	current := transactionCard(t, root, "concurrent-current", false)
	locks, err := acquire(root, LockScope{ExclusiveBoard: true})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	started := make(chan struct{}, 5)
	results := make(chan error, 5)
	actions := []func() error{
		func() error { _, e := MigrateCards(root, InitOptions{}); return e },
		func() error { _, e := MigrateCards(root, InitOptions{}); return e },
		func() error { _, e := Scan(root); return e },
		func() error {
			return UpdateDocument(root, current.Entry.TaskID, UpdateOptions{Document: "plan.md", Text: "记录", ExpectedRevision: current.Revision})
		},
		func() error {
			_, e := MoveWithOptions(current.Entry, root, "archived", MoveOptions{Result: "cancelled", Reason: "测试", Decision: "测试明确终止"})
			return e
		},
	}
	for _, action := range actions {
		wg.Add(1)
		go func(action func() error) { defer wg.Done(); started <- struct{}{}; results <- action() }(action)
	}
	for range actions {
		<-started
	}
	select {
	case e := <-results:
		t.Fatalf("maintenance lock bypass: %v", e)
	case <-time.After(50 * time.Millisecond):
	}
	if err := locks.close(); err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	close(results)
	failures := 0
	for err := range results {
		if err != nil {
			failures++
			if !strings.Contains(err.Error(), "revision") && !strings.Contains(err.Error(), "版本") {
				t.Fatal(err)
			}
		}
	}
	if failures > 1 {
		t.Fatalf("unexpected failures %d", failures)
	}
	b := mustScan(t, root)
	if len(b.Entries) != 2 || len(b.Problems) != 0 {
		t.Fatalf("bad concurrent result %+v", b)
	}
}

func TestMigrationStagingRejectsSymlink(t *testing.T) {
	root := tempBoard(t)
	r := migrationRecord(t, root)
	stageParent := control(root, "migrations", r.ID)
	if err := os.Mkdir(stageParent, 0700); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()
	marker := filepath.Join(outside, "spec.md")
	if err := os.WriteFile(marker, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	stage := filepath.Join(stageParent, filepath.Base(r.Migrations[0].To))
	if err := os.Symlink(outside, stage); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := MigrateCards(root, InitOptions{}); err == nil {
		t.Fatal("staging symlink followed")
	}
	data, err := os.ReadFile(marker)
	if err != nil || string(data) != "outside" {
		t.Fatal("outside changed")
	}
	if _, err := os.Lstat(stage); err != nil {
		t.Fatal("symlink removed")
	}
}

func TestOldWindowSnapshotCannotResurrectAfterFormMigration(t *testing.T) {
	root := tempBoard(t)
	id := "20260907-old-form-cursor-task"
	old := legacyMigrationCard(t, root, "working", id, false, "# Old\n- WINDOW: old\n")
	s := transactionSnapshot(t, root, id)
	if _, err := MigrateCards(root, InitOptions{Maintenance: true}); err != nil {
		t.Fatal(err)
	}
	if err := RollbackDocument(root, s.Entry, s.Text, ""); err == nil {
		t.Fatal("old file snapshot rollback succeeded")
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old file resurrected: %v", err)
	}
	current := transactionSnapshot(t, root, id)
	if !current.Entry.IsDirectory() || current.Text != addSize(s.Text, "small") {
		t.Fatal("migration overwritten")
	}
}

func TestDisplaySnapshotRemainsReadableAcrossMigration(t *testing.T) {
	root := tempBoard(t)
	id := "20260907-snapshot-migration-task"
	text := "# Original\n- TYPE: Chore\n"
	legacyMigrationCard(t, root, "backlog", id, false, text)
	scanned := mustScan(t, root)
	if n, err := MigrateCards(root, InitOptions{}); err != nil || n != 1 {
		t.Fatalf("migrate %d %v", n, err)
	}
	body, err := scanned.Document(id)
	if err != nil || body != text {
		t.Fatalf("captured body %q %v", body, err)
	}
	if _, err := ReadDocument(scanned.Entries[id]); err == nil {
		t.Fatal("stale mutation cursor accepted")
	}
	if list, err := FormatList(scanned, "", true); err != nil || !strings.Contains(list, "Original") || !strings.Contains(list, "small") {
		t.Fatalf("display snapshot %s %v", list, err)
	}
	payload, err := TaskPayload(root, id)
	if err != nil || payload.Kind != "small" || !strings.Contains(payload.Document, "SIZE: small") {
		t.Fatalf("fresh payload %+v %v", payload, err)
	}
	if summary := TaskSummaryOf(Entry{Kind: "large"}, "- SIZE: small\n"); summary.Kind != "small" {
		t.Fatal("summary inferred size from entry form")
	}
}

func TestInitCommandMaintenanceAndMigrationCount(t *testing.T) {
	resetLang(t)
	root := tempBoard(t)
	id := "20260907-init-maintenance-task"
	legacyMigrationCard(t, root, "review", id, false, "# Legacy\n")
	if code, _, message := capture(t, func() int { return RunInit(nil) }); code == 0 || !strings.Contains(message, "--maintenance") {
		t.Fatalf("default init %d %s", code, message)
	}
	if code, message, err := capture(t, func() int { return RunInit([]string{"--maintenance"}) }); code != 0 || !strings.Contains(message, "已迁移卡片：1") {
		t.Fatalf("maintenance init %d %s %s", code, message, err)
	}
	if code, message, err := capture(t, func() int { return RunInit(nil) }); code != 0 || !strings.Contains(message, "已迁移卡片：0") {
		t.Fatalf("repeat init %d %s %s", code, message, err)
	}
}

func TestSizeScanDoesNotExpandCheckScopeForUnreadableHistory(t *testing.T) {
	root := tempBoard(t)
	for _, state := range []string{"done", "archived"} {
		id := "20260907-" + state + "-invalid-utf8-task"
		legacyMigrationCard(t, root, state, id, true, string([]byte{0xff}))
	}
	if code, _, _, err := CheckBoard(root, nil, false); err != nil || code != 0 {
		t.Fatalf("historical UTF-8 expanded default check: %d %v", code, err)
	}
	if code, _, _, err := CheckBoard(root, nil, true); err == nil && code == 0 {
		t.Fatal("--all ignored unreadable history")
	}
}
