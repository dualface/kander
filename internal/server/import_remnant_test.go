package server

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/config"
)

// remnantBoard is v1Board with three leftovers of interrupted atomic
// writes: one equal to its target, one that differs, and one without a
// target. It returns the leftovers and the warnings an import must report.
func remnantBoard(t *testing.T) (source string, remnants map[string]string, warnings []api.ImportWarning) {
	t.Helper()
	source = v1Board(t)
	card := idleCardDir(t, source)
	id := path.Base(card)
	versions, err := filepath.Glob(filepath.Join(source, ".kander", "versions", "*.json"))
	if err != nil || len(versions) == 0 {
		t.Fatalf("versions: %v %v", versions, err)
	}
	record := ".kander/versions/" + filepath.Base(versions[0])
	recordBytes, err := os.ReadFile(versions[0])
	if err != nil {
		t.Fatal(err)
	}
	same := ".kander/versions/." + filepath.Base(versions[0]) + ".3727038.1789464141307356827.tmp"
	different := card + "/.spec.md.12.34.tmp"
	missing := card + "/attachments/.gone.md.5.6.tmp"
	remnants = map[string]string{same: string(recordBytes), different: "an older spec", missing: "never renamed"}
	for rel, text := range remnants {
		writeImportFixture(t, source, rel, text)
	}
	// In path order: ".kander" sorts first, and "." before "attachments".
	warnings = []api.ImportWarning{
		{TaskIDs: []string{}, Message: config.Text("server.import_remnant_same", same, record)},
		{TaskIDs: []string{id}, Message: config.Text("server.import_remnant_different", different, card+"/spec.md")},
		{TaskIDs: []string{id}, Message: config.Text("server.import_remnant_missing", missing, card+"/attachments/gone.md")},
	}
	return source, remnants, warnings
}

func assertRemnantsSkipped(t *testing.T, s *Server, project string, remnants map[string]string) {
	t.Helper()
	p, err := s.project(mustProject(t, s, project))
	if err != nil {
		t.Fatal(err)
	}
	published := scanImportTestTree(t, p.board)
	for rel := range remnants {
		if slices.Contains(published.files, rel) {
			t.Fatalf("remnant %s was published", rel)
		}
	}
}

func TestImportSkipsAtomicWriteRemnantsWithAWarningEach(t *testing.T) {
	source, remnants, want := remnantBoard(t)
	s, url, _, _ := openLogged(t, t.TempDir())
	result, err := s.Import("demo", "", source)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(result.Warnings, want) {
		t.Fatalf("warnings:\n%+v\nwant\n%+v", result.Warnings, want)
	}
	if encoded, _ := json.Marshal(result.Warnings[0]); !strings.Contains(string(encoded), `"task_ids":[]`) {
		t.Fatalf("a .kander remnant must encode an empty task list: %s", encoded)
	}
	if result.Files != len(scanImportTestTree(t, source).files) {
		t.Fatalf("files %d must count the whole source", result.Files)
	}
	assertRemnantsSkipped(t, s, "demo", remnants)
	for rel, text := range remnants {
		if data, err := os.ReadFile(filepath.Join(source, filepath.FromSlash(rel))); err != nil || string(data) != text {
			t.Fatalf("source remnant %s changed: %q %v", rel, data, err)
		}
	}
	again, err := s.Import("demo", "", source)
	if err != nil || !again.Existing || !reflect.DeepEqual(again.Warnings, want) {
		t.Fatalf("repeated import: %+v %v", again, err)
	}

	loopback, err := onlineImport(t, s, url, api.ImportRequest{Project: "loopback", From: source})
	if err != nil {
		t.Fatal(err)
	}
	sameImportOutcome(t, result, loopback)
	assertRemnantsSkipped(t, s, "loopback", remnants)
	uploaded, err := uploadTestBoard(t, url, api.ImportRequest{Project: "uploaded", From: source})
	if err != nil {
		t.Fatal(err)
	}
	sameImportOutcome(t, result, uploaded)
	assertRemnantsSkipped(t, s, "uploaded", remnants)
}

func TestMergeTreatsACardThatDiffersOnlyByARemnantAsExisting(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	source := v1Board(t)
	if _, err := s.Import("demo", "", source); err != nil {
		t.Fatal(err)
	}
	card := idleCardDir(t, source)
	writeImportFixture(t, source, card+"/.spec.md.12.34.tmp", "an older spec")
	result, err := s.Import("demo", "", source)
	if err != nil || result.Existing || !slices.Contains(result.ExistingTasks, path.Base(card)) || len(result.SkippedConflicts) != 0 || len(result.Warnings) != 1 {
		t.Fatalf("merge: %+v %v", result, err)
	}
}

func TestNamesThatOnlyLookLikeRemnantsFollowTheExistingRules(t *testing.T) {
	names := []string{".prompt.txt.tmp", ".prompt.txt.abc.123.tmp", "prompt.txt.1.2.tmp", ".prompt.txt.0.2.tmp", "..1.2.tmp"}
	for _, name := range names {
		t.Run("record "+name, func(t *testing.T) {
			s, h := openTest(t, t.TempDir(), nil)
			defer h.Close()
			defer s.Close()
			source := v1Board(t)
			writeImportFixture(t, source, ".kander/versions/"+name, "x")
			if _, err := s.Import("demo", "", source); !api.IsCode(err, api.CodeBoardRejected) || !strings.Contains(err.Error(), "unrecognized or unrecovered v1 board record") {
				t.Fatalf("import: %v", err)
			}
		})
	}
	for _, name := range append(names, ".notes.md.1.2.tmp/inside.txt") {
		t.Run("card "+name, func(t *testing.T) {
			s, h := openTest(t, t.TempDir(), nil)
			defer h.Close()
			defer s.Close()
			source := v1Board(t)
			rel := idleCardDir(t, source) + "/" + name
			writeImportFixture(t, source, rel, "x")
			assertImportedAsIs(t, s, source, rel)
		})
	}
	t.Run("board root", func(t *testing.T) {
		s, h := openTest(t, t.TempDir(), nil)
		defer h.Close()
		defer s.Close()
		source := v1Board(t)
		writeImportFixture(t, source, ".notes.md.1.2.tmp", "x")
		assertImportedAsIs(t, s, source, ".notes.md.1.2.tmp")
	})
}

func assertImportedAsIs(t *testing.T, s *Server, source, rel string) {
	t.Helper()
	result, err := s.Import("demo", "", source)
	if err != nil || len(result.Warnings) != 0 {
		t.Fatalf("import: %+v %v", result, err)
	}
	p, _ := s.project(mustProject(t, s, "demo"))
	if !slices.Contains(scanImportTestTree(t, p.board).files, rel) {
		t.Fatalf("%s was not copied", rel)
	}
}
