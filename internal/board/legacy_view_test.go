package board

import (
	"os"
	"strings"
	"testing"
)

func TestCommittedViewReadsALegacyCardWithEnglishNames(t *testing.T) {
	root := tempBoard(t)
	card := writeLegacyDirCard(t, root, "legacy-view", legacyDirCard)
	id := card.Entry.TaskID
	err := WithTransaction(root, LockScope{Tasks: []string{id}, ExclusiveBoard: true}, func(tx *Transaction) error {
		view, err := tx.ReadOnly().CommittedView()
		if err != nil {
			return err
		}
		text, err := view.Document(id)
		if err != nil {
			return err
		}
		if want, _ := normalizeLegacyCard(legacyDirCard); text != want {
			t.Fatalf("committed view differs from the normalized card:\n%s", text)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A view sent by a server that does not convert legacy cards is converted when
// the client restores it, and a view that is already English is left alone.
func TestRestoreViewConvertsLegacyText(t *testing.T) {
	english, _ := normalizeLegacyCard(legacyDirCard)
	view := CapturedView{Location: "project:p", Cards: []CapturedCard{
		{TaskID: "20260906-old-task", State: "done", Kind: "large", Text: legacyDirCard, Revision: 1},
		{TaskID: "20260906-new-task", State: "done", Kind: "large", Text: english, Revision: 2},
	}}
	restored, err := RestoreView(view)
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"20260906-old-task", "20260906-new-task"} {
		text, err := restored.Document(id)
		if err != nil || text != english {
			t.Fatalf("%s: restored text differs: %v\n%s", id, err, text)
		}
	}
	if got := MetadataFrom(english, FieldResult); got != "completed" {
		t.Fatalf("result %q", got)
	}
}

// The v1 import writes the staged copy, which the server compares with the
// source bytes, so the default SIZE line goes into the card as it is on disk.
func TestDefaultMissingSizeKeepsTheBytesOfALegacyCard(t *testing.T) {
	root := tempBoard(t)
	noSize := strings.Replace(legacyDirCard, "- SIZE: small\n", "", 1)
	card := writeLegacyDirCard(t, root, "legacy-size", noSize)
	changed, err := DefaultMissingSize(root)
	if err != nil || len(changed) != 1 || changed[0].TaskID != card.Entry.TaskID {
		t.Fatalf("defaulted %+v: %v", changed, err)
	}
	disk, err := os.ReadFile(card.Entry.Document)
	if err != nil {
		t.Fatal(err)
	}
	if string(disk) != DefaultedSizeText(noSize) || len(disk) != len(noSize)+len("- SIZE: large\n") || !strings.Contains(string(disk), "- 类型: Bug\n") {
		t.Fatalf("the card changed beyond the inserted SIZE line:\n%s", disk)
	}
}
