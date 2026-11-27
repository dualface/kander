package server

import (
	"bytes"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/client"
)

// receivedTree records, at the "received" checkpoint of an upload import,
// the file information of every file the server received for project. It
// keeps a hard link to each file in hold, so a removed file's inode is never
// reused for a new file, which os.SameFile could not tell apart.
type receivedTree struct {
	hold  string
	mu    sync.Mutex
	files map[string]os.FileInfo
}

func (r *receivedTree) record(t *testing.T, data, project string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(projectRoot(data, project), uploadPrefix+"*", uploadBoardDir))
	if err != nil || len(matches) != 1 {
		t.Errorf("received tree: %v %v", matches, err)
		return
	}
	files := map[string]os.FileInfo{}
	err = filepath.WalkDir(matches[0], func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(matches[0], path)
		files[filepath.ToSlash(rel)] = info
		return os.Link(path, filepath.Join(r.hold, strconv.Itoa(len(files))))
	})
	if err != nil {
		t.Error(err)
	}
	r.mu.Lock()
	r.files = files
	r.mu.Unlock()
}

func (r *receivedTree) get() map[string]os.FileInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.files
}

// recordReceived records the received tree when the import reaches it and
// keeps the order of the import checkpoints.
func recordReceived(t *testing.T, s *Server, project string) (*receivedTree, func() []string) {
	t.Helper()
	tree := &receivedTree{hold: t.TempDir()}
	var mu sync.Mutex
	var steps []string
	s.importFault = func(step string) error {
		mu.Lock()
		steps = append(steps, step)
		mu.Unlock()
		if step == "received" {
			tree.record(t, s.data, project)
		}
		return nil
	}
	return tree, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(steps)
	}
}

// recordScanRoots records the root of every tree scan until the test ends.
func recordScanRoots(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var roots []string
	observe := func(root string) {
		mu.Lock()
		roots = append(roots, root)
		mu.Unlock()
	}
	importScanObserver.Store(&observe)
	t.Cleanup(func() { importScanObserver.Store(nil) })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(roots)
	}
}

// uploadWithProgress sends the archive of req to url and collects the
// progress the server streams.
func uploadWithProgress(t *testing.T, url string, req api.ImportRequest) (api.ImportResult, []api.ImportProgress, error) {
	t.Helper()
	var archive bytes.Buffer
	if err := writeBoardUpload(t.Context(), &archive, req, nil, nil); err != nil {
		t.Fatal(err)
	}
	peer, err := client.New(url, "uploader")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var progress []api.ImportProgress
	result, err := peer.UploadImport(t.Context(), req.Project, io.NopCloser(&archive), nil, func(p api.ImportProgress) {
		mu.Lock()
		progress = append(progress, p)
		mu.Unlock()
	})
	mu.Lock()
	defer mu.Unlock()
	return result, slices.Clone(progress), err
}

// assertSameImportedTree compares the files, their digests, and the
// directories of two published boards.
func assertSameImportedTree(t *testing.T, want, got string) {
	t.Helper()
	a, b := scanImportTestTree(t, want), scanImportTestTree(t, got)
	if !slices.Equal(a.files, b.files) || !slices.Equal(a.dirs, b.dirs) {
		t.Fatalf("published trees differ:\nfiles %v\n   vs %v\ndirs %v\n  vs %v", a.files, b.files, a.dirs, b.dirs)
	}
	for _, rel := range a.files {
		if a.sums[rel] != b.sums[rel] {
			t.Fatalf("published file %s differs", rel)
		}
	}
}

// assertSameModes compares the permission bits of the files of two boards.
func assertSameModes(t *testing.T, want, got string, files []string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	for _, rel := range files {
		a, errA := os.Lstat(filepath.Join(want, filepath.FromSlash(rel)))
		b, errB := os.Lstat(filepath.Join(got, filepath.FromSlash(rel)))
		if errA != nil || errB != nil || a.Mode().Perm() != b.Mode().Perm() {
			t.Fatalf("mode of %s: local %v, upload %v (%v %v)", rel, a, b, errA, errB)
		}
	}
}

func progressTotals(progress []api.ImportProgress, phase string) []int {
	var totals []int
	for _, p := range progress {
		if p.Phase == phase {
			totals = append(totals, p.Total)
		}
	}
	return totals
}

