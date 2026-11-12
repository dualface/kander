package board

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestImportCheckPreservesUnresolvedHistoricalReviewAndRejectsDamage(t *testing.T) {
	s := defaultStrayBoard(t, "import-history")
	before := s.strayEvidence(t)
	code, _, problems, err := CheckBoard(s.root, nil, true)
	if err != nil || code == 0 || !strings.Contains(strings.Join(problems, "\n"), "unplanned execution batch") {
		t.Fatalf("normal gate: %d %v %v", code, problems, err)
	}
	code, _, problems, err = CheckBoardForImport(s.root)
	if err != nil || code != 0 {
		t.Fatalf("import storage consistency: %d %v %v", code, problems, err)
	}
	if after := s.strayEvidence(t); !reflect.DeepEqual(after, before) {
		t.Fatal("import check changed historical bytes")
	}
	// Import validation is a separate storage check; it grants neither closure
	// nor completion and cannot suppress a broken immutable report digest.
	if _, err := IntegrationReviewProgress(s.root, s.ids[0]); err == nil {
		t.Fatal("import check bypassed ordinary completion")
	}
	snapshot, err := ReadSnapshot(s.root, s.ids[0])
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(snapshot.Entry.Path, "reviews", s.run.RunID, "report.md")
	if err := os.WriteFile(path, []byte("tampered\n"), 0600); err != nil {
		t.Fatal(err)
	}
	code, _, _, err = CheckBoardForImport(s.root)
	if err == nil && code == 0 {
		t.Fatal("damaged historical original passed import consistency")
	}
}
