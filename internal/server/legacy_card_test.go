package server

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

var legacyCreatedLineRe = regexp.MustCompile(`(?m)^- CREATED_AT:.*$`)

// legacyForm rewrites a card into the form of one created before the schema
// rename: Chinese schema names, a creation time before the cutoff, no SIZE line.
func legacyForm(text string) string {
	text = legacyCreatedLineRe.ReplaceAllString(text, "- 创建时间: 2026-09-06 23:30")
	text = strings.Replace(text, "- SIZE: small\n", "", 1)
	for _, pair := range [][2]string{
		{"- TYPE:", "- 类型:"},
		{"- OWNER:", "- 负责人:"},
		{"- RESULT:", "- 结果:"},
		{"## GOAL", "## 任务目标"},
		{"## DISCUSSION", "## 讨论与决策"},
		{"<FILL_IN>", "<填写>"},
	} {
		text = strings.ReplaceAll(text, pair[0], pair[1])
	}
	return text
}

// A v1 board that holds a card of the old schema imports unchanged, and every
// way the server shows the card afterwards reads it with English names.
func TestImportedLegacyCardIsReadWithEnglishNames(t *testing.T) {
	source := v1Board(t)
	matches, err := filepath.Glob(filepath.Join(source, "backlog", "*", "spec.md"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("backlog fixture: %v %v", matches, err)
	}
	id := filepath.Base(filepath.Dir(matches[0]))
	editSpec(t, source, "backlog", id, legacyForm)
	original, err := os.ReadFile(matches[0])
	if err != nil {
		t.Fatal(err)
	}
	_, before, err := snapshotTree(source)
	if err != nil {
		t.Fatal(err)
	}

	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	result, err := s.Import("demo", "", source)
	if err != nil || result.Existing || len(result.DefaultedSize) != 1 || result.DefaultedSize[0] != id {
		t.Fatalf("import: %+v %v", result, err)
	}
	if _, after, _ := snapshotTree(source); after != before {
		t.Fatal("the source board must stay byte-identical")
	}
	proj, err := s.project(result.Project.ID)
	if err != nil {
		t.Fatal(err)
	}
	published, err := os.ReadFile(filepath.Join(proj.board, "backlog", id, "spec.md"))
	if err != nil {
		t.Fatal(err)
	}
	want := board.DefaultedSizeText(string(original))
	if string(published) != want || len(published) != len(original)+len("- SIZE: large\n") {
		t.Fatalf("the imported card differs from the source by more than the inserted SIZE line:\n%s", published)
	}

	c, _ := client.New(h.URL, "host-b")
	p := c.Project(result.Project.ID)
	shown, err := p.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"## GOAL", "## DISCUSSION", "- TYPE: Feature", "<FILL_IN>"} {
		if !strings.Contains(shown.Text, name) {
			t.Fatalf("show lacks %q:\n%s", name, shown.Text)
		}
	}
	if strings.ContainsAny(shown.Text, "类型任务目标讨论与决策填写负责人") || board.MetadataFrom(shown.Text, board.FieldSize) != "large" || board.MetadataFrom(shown.Text, board.FieldCreatedAt) != "2026-09-06 23:30" {
		t.Fatalf("show carries Chinese schema names or misses a field:\n%s", shown.Text)
	}
	doc, err := p.Document(t.Context(), id, "spec.md")
	if err != nil || doc.Text != shown.Text {
		t.Fatalf("the spec document differs from show: %v\n%s", err, doc.Text)
	}
	snapshot, err := p.Snapshot(t.Context(), api.SnapshotQuery{})
	if err != nil {
		t.Fatal(err)
	}
	restored, err := board.RestoreView(snapshot.View)
	if err != nil {
		t.Fatal(err)
	}
	if text, err := restored.Document(id); err != nil || text != shown.Text {
		t.Fatalf("the restored view differs from show: %v\n%s", err, text)
	}
	if disk, err := os.ReadFile(filepath.Join(proj.board, "backlog", id, "spec.md")); err != nil || string(disk) != want {
		t.Fatalf("reading a legacy card must not write it: %v", err)
	}

	// The next write that changes the text stores English names; a result-like
	// line in the body stays text.
	text := strings.Replace(shown.Text, "## DISCUSSION\n", "## DISCUSSION\n\n- 结果: x\n", 1)
	if _, err = p.PutDocument(t.Context(), "", id, api.Document{Path: "spec.md", Text: text, ExpectedRevision: shown.Revision}, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	disk, err := os.ReadFile(filepath.Join(proj.board, "backlog", id, "spec.md"))
	if err != nil || !strings.Contains(string(disk), "## GOAL") || strings.Contains(string(disk), "## 任务目标") || strings.Count(string(disk), "- 结果: x") != 1 {
		t.Fatalf("the update must store English schema names: %v\n%s", err, disk)
	}
}

// Metadata names in the body of a new card are ordinary text for the server.
func TestBodyFieldLinesDoNotChangeCardMetadataOnTheServer(t *testing.T) {
	s, h := openTest(t, t.TempDir(), nil)
	defer h.Close()
	defer s.Close()
	p, id := prepareTodo(t, h.URL)
	shown, err := p.Show(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(shown.Text, "## DISCUSSION\n", "## DISCUSSION\n\n- RESULT: x\n- OWNER: x\n- 结果: y\n", 1)
	updated, err := p.PutDocument(t.Context(), "", id, api.Document{Path: "spec.md", Text: text, ExpectedRevision: shown.Revision}, nil)
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	for _, field := range []string{board.FieldResult, board.FieldOwner} {
		if value := board.MetadataFrom(updated.Text, field); value != "" {
			t.Fatalf("%s read %q from the body", field, value)
		}
	}
	if !strings.Contains(updated.Text, "- 结果: y") {
		t.Fatalf("the body line is lost:\n%s", updated.Text)
	}
}