// moveFixture is a v1 board with cards that lack SIZE, a directory without
// files, atomic write remnants and dot files.
func moveFixture(t *testing.T) (source string, defaulted []string, remnants map[string]string) {
	t.Helper()
	source, remnants, _ = remnantBoard(t)
	defaulted = stripSize(t, source)
	if err := os.MkdirAll(filepath.Join(source, filepath.FromSlash(idleCardDir(t, source)), "empty", "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	writeImportFixture(t, source, ".gitignore", "/local-cache/\n")
	writeImportFixture(t, source, "done/.DS_Store", "historical Finder metadata")
	return source, defaulted, remnants
}

func defaultedSpec(rel string, ids []string) bool {
	for _, id := range ids {
		if strings.HasSuffix(rel, "/"+id+"/spec.md") {
			return true
		}
	}
	return false
}

func TestUploadMovesTheReceivedFilesIntoAnEmptyProject(t *testing.T) {
	old := setImportUmask(0o022)
	defer setImportUmask(old)
	source, defaulted, remnants := moveFixture(t)
	local, localHTTP := openTest(t, t.TempDir(), nil)
	defer local.Close()
	defer localHTTP.Close()
	want, err := local.Import("demo", "", source)
	if err != nil {
		t.Fatal(err)
	}
	wantProject, err := local.project(want.Project.ID)
	if err != nil {
		t.Fatal(err)
	}

	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	received, steps := recordReceived(t, s, "demo")
	roots := recordScanRoots(t)
	got, progress, err := uploadWithProgress(t, h.URL, api.ImportRequest{Project: "demo", From: source})
	if err != nil {
		t.Fatal(err)
	}
	sameImportOutcome(t, want, got)
	if len(got.Warnings) != len(remnants) || !slices.Equal(got.DefaultedSize, defaulted) {
		t.Fatalf("upload result: %+v", got)
	}
	p, err := s.project(got.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertSameImportedTree(t, wantProject.board, p.board)
	published := scanImportTestTree(t, p.board)
	assertSameModes(t, wantProject.board, p.board, published.files)

	// Every published file but the rewritten specs is the received file.
	files := received.get()
	for _, rel := range published.files {
		info, ok := files[rel]
		if !ok {
			t.Fatalf("published file %s was not received", rel)
		}
		now, err := os.Lstat(filepath.Join(p.board, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if same := os.SameFile(info, now); same == defaultedSpec(rel, defaulted) {
			t.Fatalf("%s: same file %v, defaulted %v", rel, same, defaultedSpec(rel, defaulted))
		}
	}
	for rel := range remnants {
		if _, ok := files[rel]; !ok || slices.Contains(published.files, rel) {
			t.Fatalf("remnant %s: received %v, published %v", rel, ok, slices.Contains(published.files, rel))
		}
	}

	if !slices.Contains(steps(), "moved") {
		t.Fatalf("the upload was not moved: %v", steps())
	}
	project := projectRoot(s.data, "demo")
	for _, root := range roots() {
		if strings.HasPrefix(root, filepath.Join(project, uploadPrefix)) {
			t.Fatalf("scanned the upload staging %s", root)
		}
	}
	if phases := phasesOf(progress); !slices.Equal(phases, allImportPhases) {
		t.Fatalf("phases: %v", phases)
	}
	if totals := progressTotals(progress, api.ImportPhaseScan); !slices.Contains(totals, len(files)) {
		t.Fatalf("scan totals %v, want %d", totals, len(files))
	}
	if totals := progressTotals(progress, api.ImportPhaseCopy); !slices.Contains(totals, len(files)-len(remnants)) {
		t.Fatalf("copy totals %v, want %d", totals, len(files)-len(remnants))
	}
	assertUploadClean(t, s.data, "demo")
	assertNoStaging(t, s.data, "demo")
}

// mergeMoveFixture is a target board and a source that adds a card, keeps an
// identical one and changes a conflicting one; every source card holds a
// directory without files, and the skipped cards hold files the target lacks.
func mergeMoveFixture(t *testing.T) (target, source string, ids map[string]string) {
	t.Helper()
	target, conflict := newImportFixture(t, "conflict")
	same := addImportTask(t, target, "same")
	source = filepath.Join(t.TempDir(), "kanban")
	if err := os.CopyFS(source, os.DirFS(target)); err != nil {
		t.Fatal(err)
	}
	editSpec(t, source, "backlog", conflict, func(text string) string { return text + "\nchanged\n" })
	added := addImportTask(t, source, "new")
	editSpec(t, source, "backlog", added, func(text string) string { return strings.Replace(text, "- SIZE: small\n", "", 1) })
	writeImportFixture(t, source, "backlog/"+added+"/attachments/new.txt", "new attachment")
	writeImportFixture(t, source, "backlog/"+conflict+"/attachments/only-source.txt", "skipped attachment")
	for _, id := range []string{added, conflict, same} {
		if err := os.MkdirAll(filepath.Join(source, "backlog", id, "empty"), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return target, source, map[string]string{"added": added, "conflict": conflict, "same": same}
}

func TestUploadMergeMovesTheSelectedSourceFiles(t *testing.T) {
	old := setImportUmask(0o022)
	defer setImportUmask(old)
	initial, source, ids := mergeMoveFixture(t)
	req := api.ImportRequest{Project: "demo", From: source, SkipConflicts: true}
	local, localHTTP := openTest(t, t.TempDir(), nil)
	defer local.Close()
	defer localHTTP.Close()
	wantProject, _ := importProject(t, local, initial)
	want, err := local.importV1Board(t.Context(), req, nil)
	if err != nil {
		t.Fatal(err)
	}

	s, h := openTest(t, t.TempDir(), nil)
	defer s.Close()
	defer h.Close()
	p, _ := importProject(t, s, initial)
	before := scanImportTestTree(t, p.board)
	received, steps := recordReceived(t, s, "demo")
	got, progress, err := uploadWithProgress(t, h.URL, req)
	if err != nil {
		t.Fatal(err)
	}
	sameImportOutcome(t, want, got)
	if got.AddedTasks != 1 || !slices.Equal(got.ExistingTasks, []string{ids["same"]}) ||
		!slices.Equal(got.SkippedConflicts, []api.ImportConflict{{Kind: "task", ID: ids["conflict"]}}) ||
		!slices.Equal(got.DefaultedSize, []string{ids["added"]}) || !slices.Contains(steps(), "moved") {
		t.Fatalf("merge upload: %+v, steps %v", got, steps())
	}
	assertSameImportedTree(t, wantProject.board, p.board)
	published := scanImportTestTree(t, p.board)
	assertSameModes(t, wantProject.board, p.board, published.files)

	files := received.get()
	planned := 0 // The files of the copy plan: the target's and the selected source files.
	for _, rel := range published.files {
		now, err := os.Lstat(filepath.Join(p.board, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		info, fromSource := files[rel]
		if sum, fromTarget := before.sums[rel]; fromTarget {
			// The target keeps its files, also where a skipped or existing
			// source card has the same path.
			if published.sums[rel] != sum || fromSource && os.SameFile(info, now) {
				t.Fatalf("target file %s: same digest %v, received file %v", rel, published.sums[rel] == sum, fromSource)
			}
			planned++
			continue
		}
		if board.ImportRecord(rel).Kind == "runtime" {
			continue // A merge never takes runtime files from the source.
		}
		defaulted := rel == "backlog/"+ids["added"]+"/spec.md"
		if !fromSource || os.SameFile(info, now) == defaulted {
			t.Fatalf("selected source file %s: received %v, defaulted %v", rel, fromSource, defaulted)
		}
		planned++
	}
	for _, rel := range []string{"backlog/" + ids["conflict"] + "/attachments/only-source.txt", "backlog/" + ids["added"] + "/attachments/new.txt"} {
		if _, ok := files[rel]; !ok {
			t.Fatalf("fixture file %s was not received", rel)
		}
	}
	if slices.Contains(published.files, "backlog/"+ids["conflict"]+"/attachments/only-source.txt") {
		t.Fatal("a file of the skipped card was published")
	}
	for _, id := range []string{ids["conflict"], ids["same"]} {
		for _, rel := range []string{"backlog/" + id + "/empty", "backlog/" + id + "/attachments"} {
			if slices.Contains(published.dirs, rel) && !slices.Contains(before.dirs, rel) {
				t.Fatalf("directory %s of a skipped or existing card was published", rel)
			}
		}
	}
	if !slices.Contains(published.dirs, "backlog/"+ids["added"]+"/empty") {
		t.Fatal("the empty directory of the added card was not published")
	}
	if totals := progressTotals(progress, api.ImportPhaseCopy); !slices.Contains(totals, planned) {
		t.Fatalf("copy totals %v, want %d", totals, planned)
	}
	assertUploadClean(t, s.data, "demo")
	assertNoStaging(t, s.data, "demo")
}
