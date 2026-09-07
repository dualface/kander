package board

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/fs"
)

func transactionCard(t *testing.T, root, slug string, large bool) Snapshot {
	t.Helper()
	path, err := NewTask(root, "chore", slug, "测试事务", "zh-CN", large)
	if err != nil {
		t.Fatal(err)
	}
	id := strings.TrimSuffix(filepath.Base(path), ".md")
	s, err := ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func transactionSnapshot(t *testing.T, root, id string) Snapshot {
	t.Helper()
	s, err := ReadSnapshot(root, id)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func readyText(s Snapshot) string {
	text := s.Text
	for _, h := range readySections {
		body, _ := SectionBody(text, h)
		value := "已确定的任务范围"
		if h == SectionAcceptanceCriteria {
			value = "- [ ] 验证任务行为"
		}
		text = strings.Replace(text, body, value, 1)
	}
	text = strings.Replace(text, "## DISCUSSION", "## DISCUSSION\n\nSELF_REVIEW: 通过\nCARD_REVIEW: 通过", 1)
	return text
}
func updateSnapshot(t *testing.T, root string, s Snapshot, text string) {
	t.Helper()
	if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision}); err != nil {
		t.Fatal(err)
	}
}

func TestControlledSmallLifecycleAndContractDecisions(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "controlled-lifecycle", false)
	updateSnapshot(t, root, s, readyText(s))
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	todo, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, todo.TaskID)
	edited := strings.Replace(s.Text, "## GOAL\n\n已确定的任务范围", "## GOAL\n\n用户批准的新目标", 1)
	if err = UpdateDocument(root, todo.TaskID, UpdateOptions{Document: "spec.md", Text: edited, ExpectedRevision: s.Revision}); err == nil {
		t.Fatal("frozen contract changed without decision")
	}
	if err = UpdateDocument(root, todo.TaskID, UpdateOptions{Document: "spec.md", Text: edited, ExpectedRevision: s.Revision, ContractDecision: "用户在本轮明确批准改为新目标。"}); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, todo.TaskID)
	if !strings.Contains(s.Text, "CONTRACT_DECISIONS") || !strings.Contains(s.Text, "用户批准的新目标") {
		t.Fatal("decision or new goal missing")
	}
	working, err := MoveWithOptions(s.Entry, root, "working", MoveOptions{Owner: "codex"})
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, working.TaskID)
	if MetadataFrom(s.Text, FieldOwner) != "codex" || MetadataFrom(s.Text, FieldStartedAt) == "" {
		t.Fatal("claim metadata missing")
	}
	body := strings.Replace(s.Text, "## SUMMARY\n\n<FILL_IN>", "## SUMMARY\n\n已实现，验证通过。", 1)
	updateSnapshot(t, root, s, body)
	s = transactionSnapshot(t, root, working.TaskID)
	done, err := MoveWithOptions(s.Entry, root, "done", MoveOptions{Result: "completed"})
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, done.TaskID)
	if s.Entry.State != "done" || MetadataFrom(s.Text, FieldResult) != "completed" || MetadataFrom(s.Text, FieldFinishedAt) == "" {
		t.Fatal("completion was not atomic")
	}
}
func TestControlledTerminationAndProtectedFields(t *testing.T) {
	root := tempBoard(t)
	for _, result := range []string{"cancelled", "duplicate", "wontfix", "trashed"} {
		s := transactionCard(t, root, "terminate-"+result, false)
		state := "archived"
		if result == "trashed" {
			state = "trash"
		}
		if _, err := MoveWithOptions(s.Entry, root, state, MoveOptions{Result: result}); err == nil {
			t.Fatal("termination without authorization accepted")
		}
		options := MoveOptions{Result: result, Reason: "用户决定终止此项。", Decision: "本轮用户明确决定", DuplicateOf: "20260907-replacement-task"}
		moved, err := MoveWithOptions(s.Entry, root, state, options)
		if err != nil {
			t.Fatal(err)
		}
		current := transactionSnapshot(t, root, moved.TaskID)
		if MetadataFrom(current.Text, FieldResult) != result || !strings.Contains(current.Text, "LIFECYCLE_DECISION") {
			t.Fatal("termination record missing")
		}
	}
	s := transactionCard(t, root, "protected", true)
	for _, field := range managedFields {
		modified, err := setMetadata(s.Text, field, "forged")
		if err != nil {
			t.Fatal(err)
		}
		if err = UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: modified, ExpectedRevision: s.Revision}); err == nil {
			t.Fatalf("managed %s accepted", field)
		}
	}
	for _, name := range []string{"../escape.md", "/tmp/escape", "spec.md/../../escape", "reviews/index.json", "reviews/run/report.md", ".kander/checkpoint.json", "x\\spec.md", "x:stream", "a/../plan.md"} {
		if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: name, Text: "x", ExpectedRevision: s.Revision}); err == nil {
			t.Fatalf("unsafe %s accepted", name)
		}
	}
	for _, name := range []string{"plan.md", "report.md", "notes.txt"} {
		if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: name, Text: "内容", ExpectedRevision: s.Revision}); err != nil {
			t.Fatal(err)
		}
		s = transactionSnapshot(t, root, s.Entry.TaskID)
	}
	small := transactionCard(t, root, "no-small-attachment", false)
	if err := UpdateDocument(root, small.Entry.TaskID, UpdateOptions{Document: "report.md", Text: "x", ExpectedRevision: small.Revision}); err != nil {
		t.Fatal(err)
	}
}
func TestConcurrentNewAndUpdateMove(t *testing.T) {
	root := tempBoard(t)
	const workers = 12
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := NewTask(root, "chore", "same-id", "same", "en", false)
			results <- err
		}()
	}
	wg.Wait()
	close(results)
	success := 0
	for err := range results {
		if err == nil {
			success++
		}
	}
	if success != 1 {
		t.Fatalf("created %d copies", success)
	}
	s := transactionCard(t, root, "move-race", false)
	updateSnapshot(t, root, s, readyText(s))
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	if _, err := MoveEntry(s.Entry, root, "todo"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 20; i++ {
		s = transactionSnapshot(t, root, s.Entry.TaskID)
		state := "backlog"
		if s.Entry.State == "backlog" {
			state = "todo"
		}
		start := make(chan struct{})
		results = make(chan error, 2)
		go func(s Snapshot) {
			<-start
			results <- UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: s.Text + "\n执行记录\n", ExpectedRevision: s.Revision})
		}(s)
		go func(s Snapshot) { <-start; _, err := MoveEntry(s.Entry, root, state); results <- err }(s)
		close(start)
		a, b := <-results, <-results
		if a != nil && b != nil {
			t.Fatalf("both failed: %v, %v", a, b)
		}
		board, err := Scan(root)
		if err != nil || len(board.Problems) != 0 {
			t.Fatalf("duplicates: %+v %v", board, err)
		}
	}
}
func TestSortedBatchLocksAndConcurrentIndexAppend(t *testing.T) {
	root := tempBoard(t)
	a := transactionCard(t, root, "batch-a", true)
	b := transactionCard(t, root, "batch-b", true)
	const workers = 16
	errors := make(chan error, workers)
	for i := 0; i < workers; i++ {
		go func(i int) {
			ids := []string{a.Entry.TaskID, b.Entry.TaskID}
			if i%2 == 1 {
				ids[0], ids[1] = ids[1], ids[0]
			}
			errors <- WithTransaction(root, LockScope{Tasks: ids, Groups: []string{"20260907-batch-group"}}, func(tx *Transaction) error {
				for _, id := range ids {
					s, err := tx.Snapshot(id)
					if err != nil {
						return err
					}
					if err = tx.Put(id, "spec.md", s.Text+fmt.Sprintf("\nrecord-%d\n", i)); err != nil {
						return err
					}
				}
				return nil
			})
		}(i)
	}
	for i := 0; i < workers; i++ {
		select {
		case err := <-errors:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(20 * time.Second):
			t.Fatal("batch lock deadlock")
		}
	}
	for _, id := range []string{a.Entry.TaskID, b.Entry.TaskID} {
		s := transactionSnapshot(t, root, id)
		if strings.Count(s.Text, "record-") != workers {
			t.Fatal("lost concurrent append")
		}
	}
}
func TestUpdateRejectsReparseAndMissingEntry(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "unsafe-attachment", true)
	outside := filepath.Join(t.TempDir(), "outside.md")
	if err := os.WriteFile(outside, []byte("outside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.Entry.Path, "notes.md")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "notes.md", Text: "overwrite", ExpectedRevision: s.Revision}); err == nil {
		t.Fatal("reparse accepted")
	}
	body, _ := os.ReadFile(outside)
	if string(body) != "outside" {
		t.Fatal("outside overwritten")
	}
	missing := "20260907-missing-task"
	if err := UpdateDocument(root, missing, UpdateOptions{Document: "spec.md", Text: "recreated"}); err == nil {
		t.Fatal("missing card recreated")
	}
}

