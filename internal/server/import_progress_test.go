package server

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

// editSpec rewrites one card document of a fixture source board directly.
func editSpec(t *testing.T, source, state, id string, edit func(string) string) {
	t.Helper()
	path := filepath.Join(source, state, id, "spec.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(edit(string(data))), 0o600); err != nil {
		t.Fatal(err)
	}
}

// stripSize removes the SIZE line of every card of a fixture and returns the
// card IDs in order.
func stripSize(t *testing.T, source string) []string {
	t.Helper()
	var ids []string
	specs, err := filepath.Glob(filepath.Join(source, "*", "*", "spec.md"))
	if err != nil || len(specs) == 0 {
		t.Fatalf("fixture cards: %v %v", specs, err)
	}
	for _, spec := range specs {
		data, err := os.ReadFile(spec)
		if err != nil {
			t.Fatal(err)
		}
		out := strings.Replace(string(data), "- SIZE: small\n", "", 1)
		if out == string(data) {
			t.Fatalf("%s has no small SIZE line", spec)
		}
		if err := os.WriteFile(spec, []byte(out), 0o600); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, filepath.Base(filepath.Dir(spec)))
	}
	slices.Sort(ids)
	return ids
}

// badPrerequisites makes a card fail the dependency check: a group member
// whose PREREQUISITES line is outside the opening code block.
func badPrerequisites(text string) string {
	text = strings.Replace(text, "- TASK_GROUP:\n", "- TASK_GROUP: 20261117-fixture-group\n", 1)
	return strings.Replace(text, "## DISCUSSION\n", "## DISCUSSION\n\nPREREQUISITES: N/A\n", 1)
}

// addClosedCard adds a card to the closed state of a fixture, with the
// dependency problem of badPrerequisites when bad is set.
func addClosedCard(t *testing.T, source, state, slug string, bad bool) string {
	t.Helper()
	target, err := board.NewTask(source, "feature", slug, "Closed "+slug, "en", false)
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(target)
	editSpec(t, source, "backlog", id, func(text string) string {
		text = strings.ReplaceAll(text, "<FILL_IN>", "filled")
		if bad {
			text = badPrerequisites(text)
		}
		return text
	})
	if err := os.Rename(filepath.Join(source, "backlog", id), filepath.Join(source, state, id)); err != nil {
		t.Fatal(err)
	}
	return id
}

func collectProgress() (func(api.ImportProgress), *[]api.ImportProgress) {
	var got []api.ImportProgress
	return func(p api.ImportProgress) { got = append(got, p) }, &got
}

func phasesOf(progress []api.ImportProgress) []string {
	var phases []string
	for _, p := range progress {
		if len(phases) == 0 || phases[len(phases)-1] != p.Phase {
			phases = append(phases, p.Phase)
		}
	}
	return phases
}

var allImportPhases = []string{api.ImportPhaseLock, api.ImportPhaseScan, api.ImportPhaseCopy, api.ImportPhaseVerify, api.ImportPhaseCheck, api.ImportPhasePublish}

