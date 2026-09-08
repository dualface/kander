package board

import (
	"errors"
	"sort"
	"time"

	"github.com/dualface/kander/internal/fs"
)

// Keep 100 recent commits for local diagnosis while bounding normal journal
// storage and metadata scans. Migration staging evidence is retained separately.
const committedJournalRetention = 100

func warnJournalCleanup(err error, warnings ...*WarningLog) {
	if err != nil {
		journalWarning(t("board.journal_cleanup_warning", err.Error()), warnings...)
	}
}

func pruneJournal(root string) {
	warnJournalCleanup(withJournalLock(root, false, func() error {
		return pruneCommitted(root, func(string) error { return nil })
	}))
}

// pruneCommitted holds the exclusive journal lock. It never parses old redo
// images; staging directory identities conservatively pin matching records.
func pruneCommitted(root string, checkpoint func(string) error) error {
	files, err := journalFiles(root)
	if err != nil {
		return err
	}
	staging, err := fs.ListDirectory(root, control(root, "migrations"))
	if err != nil {
		return err
	}
	protected := map[string]bool{}
	for _, entry := range staging {
		if entry.Kind != fs.KindDirectory {
			return kanbanError("board.transaction_invalid", entry.Name)
		}
		protected[entry.Name+".json"] = true
	}
	type candidate struct {
		file journalFile
		time time.Time
	}
	var committed []candidate
	for _, file := range files {
		if file.partition != "committed" {
			continue
		}
		handle, err := fs.OpenRegularFileIfExists(root, file.path(root))
		if err != nil {
			return err
		}
		if handle == nil {
			return kanbanError("board.transaction_conflict", file.name)
		}
		info, statErr := handle.Stat()
		if err := errors.Join(statErr, handle.Close()); err != nil {
			return err
		}
		committed = append(committed, candidate{file, info.ModTime()})
	}
	sort.Slice(committed, func(i, j int) bool {
		if committed[i].time.Equal(committed[j].time) {
			return committed[i].file.name > committed[j].file.name
		}
		return committed[i].time.After(committed[j].time)
	})
	for i := committedJournalRetention; i < len(committed); i++ {
		file := committed[i].file
		if protected[file.name] {
			continue
		}
		if _, err := fs.RemoveRegularFileIfExists(root, file.path(root)); err != nil {
			return err
		}
		if err := checkpoint("journal-pruned"); err != nil {
			return err
		}
	}
	return nil
}