// The child is killed at persisted protocol boundaries without running deferred
// cleanup. Restart tests cover the exact redo bytes and all visibility gates.
func TestTransactionCrashChild(t *testing.T) {
	root := os.Getenv("KANDER_TEST_CRASH_ROOT")
	if root == "" {
		return
	}
	stage := os.Getenv("KANDER_TEST_CRASH_STAGE")
	id := "20260907-crash-task"
	locks, err := acquire(root, LockScope{Tasks: []string{id}, ExclusiveBoard: true})
	if err != nil {
		t.Fatal(err)
	}
	defer locks.close()
	before := "before\n"
	after := "after\n"

	r := OperationRecord{Schema: 1, ID: "crash-operation", Phase: "prepared", Revisions: map[string]uint64{id: 1},
		Directories: []string{filepath.Join("working", id, "reviews"), filepath.Join("working", id, "reviews", "run-1")},
		Files:       []FileChange{{Path: filepath.Join("working", id, "spec.md"), Before: &before, After: after}, {Path: filepath.Join("working", id, "reviews", "run-1", "report.md"), After: "report\n"}},
		Entries:     []EntryChange{{From: filepath.Join("working", id), To: filepath.Join("review", id), Kind: "large", Text: after}}}
	path := control(root, "operations", r.ID+".json")
	if err = writeJSON(root, path, r, false); err != nil {
		t.Fatal(err)
	}
	if stage == "prepared" {
		crashBoundary()
	}
	for _, directory := range r.Directories {
		if err = fs.CreatePrivateDirectory(root, filepath.Join(root, directory)); err != nil {
			t.Fatal(err)
		}
	}
	if stage == "directories" {
		crashBoundary()
	}
	if err = fs.WriteTextAtomic(root, filepath.Join(root, r.Files[0].Path), after, true); err != nil {
		t.Fatal(err)
	}
	if stage == "first-file" {
		crashBoundary()
	}
	if err = fs.WriteTextAtomic(root, filepath.Join(root, r.Files[1].Path), "report\n", false); err != nil {
		t.Fatal(err)
	}
	if stage == "files" {
		crashBoundary()
	}
	if err = fs.Rename(root, filepath.Join(root, r.Entries[0].From), filepath.Join(root, r.Entries[0].To)); err != nil {
		t.Fatal(err)
	}
	if stage == "rename" {
		crashBoundary()
	}
	if err = writeJSON(root, control(root, "versions", id+".json"), versionRecord{Revision: 1, OperationID: r.ID, ContractFrozen: true}, true); err != nil {
		t.Fatal(err)
	}
	if stage == "revision" {
		crashBoundary()
	}
	if err = applyRecord(root, path, &r); err != nil {
		t.Fatal(err)
	}
	crashBoundary()
}
func TestCrashRestartRecoveryAndReadVisibility(t *testing.T) {
	for _, stage := range []string{"prepared", "directories", "first-file", "files", "rename", "revision", "committed"} {
		t.Run(stage, func(t *testing.T) {
			root := tempBoard(t)
			id := "20260907-crash-task"

			original := filepath.Join(root, "working", id)
			if err := os.Mkdir(original, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(original, "spec.md"), []byte("before\n"), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(os.Args[0], "-test.run=^TestTransactionCrashChild$")
			cmd.Env = append(os.Environ(), "KANDER_TEST_CRASH_ROOT="+root, "KANDER_TEST_CRASH_STAGE="+stage)

			stdout, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			cmd.Stderr = os.Stderr
			if err = cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer func() {
				if cmd.ProcessState == nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
				}
			}()
			reached := make(chan bool, 1)
			go func() {
				scanner := bufio.NewScanner(stdout)
				reached <- scanner.Scan() && scanner.Text() == "crash-boundary"
			}()
			select {
			case ok := <-reached:
				if !ok {
					t.Fatal("child failed before crash boundary")
				}
			case <-time.After(15 * time.Second):
				t.Fatal("crash child timed out")
			}
			if err = cmd.Process.Kill(); err != nil {
				t.Fatal(err)
			}
			if err = cmd.Wait(); err == nil {
				t.Fatal("child was not killed")
			}

			if stage != "committed" {
				if _, err := ReadSnapshot(root, id); err == nil {
					t.Fatal("uncommitted body visible")
				}
				if _, err := Scan(root); err == nil {
					t.Fatal("scan exposed pending move")
				}
			}
			if _, _, _, _, err := InitBoardWithOptions("", InitOptions{Maintenance: true}); err != nil {
				t.Fatal(err)
			}
			if err := RecoverTransactions(root); err != nil {
				t.Fatal("recovery is not idempotent", err)
			}
			s := transactionSnapshot(t, root, id)
			if s.Entry.State != "review" || s.Revision != 2 || s.Text != "- SIZE: large\nafter\n" {
				t.Fatalf("bad recovery %+v", s)
			}
			report, err := fs.ReadRegularFile(root, filepath.Join(root, "review", id, "reviews", "run-1", "report.md"))
			if err != nil || string(report) != "report\n" {
				t.Fatalf("report %q %v", report, err)
			}
			if _, err = os.Stat(filepath.Join(root, "working", id)); !os.IsNotExist(err) {
				t.Fatal("source resurrected")
			}
		})
	}
}
func TestRecoveryPreservesConflictingCopies(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "conflicting-recovery", false)
	id := s.Entry.TaskID
	target := filepath.Join(root, "todo", id+".md")
	if err := os.WriteFile(target, []byte("new copy"), 0600); err != nil {
		t.Fatal(err)
	}
	record := OperationRecord{Schema: 1, ID: "conflict", Phase: "prepared", Revisions: map[string]uint64{id: s.Revision + 1}, Entries: []EntryChange{{From: filepath.Join("backlog", id+".md"), To: filepath.Join("todo", id+".md"), Kind: "small"}}}
	if err := writeJSON(root, control(root, "operations", "conflict.json"), record, false); err != nil {
		t.Fatal(err)
	}
	if err := RecoverTransactions(root); err == nil {
		t.Fatal("duplicate repaired by guessing")
	}
	for _, path := range []string{s.Entry.Path, target} {
		if _, err := os.Stat(path); err != nil {
			t.Fatal("copy removed", err)
		}
	}
}
func TestShowJSONAndUpdateCommand(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "json-update", false)
	code, out, stderr := capture(t, func() int { return RunShow([]string{s.Entry.TaskID, "--json"}) })
	if code != 0 {
		t.Fatal(stderr)
	}
	var decoded Snapshot
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Revision != s.Revision || decoded.Text != s.Text || decoded.Entry.Path != s.Entry.Path {
		t.Fatal("inconsistent JSON snapshot")
	}
	input := filepath.Join(t.TempDir(), "input.md")
	if err := os.WriteFile(input, []byte(readyText(s)), 0600); err != nil {
		t.Fatal(err)
	}
	args := []string{s.Entry.TaskID, "--document", "spec.md", "--file", input, "--expect-revision", fmt.Sprint(s.Revision)}
	if code, _, stderr = capture(t, func() int { return RunUpdate(args) }); code != 0 {
		t.Fatal(stderr)
	}
	if code, _, _ = capture(t, func() int { return RunUpdate(args) }); code == 0 {
		t.Fatal("stale command accepted")
	}
}

