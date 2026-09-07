package board

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

// Only ordinary non-card files are harmless to init when there is no migration.
// Missing specs, conflicting IDs, reparse points and migration artifacts retain
// their failure semantics; none is selected, removed or silently ignored.
func migrationStructure(root string, b Board, changes bool) error {
	if len(b.Problems) == 0 {
		return nil
	}
	harmless := !changes && len(b.Blocked) == 0
	messages := make([]string, 0, len(b.Problems))
	for _, problem := range b.Problems {
		messages = append(messages, problem.Message)
		regular, err := fs.RegularFileExists(root, problem.Path)
		if err != nil || !regular || strings.HasSuffix(filepath.Base(problem.Path), "-task.md") {
			harmless = false
		}
	}
	if !harmless {
		return kanbanError("board.migration_structure", strings.Join(messages, "\n"))
	}
	os.Stderr.WriteString(t("board.kander_warning_ignored_invalid_entries_run_kander_check_for", itoa(len(b.Problems))))
	return nil
}