func TestImportReportsEveryPhaseInOrder(t *testing.T) {
	source := v1Board(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	report, got := collectProgress()
	result, err := s.ImportWithProgress(t.Context(), "demo", "", source, report)
	if err != nil || result.Tasks != 2 {
		t.Fatalf("import: %+v %v", result, err)
	}
	if phases := phasesOf(*got); !slices.Equal(phases, allImportPhases) {
		t.Fatalf("phases %v, want %v", phases, allImportPhases)
	}
	var copying []api.ImportProgress
	for _, p := range *got {
		if p.Phase == api.ImportPhaseCopy {
			copying = append(copying, p)
		}
	}
	if len(copying) < 2 || copying[0].Done != 0 || copying[0].Total != result.Files || copying[0].TotalBytes == 0 {
		t.Fatalf("copy start: %+v", copying)
	}
	last := copying[len(copying)-1]
	if last.Done != last.Total || last.Bytes != last.TotalBytes {
		t.Fatalf("copy end: %+v", last)
	}
	for i := 1; i < len(copying); i++ {
		if copying[i].Done < copying[i-1].Done || copying[i].Bytes < copying[i-1].Bytes {
			t.Fatalf("copy counts went back: %+v then %+v", copying[i-1], copying[i])
		}
	}
	// A repeated import answers from the record after its scan.
	again, got2 := collectProgress()
	if r, err := s.ImportWithProgress(t.Context(), "demo", "", source, again); err != nil || !r.Existing {
		t.Fatalf("repeat: %+v %v", r, err)
	}
	if phases := phasesOf(*got2); phases[0] != api.ImportPhaseLock || slices.Contains(phases, api.ImportPhaseCopy) {
		t.Fatalf("a repeated import copied: %v", phases)
	}
}

func TestImportDefaultsAMissingSizeAndKeepsTheSource(t *testing.T) {
	source := v1Board(t)
	ids := stripSize(t, source)
	_, before, err := snapshotTree(source)
	if err != nil {
		t.Fatal(err)
	}
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	result, err := s.Import("demo", "", source)
	if err != nil || result.Tasks != 2 || result.Existing || len(result.Warnings) != 0 {
		t.Fatalf("import: %+v %v", result, err)
	}
	if !slices.Equal(result.DefaultedSize, ids) {
		t.Fatalf("defaulted %v, want %v", result.DefaultedSize, ids)
	}
	if _, after, _ := snapshotTree(source); after != before || result.Snapshot != before {
		t.Fatal("the source board must stay byte-identical and the snapshot is the source digest")
	}
	p, err := s.project(result.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		state := filepath.Base(filepath.Dir(filepath.Dir(mustFind(t, source, id))))
		original, err := os.ReadFile(filepath.Join(source, state, id, "spec.md"))
		if err != nil {
			t.Fatal(err)
		}
		published, err := os.ReadFile(filepath.Join(p.board, state, id, "spec.md"))
		if err != nil {
			t.Fatal(err)
		}
		if string(published) != board.DefaultedSizeText(string(original)) || strings.Count(string(published), "- SIZE: large\n") != 1 || len(published) != len(original)+len("- SIZE: large\n") {
			t.Fatalf("%s: published text differs from the source by more than one SIZE line", id)
		}
	}
	again, err := s.Import("demo", "", source)
	if err != nil || !again.Existing || !slices.Equal(again.DefaultedSize, ids) || again.Snapshot != result.Snapshot {
		t.Fatalf("the same snapshot answers with the same defaults: %+v %v", again, err)
	}
	// The record, not the published tree, answers from the precheck of a project with tasks.
	moved := filepath.Join(t.TempDir(), "kanban")
	if err := os.CopyFS(moved, os.DirFS(source)); err != nil {
		t.Fatal(err)
	}
	if elsewhere, err := s.Import("demo", "", moved); err != nil || !elsewhere.Existing || !slices.Equal(elsewhere.DefaultedSize, ids) {
		t.Fatalf("copy of the source: %+v %v", elsewhere, err)
	}
}

func mustFind(t *testing.T, source, id string) string {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(source, "*", id, "spec.md"))
	if len(matches) != 1 {
		t.Fatalf("card %s: %v", id, matches)
	}
	return matches[0]
}