func crashBoundary() { fmt.Println("crash-boundary"); time.Sleep(time.Hour); os.Exit(78) }

func TestContractStaysFrozenAfterWithdrawal(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "withdrawal", false)
	updateSnapshot(t, root, s, readyText(s))
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	todo, err := MoveEntry(s.Entry, root, "todo")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = MoveEntry(todo, root, "backlog"); err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, s.Entry.TaskID)
	text := strings.Replace(s.Text, "## GOAL\n\n已确定的任务范围", "## GOAL\n\n未经授权的新目标", 1)
	if err = UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: text, ExpectedRevision: s.Revision}); err == nil {
		t.Fatal("withdrawal unfroze contract")
	}
}
func TestNestedPublicationAndGroupControl(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "nested-publication", true)
	group := "20260907-publication-group"
	err := WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}, Groups: []string{group}}, func(tx *Transaction) error {
		if err := tx.Put(s.Entry.TaskID, "reviews/run-1/report.md", "报告正文"); err != nil {
			return err
		}
		return tx.PutGroup(group, "checkpoint.json", `{"version":1}`)
	})
	if err != nil {
		t.Fatal(err)
	}
	err = WithTransaction(root, LockScope{Tasks: []string{s.Entry.TaskID}, Groups: []string{group}, ReadOnly: true}, func(tx *Transaction) error {
		report, err := tx.Read(s.Entry.TaskID, "reviews/run-1/report.md")
		if err != nil {
			return err
		}
		if report != "报告正文" {
			return fmt.Errorf("report mismatch")
		}
		body, exists, err := tx.ReadGroup(group, "checkpoint.json")
		if err != nil {
			return err
		}
		if !exists || string(body) != `{"version":1}` {
			return fmt.Errorf("checkpoint mismatch")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
func TestSpecAliasesAndDuplicateContractRejected(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "alias-spec", true)
	for _, name := range []string{"SPEC.MD", "spec.md.", "spec.md ", "reviews /index.json"} {
		if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: name, Text: "forged", ExpectedRevision: s.Revision}); err == nil {
			t.Fatalf("alias accepted: %q", name)
		}
	}
	for _, extra := range []string{"\n- TASK_GROUP: 20260907-forged-group\n- TASK_GROUP: 20260907-other-group\n", "\n## GOAL\n\nforged\n"} {
		if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: s.Text + extra, ExpectedRevision: s.Revision}); err == nil {
			t.Fatal("ambiguous contract accepted")
		}
	}
}
func TestIncompleteLargeCreationRecovers(t *testing.T) {
	root := tempBoard(t)
	id := "20260907-large-crash-task"
	text := "# Recovered\n"
	if err := ensureControl(root); err != nil {
		t.Fatal(err)
	}
	record := OperationRecord{Schema: 1, ID: "large-crash", Phase: "prepared", Revisions: map[string]uint64{id: 1}, Entries: []EntryChange{{To: filepath.Join("backlog", id), Kind: "large", Text: text}}}
	if err := writeJSON(root, control(root, "operations", record.ID+".json"), record, false); err != nil {
		t.Fatal(err)
	}
	if err := fs.CreatePrivateDirectory(root, filepath.Join(root, "backlog", id)); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshot(root, id); err == nil {
		t.Fatal("partial card treated as committed")
	}
	if err := RecoverTransactions(root); err != nil {
		t.Fatal(err)
	}
	s := transactionSnapshot(t, root, id)
	if s.Text != text || s.Revision != 1 {
		t.Fatalf("incomplete create: %+v", s)
	}
}

