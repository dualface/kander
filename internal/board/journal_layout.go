package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

var journalName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*\.json$`)
var journalTemporaryName = regexp.MustCompile(`^\.[A-Za-z0-9][A-Za-z0-9_-]*\.json\.[0-9]+\.[0-9]+\.tmp$`)

type journalFile struct {
	name      string
	partition string
}

func (f journalFile) path(root string) string {
	return control(root, "operations", f.partition, f.name)
}

// journalFiles checks names and object types without opening record contents.
// The caller holds journal.lock until every returned path has been consumed.
func journalFiles(root string) ([]journalFile, error) {
	var files []journalFile
	seen := map[string]bool{}
	for _, partition := range []string{"", "pending", "committed"} {
		entries, err := fs.ListDirectory(root, control(root, "operations", partition))
		if err != nil {
			return nil, err
		}
		for _, entry := range entries {
			if partition == "" && (entry.Name == "pending" || entry.Name == "committed") && entry.Kind == fs.KindDirectory {
				continue
			}
			// Atomic-write remnants are never records. Preserve them for diagnosis.
			if entry.Kind == fs.KindFile && journalTemporaryName.MatchString(entry.Name) {
				continue
			}
			key := strings.ToLower(entry.Name)
			if entry.Kind != fs.KindFile || !journalName.MatchString(entry.Name) || seen[key] {
				return nil, kanbanError("board.transaction_invalid", entry.Name)
			}
			seen[key] = true
			files = append(files, journalFile{entry.Name, partition})
		}
	}
	return files, nil
}

func readJournalRecord(root string, file journalFile) (r OperationRecord, err error) {
	data, err := fs.ReadRegularFile(root, file.path(root))
	if err != nil {
		return r, err
	}
	if err = json.Unmarshal(data, &r); err != nil {
		return r, err
	}
	if r.Schema != 1 || r.ID+".json" != file.name || (r.Phase != "prepared" && r.Phase != "committed") || (file.partition == "committed" && r.Phase != "committed") {
		return r, kanbanError("board.transaction_invalid", file.name)
	}
	return r, nil
}

func readPendingRecords(root string, warnings ...*WarningLog) ([]OperationRecord, error) {
	files, err := journalFiles(root)
	if err != nil {
		return nil, err
	}
	var records []OperationRecord
	legacy := false
	for _, file := range files {
		if file.partition == "" {
			legacy = true
		}
	}
	if legacy {
		journalWarning(t("board.journal_legacy"), warnings...)
	}
	for _, file := range files {
		if file.partition == "committed" {
			continue
		}
		r, err := readJournalRecord(root, file)
		if err != nil {
			return nil, err
		}
		records = append(records, r)
	}
	return records, nil
}

// partitionJournal runs only under exclusive board access after recovery
// preflight. Rename preserves legacy bytes and timestamps, one record at a time.
func partitionJournal(root string, records []OperationRecord, options InitOptions) error {
	return partitionJournalWithCheckpoint(root, records, options, func(string) error { return nil })
}

func partitionJournalWithCheckpoint(root string, records []OperationRecord, options InitOptions, checkpoint func(string) error) error {
	var files []journalFile
	if err := withJournalLock(root, true, func() (err error) { files, err = journalFiles(root); return err }); err != nil {
		return err
	}
	legacy := false
	for _, file := range files {
		legacy = legacy || file.partition == ""
	}
	if legacy && !options.Maintenance {
		b, err := scan(root)
		if err != nil {
			return err
		}
		var active []string
		for _, entry := range b.Entries {
			if maintenanceRequired(entry.State) {
				active = append(active, entry.TaskID)
			}
		}
		if len(active) > 0 {
			sort.Strings(active)
			return kanbanError("board.migration_maintenance", strings.Join(active, ", "))
		}
	}
	phases := map[string]string{}
	for _, record := range records {
		phases[record.ID+".json"] = record.Phase
	}
	count := 0
	err := withJournalLock(root, false, func() error {
		for _, file := range files {
			phase := phases[file.name]
			partition := "pending"
			if phase == "committed" {
				partition = "committed"
			} else if phase != "prepared" {
				return kanbanError("board.transaction_invalid", file.name)
			}
			if file.partition == partition {
				continue
			}
			if err := fs.Rename(root, file.path(root), control(root, "operations", partition, file.name)); err != nil {
				return err
			}
			count++
			if err := checkpoint("journal-partitioned"); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		_, _ = os.Stderr.WriteString(t("board.journal_partitioned", itoa(count)) + "\n")
	}
	return err
}

func commitOperation(root, path string, record *OperationRecord, checkpoint func(string) error, warnings ...*WarningLog) error {
	return withJournalLock(root, false, func() error {
		record.Phase = "committed"
		if err := writeJSON(root, path, record, true); err != nil {
			return err
		}
		if err := checkpoint("journal-committed-phase"); err != nil {
			return err
		}
		target := control(root, "operations", "committed", record.ID+".json")
		if filepath.Clean(path) != target {
			if err := fs.Rename(root, path, target); err != nil {
				return err
			}
		}
		warnJournalCleanup(pruneCommitted(root, checkpoint), warnings...)
		return nil
	})
}