func TestImportAcceptsProblemsOfClosedCardsAsWarnings(t *testing.T) {
	for _, state := range []string{"done", "archived", "trash"} {
		t.Run(state, func(t *testing.T) {
			source := v1Board(t)
			closed := addClosedCard(t, source, state, "history", true)
			s, h := openTest(t, t.TempDir(), nil)
			defer h.Close()
			defer s.Close()
			result, err := s.Import("demo", "", source)
			if err != nil || result.Tasks != 3 || len(result.Warnings) != 1 {
				t.Fatalf("import: %+v %v", result, err)
			}
			w := result.Warnings[0]
			if !slices.Equal(w.TaskIDs, []string{closed}) || !strings.Contains(w.Message, closed) {
				t.Fatalf("warning: %+v", w)
			}
			again, err := s.Import("demo", "", source)
			if err != nil || !again.Existing || len(again.Warnings) != 1 || again.Warnings[0].Message != w.Message {
				t.Fatalf("the same snapshot answers with the same warnings: %+v %v", again, err)
			}
			p, _ := s.project(result.Project.ID)
			published, err := os.ReadFile(filepath.Join(p.board, state, closed, "spec.md"))
			original, _ := os.ReadFile(filepath.Join(source, state, closed, "spec.md"))
			if err != nil || !bytes.Equal(published, original) {
				t.Fatalf("a warned card is imported as it is: %v", err)
			}
		})
	}
}

func TestImportRejectsTheSameProblemOnAnActiveCard(t *testing.T) {
	for _, state := range []string{"backlog", "working"} {
		t.Run(state, func(t *testing.T) {
			source := v1Board(t)
			active := addClosedCard(t, source, "done", "history", true)
			ids, _ := filepath.Glob(filepath.Join(source, state, "*"))
			editSpec(t, source, state, filepath.Base(ids[0]), badPrerequisites)
			s, h := openTest(t, t.TempDir(), nil)
			defer h.Close()
			defer s.Close()
			result, err := s.Import("demo", "", source)
			if err == nil || !strings.Contains(err.Error(), "staged board fails check; nothing was published") || !strings.Contains(err.Error(), filepath.Base(ids[0])) {
				t.Fatalf("an active card with the problem must be refused: %+v %v", result, err)
			}
			if strings.Contains(err.Error(), active) {
				t.Fatalf("the refusal lists only blocking problems: %v", err)
			}
			p, _ := s.project(mustProject(t, s, "demo"))
			view, err := board.LoadBoard(p.board)
			if err != nil || len(view.Entries) != 0 {
				t.Fatalf("nothing is published: %+v %v", view.Entries, err)
			}
			if _, err := os.Lstat(filepath.Join(p.root, "control", "import.json")); !os.IsNotExist(err) {
				t.Fatalf("no import record: %v", err)
			}
		})
	}
}

func mustProject(t *testing.T, s *Server, name string) string {
	t.Helper()
	info, err := s.projectByName(name)
	if err != nil {
		t.Fatal(err)
	}
	return info.ID
}

