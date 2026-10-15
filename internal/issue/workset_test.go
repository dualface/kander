package issue

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dualface/kander/internal/board"
)

func sourceLine(key, anchor string) string {
	return "SOURCE_ISSUE: " + key + " (anchor: " + anchor + ")"
}

// siblingFixture creates a small card whose DISCUSSION starts with the given
// text, then places it in state with the given RESULT. Only the isolated test
// board is written.
func siblingFixture(t *testing.T, root, slug, discussion, state, result string) string {
	t.Helper()
	path, err := board.NewTask(root, "feature", slug, "Sibling "+slug, "en", false)
	if err != nil {
		t.Fatal(err)
	}
	id := filepath.Base(path)
	spec := filepath.Join(path, "spec.md")
	data, err := os.ReadFile(spec)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(data), "## DISCUSSION\n", "## DISCUSSION\n\n"+discussion+"\n", 1)
	text = strings.Replace(text, "- [ ] <FILL_IN>", "- [x] Sibling "+slug+" delivered", 1)
	text = strings.Replace(text, "## SUMMARY\n", "## SUMMARY\n\nSibling "+slug+" verified: go vet ./...\n", 1)
	if result != "" {
		text = strings.Replace(text, "- RESULT:\n", "- RESULT: "+result+"\n", 1)
	}
	if err := os.WriteFile(spec, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	if state != "backlog" {
		if err := os.Rename(path, filepath.Join(root, state, id)); err != nil {
			t.Fatal(err)
		}
	}
	return id
}

// moveFixture relocates a card directory and rewrites its RESULT, standing in
// for lifecycle moves whose gates are covered by the board package.
func moveFixture(t *testing.T, root, id, from, to, result string) {
	t.Helper()
	target := filepath.Join(root, to, id)
	if err := os.Rename(filepath.Join(root, from, id), target); err != nil {
		t.Fatal(err)
	}
	spec := filepath.Join(target, "spec.md")
	data, err := os.ReadFile(spec)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		if strings.HasPrefix(line, "- RESULT:") {
			lines[i] = strings.TrimSpace("- RESULT: " + result)
			if result == "" {
				lines[i] = "- RESULT:"
			}
		}
	}
	if err := os.WriteFile(spec, []byte(strings.Join(lines, "\n")), 0600); err != nil {
		t.Fatal(err)
	}
}

func loadFixtureWorkSet(t *testing.T, root, key string) WorkSet {
	t.Helper()
	sets, err := LoadWorkSets(root)
	if err != nil {
		t.Fatal(err)
	}
	set, ok := sets[key]
	if !ok {
		t.Fatalf("no work set for %s", key)
	}
	return set
}

func workIDs(cards []WorkCard) []string {
	ids := []string{}
	for _, card := range cards {
		ids = append(ids, card.TaskID)
	}
	return ids
}

func TestWorkSetAnchorOnly(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	set := loadFixtureWorkSet(t, root, key)
	if !reflect.DeepEqual(workIDs(set.Members()), []string{id}) || len(set.Excluded) != 0 || !set.Ready() {
		t.Fatalf("work set = %+v", set)
	}
}

func TestWorkSetIncludesValidSiblings(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	a := siblingFixture(t, root, "sib-a", sourceLine(key, id), "done", "completed")
	b := siblingFixture(t, root, "sib-b", sourceLine(key, id), "working", "")
	set := loadFixtureWorkSet(t, root, key)
	if !reflect.DeepEqual(workIDs(set.Members()), []string{id, a, b}) {
		t.Fatalf("members = %v", workIDs(set.Members()))
	}
	if set.Ready() || !reflect.DeepEqual(set.Pending(), []string{b}) {
		t.Fatalf("pending = %v", set.Pending())
	}
	if done, total := set.Progress(); done != 2 || total != 3 {
		t.Fatalf("progress = %d/%d", done, total)
	}
}

func TestWorkSetAcceptsListPrefixAndCodeBlock(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	listed := siblingFixture(t, root, "sib-list", "  - "+sourceLine(key, id)+"  ", "done", "")
	fenced := siblingFixture(t, root, "sib-fenced", "```text\nPREREQUISITES: N/A\n"+sourceLine(key, id)+"\n```", "done", "")
	set := loadFixtureWorkSet(t, root, key)
	if !reflect.DeepEqual(workIDs(set.Siblings), []string{fenced, listed}) {
		t.Fatalf("siblings = %v", workIDs(set.Siblings))
	}
}

func TestWorkSetRejectsMismatchedKey(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	other, _ := repo.IssueSourceKey(43)
	siblingFixture(t, root, "sib-key", sourceLine(other, id), "working", "")
	if set := loadFixtureWorkSet(t, root, key); len(set.Siblings)+len(set.Excluded) != 0 {
		t.Fatalf("mismatched key joined: %+v", set)
	}
}

func TestWorkSetRejectsMismatchedAnchor(t *testing.T) {
	root, repo, _, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	siblingFixture(t, root, "sib-anchor", sourceLine(key, "20260101-other-task"), "working", "")
	if set := loadFixtureWorkSet(t, root, key); len(set.Siblings)+len(set.Excluded) != 0 {
		t.Fatalf("mismatched anchor joined: %+v", set)
	}
}

func TestWorkSetRejectsExtraText(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	siblingFixture(t, root, "sib-extra", "Note: "+sourceLine(key, id), "working", "")
	siblingFixture(t, root, "sib-trailing", sourceLine(key, id)+" for docs", "working", "")
	if set := loadFixtureWorkSet(t, root, key); len(set.Siblings)+len(set.Excluded) != 0 {
		t.Fatalf("line with extra text joined: %+v", set)
	}
}

func TestWorkSetExcludesTrashedSibling(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	trashed := siblingFixture(t, root, "sib-trash", sourceLine(key, id), "trash", "trashed")
	set := loadFixtureWorkSet(t, root, key)
	if len(set.Siblings) != 0 || !reflect.DeepEqual(set.Excluded, []WorkCard{{TaskID: trashed, State: "trash", Result: "trashed"}}) || !set.Ready() {
		t.Fatalf("work set = %+v", set)
	}
}

func TestWorkSetExcludesCancelledArchivedSibling(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	cancelled := siblingFixture(t, root, "sib-cancel", sourceLine(key, id), "archived", "cancelled")
	set := loadFixtureWorkSet(t, root, key)
	if len(set.Siblings) != 0 || !reflect.DeepEqual(set.Excluded, []WorkCard{{TaskID: cancelled, State: "archived", Result: "cancelled"}}) || !set.Ready() {
		t.Fatalf("work set = %+v", set)
	}
}

func TestIndexCarriesWorkProgress(t *testing.T) {
	root, repo, id, _ := resultFixture(t)
	key, _ := repo.IssueSourceKey(42)
	siblingFixture(t, root, "sib-progress", sourceLine(key, id), "review", "")
	index, err := LoadIndex(root)
	if err != nil {
		t.Fatal(err)
	}
	local := index[key]
	if local.WorkDone != 1 || local.WorkTotal != 2 || local.WorkReady || local.ResultPending || local.Number != 42 || local.Repository.Name != "kander" {
		t.Fatalf("local = %+v", local)
	}
}
