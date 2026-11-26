package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
	"github.com/dualface/kander/v2/internal/command/commandtest"
)

func newImportFixture(t *testing.T, slug string) (root, id string) {
	t.Helper()
	root = filepath.Join(t.TempDir(), "kanban")
	if err := board.InitStorage(root); err != nil {
		t.Fatal(err)
	}
	return root, addImportTask(t, root, slug)
}

func addImportTask(t *testing.T, root, slug string) string {
	t.Helper()
	path, err := board.NewTask(root, "feature", slug, slug, "en", false)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Base(path)
}

func importProject(t *testing.T, s *Server, source string) (*project, api.ImportResult) {
	t.Helper()
	result, err := s.Import("demo", "", source)
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.project(result.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	return p, result
}

func scanImportTestTree(t *testing.T, path string) treeScan {
	t.Helper()
	tree, err := scanTree(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	return tree
}

func assertOriginalImportFiles(t *testing.T, before, after treeScan) {
	t.Helper()
	for path, want := range before.sums {
		if after.sums[path] != want {
			t.Errorf("original file changed: %s", path)
		}
	}
}

func writeImportFixture(t *testing.T, root, rel, text string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestImportMergeOfflineMatchesOnlineAndPreservesTheTarget(t *testing.T) {
	initial, first := newImportFixture(t, "target")
	source := v1Board(t)
	const lock = ".kander/locks/source-only.lock"
	const cache = ".kander/caches/test/source-only.json"
	writeImportFixture(t, source, lock, "runtime lock")
	writeImportFixture(t, source, cache, "runtime cache")
	writeImportFixture(t, initial, ".kander/caches/test/target.json", "existing cache")
	writeImportFixture(t, initial, "backlog/"+first+"/attachments/original.md", "preserved attachment")
	var want api.ImportResult
	var wantTree treeScan
	for _, online := range []bool{false, true} {
		dir := t.TempDir()
		s, h := openTest(t, dir, nil)
		defer h.Close()
		defer s.Close()
		p, _ := importProject(t, s, initial)
		before := scanImportTestTree(t, p.board)
		if online {
			useImportTarget(t, h.URL)
		} else if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		code, stdout, stderr := runImportCommand(t, dir, source, make(chan os.Signal, 1))
		if code != 0 {
			t.Fatalf("online=%v exit=%d: %s", online, code, stderr.String())
		}
		var got api.ImportResult
		if err := json.Unmarshal([]byte(stdout.String()), &got); err != nil {
			t.Fatal(err)
		}
		got.Project = api.Project{}
		after := scanImportTestTree(t, p.board)
		if got.AddedTasks != 2 || got.Tasks != 2 || got.Existing || got.Files != len(scanImportTestTree(t, source).files) {
			t.Fatalf("merge: %+v", got)
		}
		assertOriginalImportFiles(t, before, after)
		for _, rel := range []string{lock, cache} {
			if _, exists := after.sums[rel]; exists {
				t.Errorf("copied a source runtime file: %s", rel)
			}
		}
		if online {
			if !reflect.DeepEqual(want, got) || wantTree.digest != after.digest {
				t.Fatalf("online differs from offline: %+v vs %+v, %s vs %s", want, got, wantTree.digest, after.digest)
			}
		} else {
			want, wantTree = got, after
		}
	}
}

func TestImportMergeMatchesCardsAfterDefaultingSize(t *testing.T) {
	for _, missingSize := range []bool{false, true} {
		name := "same bytes"
		if missingSize {
			name = "source without SIZE"
		}
		t.Run(name, func(t *testing.T) {
			source, id := newImportFixture(t, "original")
			if missingSize {
				stripSize(t, source)
			}
			s, h := openTest(t, t.TempDir(), nil)
			defer h.Close()
			defer s.Close()
			p, first := importProject(t, s, source)
			before := scanImportTestTree(t, p.board)
			addImportTask(t, source, "added")
			result, err := s.Import("demo", "", source)
			if err != nil || result.Snapshot == first.Snapshot || !slices.Equal(result.ExistingTasks, []string{id}) ||
				result.AddedTasks != 1 || len(result.DefaultedSize) != 0 {
				t.Fatalf("same card was not skipped: %+v %v", result, err)
			}
			after := scanImportTestTree(t, p.board)
			assertOriginalImportFiles(t, before, after)
			again, err := s.Import("demo", "", source)
			result.Existing = true
			if err != nil || !reflect.DeepEqual(result, again) {
				t.Fatalf("receipt changed: got %+v want %+v: %v", again, result, err)
			}
		})
	}
}

func TestImportMergeRejectsOrSkipsWholeConflictingCards(t *testing.T) {
	target, old := newImportFixture(t, "in-both")
	old2 := addImportTask(t, target, "also-in-both")
	source := filepath.Join(t.TempDir(), "kanban")
	if err := os.CopyFS(source, os.DirFS(target)); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{old, old2} {
		editSpec(t, source, "backlog", id, func(text string) string { return text + "\nsource-only edit\n" })
	}
	sharedOwner := addImportTask(t, source, "owns-different-dispatch")
	added := addImportTask(t, source, "safe-to-add")
	otherOld := addImportTask(t, target, "dispatch-already-owned")
	const registry = ".kander/groups/00000000-dispatch-group/"
	const competing = registry + "shared-dispatch.json"
	const skippedOnly = registry + "skipped-only.json"
	const incomingOnly = registry + "incoming-only.json"
	const group = "20261126-shared-record-group"
	const groupPath = ".kander/groups/" + group + "/checkpoint.json"
	writeImportFixture(t, target, competing, `{"task_id":"`+otherOld+`"}`)
	writeImportFixture(t, source, competing, `{"task_id":"`+sharedOwner+`"}`)
	writeImportFixture(t, source, skippedOnly, `{"task_id":"`+old+`"}`)
	writeImportFixture(t, source, incomingOnly, `{"task_id":"`+added+`"}`)
	writeImportFixture(t, source, groupPath, `{"members":{"`+old2+`":{}}}`)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	p, _ := importProject(t, s, target)
	before := scanImportTestTree(t, p.board)
	req := api.ImportRequest{Project: "demo", From: source}
	_, err := onlineImport(t, s, h.URL, req)
	var failure *api.Error
	if !errors.As(err, &failure) || failure.Code != api.CodeBoardRejected || failure.Status != http.StatusUnprocessableEntity {
		t.Fatalf("expected board rejection, got %v", err)
	}
	for _, id := range []string{old, old2, sharedOwner, "shared-dispatch"} {
		if !strings.Contains(failure.Message, id) {
			t.Errorf("rejection omits %s: %s", id, failure.Message)
		}
	}
	if after := scanImportTestTree(t, p.board); after.digest != before.digest {
		t.Fatal("rejected merge changed destination")
	}
	req.SkipConflicts = true
	result, err := onlineImport(t, s, h.URL, req)
	want := []api.ImportConflict{{Kind: "dispatch", ID: "shared-dispatch"}, {Kind: "task", ID: old2}, {Kind: "task", ID: old}, {Kind: "task", ID: sharedOwner}}
	if err != nil || result.AddedTasks != 1 || !slices.Equal(want, result.SkippedConflicts) {
		t.Fatalf("skip conflicts: %+v %v", result, err)
	}
	after := scanImportTestTree(t, p.board)
	assertOriginalImportFiles(t, before, after)
	for _, rel := range []string{skippedOnly, groupPath, "backlog/" + sharedOwner + "/spec.md", ".kander/versions/" + sharedOwner + ".json"} {
		if _, exists := after.sums[rel]; exists {
			t.Errorf("copied rejected card data: %s", rel)
		}
	}
	for _, rel := range []string{incomingOnly, "backlog/" + added + "/spec.md"} {
		if _, exists := after.sums[rel]; !exists {
			t.Errorf("missing accepted card data: %s", rel)
		}
	}
}

func TestImportCommandPassesSkipConflictsToTheServer(t *testing.T) {
	target, id := newImportFixture(t, "target")
	source := filepath.Join(t.TempDir(), "kanban")
	if err := os.CopyFS(source, os.DirFS(target)); err != nil {
		t.Fatal(err)
	}
	editSpec(t, source, "backlog", id, func(text string) string { return text + "\nchanged\n" })
	addImportTask(t, source, "new")
	dir := t.TempDir()
	s, h := openTest(t, dir, nil)
	defer h.Close()
	defer s.Close()
	importProject(t, s, target)
	useImportTarget(t, h.URL)
	code, stdout, stderr := commandtest.Capture(func() int {
		return commandtest.Run(NewImportCommand(), "--data", dir, "--project", "demo", "--from", source, "--skip-conflicts")
	})
	var result api.ImportResult
	if err := json.Unmarshal([]byte(stdout), &result); code != 0 || err != nil || result.AddedTasks != 1 ||
		!slices.Equal(result.SkippedConflicts, []api.ImportConflict{{Kind: "task", ID: id}}) {
		t.Fatalf("CLI merge returned %d: %+v %v\n%s", code, result, err, stderr)
	}
}

func TestImportMergeRefusesUnknownPrivateFiles(t *testing.T) {
	target, _ := newImportFixture(t, "target")
	source, _ := newImportFixture(t, "incoming")
	const unknown = ".kander/groups/00000000-unknown-group/payload.json"
	writeImportFixture(t, source, unknown, "unclassified file")
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	p, _ := importProject(t, s, target)
	before := scanImportTestTree(t, p.board)
	_, err := s.importV1Board(t.Context(), api.ImportRequest{Project: "demo", From: source, SkipConflicts: true}, nil)
	if !api.IsCode(err, api.CodeBoardRejected) || !strings.Contains(err.Error(), unknown) {
		t.Fatalf("unknown file: %v", err)
	}
	if after := scanImportTestTree(t, p.board); after.digest != before.digest {
		t.Fatal("unrecognized import changed destination")
	}
}

func TestImportMergeChecksTheWholeBoard(t *testing.T) {
	target, old := newImportFixture(t, "target")
	source, incoming := newImportFixture(t, "incoming")
	closed := addClosedCard(t, source, "archived", "closed", true)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	p, first := importProject(t, s, target)
	c, _ := client.New(h.URL, "checker")
	remote := c.Project(first.Project.ID)
	baseline, err := remote.Check(t.Context(), api.CheckQuery{})
	if err != nil || baseline.Code != 0 {
		t.Fatalf("check before: %+v %v", baseline, err)
	}
	restoreOld, err := readImportFile(p.board, "backlog/"+old+"/spec.md")
	if err != nil {
		t.Fatal(err)
	}
	editSpec(t, p.board, "backlog", old, badPrerequisites)
	before := scanImportTestTree(t, p.board)
	_, err = s.Import("demo", "", source)
	wantRejection(t, err, old)
	if after := scanImportTestTree(t, p.board); after.digest != before.digest {
		t.Fatal("blocked target was changed")
	}
	writeImportFixture(t, p.board, "backlog/"+old+"/spec.md", string(restoreOld))
	restoreIncoming, err := readImportFile(source, "backlog/"+incoming+"/spec.md")
	if err != nil {
		t.Fatal(err)
	}
	editSpec(t, source, "backlog", incoming, badPrerequisites)
	_, err = s.Import("demo", "", source)
	wantRejection(t, err, incoming)
	writeImportFixture(t, source, "backlog/"+incoming+"/spec.md", string(restoreIncoming))
	result, err := s.Import("demo", "", source)
	if err != nil || len(result.Warnings) != 1 || !slices.Equal(result.Warnings[0].TaskIDs, []string{closed}) {
		t.Fatalf("closed-card warning: %+v %v", result, err)
	}
	after, err := remote.Check(t.Context(), api.CheckQuery{})
	if err != nil || after.Code != baseline.Code || !slices.Equal(after.Diagnostics, baseline.Diagnostics) {
		t.Fatalf("new check failures: before %+v after %+v: %v", baseline, after, err)
	}
}