func TestImportOfDefaultedSizeRecoversAfterEveryBoundary(t *testing.T) {
	for _, step := range []string{"prepared", "backup-renamed", "published", "recorded"} {
		t.Run(step, func(t *testing.T) {
			source := v1Board(t)
			ids := stripSize(t, source)
			data := t.TempDir()
			srv, err := Open(Options{DataDir: data})
			if err != nil {
				t.Fatal(err)
			}
			interrupted := fmt.Errorf("interrupted at %s: %w", step, errImportCrash)
			srv.importFault = func(current string) error {
				if current == step {
					return interrupted
				}
				return nil
			}
			if _, err := srv.Import("recovered", "", source); !errors.Is(err, interrupted) {
				t.Fatalf("boundary %s: %v", step, err)
			}
			if err := srv.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(Options{DataDir: data})
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			again, err := reopened.Import("recovered", "", source)
			if err != nil || !again.Existing || !slices.Equal(again.DefaultedSize, ids) {
				t.Fatalf("recovered import: %+v %v", again, err)
			}
			p, err := reopened.project(again.Project.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, id := range ids {
				published, err := os.ReadFile(mustFind(t, p.board, id))
				if err != nil || strings.Count(string(published), "- SIZE: large\n") != 1 {
					t.Fatalf("%s lost its default SIZE: %v", id, err)
				}
			}
			for _, name := range []string{"board.import", "board.before-import", filepath.Join("control", "import-pending.json")} {
				if _, err := os.Lstat(filepath.Join(p.root, name)); !os.IsNotExist(err) {
					t.Fatalf("recovery artifact %s: %v", name, err)
				}
			}
		})
	}
}

func TestImportRecoversAnIntentWithoutAStagedDigest(t *testing.T) {
	source := v1Board(t)
	data := t.TempDir()
	srv, err := Open(Options{DataDir: data})
	if err != nil {
		t.Fatal(err)
	}
	interrupted := fmt.Errorf("interrupted: %w", errImportCrash)
	srv.importFault = func(current string) error {
		if current == "prepared" {
			return interrupted
		}
		return nil
	}
	if _, err := srv.Import("legacy-intent", "", source); !errors.Is(err, interrupted) {
		t.Fatal(err)
	}
	p, err := srv.project(mustProject(t, srv, "legacy-intent"))
	if err != nil {
		t.Fatal(err)
	}
	var pending map[string]any
	if found, err := readJSON(p.root, filepath.Join("control", "import-pending.json"), &pending); err != nil || !found {
		t.Fatalf("pending intent: %v %v", found, err)
	}
	if pending["staged_digest"] != pending["record"].(map[string]any)["snapshot"] {
		t.Fatal("without defaults the staged board equals the source snapshot")
	}
	// An intent written before the field existed carries the digest nowhere.
	delete(pending, "staged_digest")
	if err := writeJSON(p.root, filepath.Join("control", "import-pending.json"), pending); err != nil {
		t.Fatal(err)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(Options{DataDir: data})
	if err != nil {
		t.Fatalf("an old intent is recovered at startup: %v", err)
	}
	defer reopened.Close()
	if again, err := reopened.Import("legacy-intent", "", source); err != nil || !again.Existing || again.Tasks != 2 {
		t.Fatalf("recovered: %+v %v", again, err)
	}
}

// streamImport posts an import with Accept set to the stream type and returns
// the status, content type, and decoded lines.
func streamImport(t *testing.T, url, token, body string) (int, string, []api.ImportStreamLine, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/api/v1/server/import", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(api.HeaderProtocol, strconv.Itoa(api.ProtocolVersion))
	req.Header.Set(api.HeaderAdminToken, token)
	req.Header.Set("Accept", api.ImportStreamType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw bytes.Buffer
	var lines []api.ImportStreamLine
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		raw.Write(scanner.Bytes())
		raw.WriteByte('\n')
		var line api.ImportStreamLine
		if err := json.Unmarshal(scanner.Bytes(), &line); err != nil {
			t.Fatalf("stream line %q: %v", scanner.Text(), err)
		}
		lines = append(lines, line)
	}
	return resp.StatusCode, resp.Header.Get("Content-Type"), lines, raw.String()
}

func TestImportRouteStreamsProgressThenTheResult(t *testing.T) {
	source := v1Board(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	body := fmt.Sprintf(`{"project":"demo","from":%q}`, source)
	status, contentType, lines, raw := streamImport(t, h.URL, s.adminToken, body)
	if status != http.StatusOK || contentType != api.ImportStreamType {
		t.Fatalf("status=%d type=%q", status, contentType)
	}
	if strings.Contains(raw, s.adminToken) {
		t.Fatal("the stream carries the administration token")
	}
	var progress []api.ImportProgress
	for i, line := range lines[:len(lines)-1] {
		if line.Progress == nil || line.Result != nil || line.Error != nil {
			t.Fatalf("line %d is not a progress line: %+v", i, line)
		}
		progress = append(progress, *line.Progress)
	}
	if phases := phasesOf(progress); !slices.Equal(phases, allImportPhases) {
		t.Fatalf("streamed phases %v, want %v", phases, allImportPhases)
	}
	last := lines[len(lines)-1]
	if last.Result == nil || last.Result.Tasks != 2 || last.Error != nil {
		t.Fatalf("last line: %+v", last)
	}
	if _, err := s.projectByName("demo"); err != nil {
		t.Fatal(err)
	}
}

func TestImportRouteStreamsAnErrorWithItsStatus(t *testing.T) {
	source := v1Board(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	if _, err := s.Import("demo", "", source); err != nil {
		t.Fatal(err)
	}
	if _, err := board.NewTask(source, "chore", "later", "Later", "en", false); err != nil {
		t.Fatal(err)
	}
	status, _, lines, _ := streamImport(t, h.URL, s.adminToken, fmt.Sprintf(`{"project":"demo","from":%q}`, source))
	last := lines[len(lines)-1]
	if status != http.StatusOK || last.Error == nil || last.Error.Code != api.CodeProjectNotEmpty || last.Status != http.StatusConflict {
		t.Fatalf("status=%d last=%+v", status, last)
	}
	c, _ := client.New(h.URL, "host-b")
	_, err := c.ImportWithProgress(t.Context(), s.adminToken, api.ImportRequest{Project: "demo", From: source}, func(api.ImportProgress) {})
	var apiErr *api.Error
	if !errors.As(err, &apiErr) || apiErr.Code != api.CodeProjectNotEmpty || apiErr.Status != http.StatusConflict {
		t.Fatalf("the client restores the status of a streamed error: %v", err)
	}
}

func TestImportRouteRefusesBeforeItStreams(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	for name, tc := range map[string]struct {
		token, body string
		status      int
	}{
		"token":    {strings.Repeat("a", 64), `{"project":"demo","from":"/x"}`, http.StatusForbidden},
		"relative": {s.adminToken, `{"project":"demo","from":"x"}`, http.StatusBadRequest},
		"body":     {s.adminToken, `{`, http.StatusBadRequest},
	} {
		t.Run(name, func(t *testing.T) {
			status, contentType, lines, raw := streamImport(t, h.URL, tc.token, tc.body)
			if status != tc.status || !strings.HasPrefix(contentType, "application/json") || len(lines) != 1 || lines[0].Progress != nil {
				t.Fatalf("status=%d type=%q body=%s", status, contentType, raw)
			}
		})
	}
}

func TestImportRouteWithoutAcceptAnswersWithOneJSON(t *testing.T) {
	source := v1Board(t)
	stripSize(t, source)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	c, _ := client.New(h.URL, "host-b")
	result, err := c.Import(t.Context(), s.adminToken, api.ImportRequest{Project: "demo", From: source})
	if err != nil || result.Tasks != 2 || len(result.DefaultedSize) != 2 {
		t.Fatalf("single response: %+v %v", result, err)
	}
}

func TestVerifyDefaultedTreeAcceptsOnlyTheInsertedLine(t *testing.T) {
	source := t.TempDir()
	card := "backlog/a-task/spec.md"
	other := "backlog/a-task/notes.md"
	for rel, text := range map[string]string{card: "# A\n\n- TYPE: Feature\n", other: "notes\n"} {
		path := filepath.Join(source, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	src, err := scanTree(source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defaults := []board.SizeDefault{{TaskID: "a-task", Document: card}}
	stage := func(edit func(root string)) treeScan {
		root := t.TempDir()
		if err := os.CopyFS(root, os.DirFS(source)); err != nil {
			t.Fatal(err)
		}
		edit(root)
		scan, err := scanTree(root, nil)
		if err != nil {
			t.Fatal(err)
		}
		return scan
	}
	write := func(root, rel, text string) {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(rel)), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	good := stage(func(root string) { write(root, card, "# A\n\n- TYPE: Feature\n- SIZE: large\n") })
	if err := verifyDefaultedTree(source, src, good, defaults); err != nil {
		t.Fatalf("the expected difference: %v", err)
	}
	for name, edit := range map[string]func(string){
		"another size":    func(root string) { write(root, card, "# A\n\n- TYPE: Feature\n- SIZE: small\n") },
		"a second change": func(root string) { write(root, card, "# A\n\n- TYPE: Feature\n- SIZE: large\n\nmore\n") },
		"another file": func(root string) {
			write(root, card, "# A\n\n- TYPE: Feature\n- SIZE: large\n")
			write(root, other, "changed\n")
		},
		"no change": func(root string) {},
		"an extra file": func(root string) {
			write(root, card, "# A\n\n- TYPE: Feature\n- SIZE: large\n")
			write(root, "backlog/extra.md", "x")
		},
		"a removed record": func(root string) {
			write(root, card, "# A\n\n- TYPE: Feature\n- SIZE: large\n")
			_ = os.Remove(filepath.Join(root, other))
		},
	} {
		if err := verifyDefaultedTree(source, src, stage(edit), defaults); !api.IsCode(err, api.CodeBoardRejected) {
			t.Fatalf("%s: a staged tree that differs beyond the SIZE line must be a board rejection: %v", name, err)
		}
	}
}

func TestImportRepeatsTheLatestReportWhileOneStepRunsLong(t *testing.T) {
	previous := importProgressInterval
	importProgressInterval = 20 * time.Millisecond
	t.Cleanup(func() { importProgressInterval = previous })
	source := v1Board(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	var mu sync.Mutex
	var copies []api.ImportProgress
	report := func(p api.ImportProgress) {
		mu.Lock()
		defer mu.Unlock()
		if p.Phase == api.ImportPhaseCopy {
			copies = append(copies, p)
		}
	}
	copied := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(copies)
	}
	fired := false
	s.importFault = func(step string) error {
		if step != "copying" || fired {
			return nil
		}
		fired = true
		// The first file is held: only the timer can report now.
		before := copied()
		deadline := time.Now().Add(10 * time.Second)
		for copied() < before+3 {
			if time.Now().After(deadline) {
				return errors.New("no report while the copy was held")
			}
			time.Sleep(5 * time.Millisecond)
		}
		return nil
	}
	if result, err := s.ImportWithProgress(t.Context(), "demo", "", source, report); err != nil || result.Tasks != 2 {
		t.Fatalf("import: %+v %v", result, err)
	}
	mu.Lock()
	defer mu.Unlock()
	held := 0
	for _, p := range copies {
		if p.Done == 0 && p.Total > 0 {
			held++
		}
	}
	if held < 3 {
		t.Fatalf("the unchanged count was repeated %d times: %+v", held, copies)
	}
}

func TestImportRepeatsTheNewestCountsWhileTheNextFileRunsLong(t *testing.T) {
	previous := importProgressInterval
	importProgressInterval = 50 * time.Millisecond
	t.Cleanup(func() { importProgressInterval = previous })
	source := v1Board(t)
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	var mu sync.Mutex
	var copies []api.ImportProgress
	report := func(p api.ImportProgress) {
		mu.Lock()
		defer mu.Unlock()
		if p.Phase == api.ImportPhaseCopy {
			copies = append(copies, p)
		}
	}
	repeats := func(done int) (n int) {
		mu.Lock()
		defer mu.Unlock()
		for _, p := range copies {
			if p.Done == done {
				n++
			}
		}
		return n
	}
	files := 0
	s.importFault = func(step string) error {
		if step != "copying" {
			return nil
		}
		files++
		if files != 2 {
			return nil
		}
		// One file is done, which the throttle held back from the report, and the next one is held:
		// the timer must repeat the count of one file.
		deadline := time.Now().Add(10 * time.Second)
		for repeats(1) < 3 {
			if time.Now().After(deadline) {
				return errors.New("the timer never repeated the newest count")
			}
			time.Sleep(5 * time.Millisecond)
		}
		return nil
	}
	if result, err := s.ImportWithProgress(t.Context(), "demo", "", source, report); err != nil || result.Tasks != 2 {
		t.Fatalf("import: %+v %v", result, err)
	}
}