func TestStaleRecoveryDoesNotOverwriteNewRevision(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "stale-recovery", false)
	before := s.Text
	stale := OperationRecord{Schema: 1, ID: "stale-operation", Phase: "prepared", Revisions: map[string]uint64{s.Entry.TaskID: s.Revision + 1}, Files: []FileChange{{Path: filepath.Join("backlog", s.Entry.TaskID, "spec.md"), Before: &before, After: "obsolete content"}}}
	updateSnapshot(t, root, s, s.Text+"\nnew record\n")
	if err := writeJSON(root, control(root, "operations", "stale-operation.json"), stale, false); err != nil {
		t.Fatal(err)
	}
	if err := RecoverTransactions(root); err == nil {
		t.Fatal("stale operation took ownership of another commit")
	}
	body, err := os.ReadFile(s.Entry.Document)
	if err != nil || string(body) != s.Text+"\nnew record\n" {
		t.Fatalf("new data overwritten: %q %v", body, err)
	}
}

func TestReadAndUpdateRejectIncompleteLayout(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "incomplete-layout", false)
	if err := os.Remove(filepath.Join(root, "done")); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadSnapshot(root, s.Entry.TaskID); err == nil {
		t.Fatal("read accepted incomplete board")
	}
	if err := UpdateDocument(root, s.Entry.TaskID, UpdateOptions{Document: "spec.md", Text: s.Text, ExpectedRevision: s.Revision}); err == nil {
		t.Fatal("update accepted incomplete board")
	}
}
func TestTerminationHistoryRemainsOneProtectedSection(t *testing.T) {
	root := tempBoard(t)
	s := transactionCard(t, root, "termination-history", false)
	archived, err := MoveWithOptions(s.Entry, root, "archived", MoveOptions{Result: "cancelled", Reason: "取消", Decision: "取消决定"})
	if err != nil {
		t.Fatal(err)
	}
	trash, err := MoveWithOptions(archived, root, "trash", MoveOptions{Result: "trashed", Reason: "删除", Decision: "删除决定"})
	if err != nil {
		t.Fatal(err)
	}
	s = transactionSnapshot(t, root, trash.TaskID)
	if strings.Count(s.Text, "## LIFECYCLE_DECISION") != 1 || !strings.Contains(s.Text, "取消决定") || !strings.Contains(s.Text, "删除决定") {
		t.Fatal("termination history lost or ambiguous")
	}
	updateSnapshot(t, root, s, s.Text+"\n## NOTES\n\n后续说明\n")
}
