package board

import (
	"errors"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dualface/kander/internal/fs"
)

// FormMigration is a redo operation owned by the normal board journal. Staging
// lives on the same volume under .kander/migrations/<operation>/<task>/spec.md.
// Source removal and target publication are separate, recoverable steps.
type FormMigration struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Before string `json:"before"`
	After  string `json:"after"`
}

// InitOptions records an operator's explicit maintenance-window acknowledgement.
// It cannot prove that arbitrary external editors or old agents have stopped.
type InitOptions struct{ Maintenance bool }

func maintenanceRequired(state string) bool { return state == "working" || state == "review" }

// MigrateCards takes exclusive board access before recovery, preflight and all
// publications. Cooperating readers/writers cannot observe the internal gap.
func MigrateCards(root string, options InitOptions) (count int, err error) {
	locks, err := acquire(root, LockScope{ExclusiveBoard: true})
	if err != nil {
		return 0, err
	}
	defer func() { err = errors.Join(err, locks.close()) }()
	records, err := operationRecords(root)
	if err != nil {
		return 0, err
	}
	if err = validateMigrationStaging(root, records); err != nil {
		return count, err
	}
	if err = requireMigrationWindow(root, records, options.Maintenance); err != nil {
		return count, err
	}

	for _, record := range records {
		if record.Phase == "prepared" {
			if err = applyRecord(root, control(root, "operations", record.ID+".json"), &record); err != nil {
				return count, err
			}
			if record.Purpose == "migration" || len(record.Migrations) > 0 {
				count += len(record.Revisions)
			}
		}
	}
	b, err := scan(root)
	if err != nil {
		return count, err
	}
	if len(b.Problems) > 0 {
		return count, errors.New(b.Problems[0].Message)
	}
	type candidate struct {
		entry         Entry
		before, after string
	}
	var candidates []candidate
	for _, e := range selectedEntries(b.Entries, "") {
		text, err := readDocument(e)
		if err != nil {
			return count, err
		}
		size, err := taskSize(e, text)
		if err != nil {
			return count, err
		}
		if !e.IsDirectory() || len(fieldLines(text, FieldSize)) == 0 {
			candidates = append(candidates, candidate{e, text, addSize(text, size)})
		}
	}
	if len(candidates) == 0 {
		return count, nil
	}
	// Even an unchanged active card can have an in-flight legacy writer. Require
	// the whole board's execution/delivery window to be quiet before migration.
	if !options.Maintenance {
		var active []string
		for _, e := range b.Entries {
			if maintenanceRequired(e.State) {
				active = append(active, e.TaskID)
			}
		}
		sort.Strings(active)
		if len(active) > 0 {
			return count, kanbanError("board.migration_maintenance", strings.Join(active, ", "))
		}
	}
	for _, c := range candidates {
		id, err := operationID()
		if err != nil {
			return count, err
		}
		v, err := revision(root, c.entry.TaskID)
		if err != nil {
			return count, err
		}
		if v == ^uint64(0) {
			return count, kanbanError("board.transaction_invalid", "revision overflow")
		}
		record := OperationRecord{Schema: 1, Purpose: "migration", ID: id, Phase: "prepared", Revisions: map[string]uint64{c.entry.TaskID: v + 1}}
		from, _ := filepath.Rel(root, c.entry.Path)
		if c.entry.IsDirectory() {
			doc, _ := filepath.Rel(root, c.entry.Document)
			record.Files = []FileChange{{Path: doc, Before: &c.before, After: c.after}}
		} else {
			record.Migrations = []FormMigration{{From: from, To: strings.TrimSuffix(from, ".md"), Before: c.before, After: c.after}}
		}
		path := control(root, "operations", id+".json")
		if err = validateRecord(root, &record); err != nil {
			return count, err
		}
		if err = writeOperation(root, path, record, false); err != nil {
			return count, err
		}
		if err = applyRecord(root, path, &record); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

func applyMigration(root string, record *OperationRecord, m FormMigration, checkpoint func(string) error) error {
	from := filepath.Join(root, m.From)
	to := filepath.Join(root, m.To)
	stageParent := control(root, "migrations", record.ID)
	stage := filepath.Join(stageParent, filepath.Base(m.To))
	stagedSpec := filepath.Join(stage, "spec.md")
	source, sourceExists, err := fs.ReadRegularFileIfExists(root, from)
	if err != nil {
		return err
	}
	targetExists, err := fs.DirectoryExists(root, to)
	if err != nil {
		return err
	}
	stageExists, err := fs.DirectoryExists(root, stage)
	if err != nil {
		return err
	}
	if targetExists {
		if sourceExists || stageExists {
			return kanbanError("board.transaction_conflict", m.To)
		}
		text, err := fs.ReadRegularFile(root, filepath.Join(to, "spec.md"))
		if err != nil {
			return err
		}
		if string(text) != m.After {
			return kanbanError("board.transaction_conflict", m.To)
		}
		return nil
	}
	if sourceExists && string(source) != m.Before {
		return kanbanError("board.transaction_conflict", m.From)
	}
	if stageExists {
		items, err := fs.ListDirectory(root, stage)
		if err != nil {
			return err
		}
		for _, item := range items {
			if item.Name != "spec.md" || item.Kind != fs.KindFile {
				return kanbanError("board.transaction_conflict", stage)
			}
		}
	} else {
		if !sourceExists {
			return kanbanError("board.transaction_conflict", m.From)
		}
		if err = fs.EnsurePrivateDirectory(root, stageParent, true); err != nil {
			return err
		}
		if err = fs.CreatePrivateDirectory(root, stage); err != nil {
			return err
		}
	}
	if err := checkpoint("migration-stage"); err != nil {
		return err
	}
	staged, exists, err := fs.ReadRegularFileIfExists(root, stagedSpec)
	if err != nil {
		return err
	}
	if sourceExists {
		if exists {
			return kanbanError("board.transaction_conflict", m.From)
		}
		if err = fs.Rename(root, from, stagedSpec); err != nil {
			return err
		}
		staged, exists = source, true
	}
	if err := checkpoint("migration-source"); err != nil {
		return err
	}
	if !exists || (string(staged) != m.Before && string(staged) != m.After) {
		return kanbanError("board.transaction_conflict", stagedSpec)
	}
	if string(staged) != m.After {
		if err = fs.WriteTextAtomic(root, stagedSpec, m.After, true); err != nil {
			return err
		}
	}
	if err := checkpoint("migration-size"); err != nil {
		return err
	}
	if err := fs.Rename(root, stage, to); err != nil {
		return err
	}
	return checkpoint("migration-publish")
}

func validateMigration(root string, record *OperationRecord, m FormMigration) error {
	if filepath.Base(record.ID) != record.ID || strings.HasPrefix(record.ID, ".") || strings.ContainsAny(record.ID, "\\:/") {
		return kanbanError("board.transaction_invalid", record.ID)
	}

	if _, err := safeRecordPath(root, m.From); err != nil {
		return err
	}
	if _, err := safeRecordPath(root, m.To); err != nil {
		return err
	}
	parts := strings.Split(filepath.ToSlash(m.To), "/")
	if len(parts) != 2 || !taskIDRe.MatchString(parts[1]) || m.From != m.To+".md" || record.Revisions[parts[1]] == 0 {
		return kanbanError("board.transaction_invalid", m.From)
	}
	size, err := taskSize(Entry{Path: m.From, TaskID: parts[1]}, m.Before)
	if err != nil {
		return err
	}
	if m.After != addSize(m.Before, size) {
		return kanbanError("board.transaction_invalid", m.To)
	}
	return nil
}

func validateMigrationStaging(root string, records []OperationRecord) error {
	known := map[string]OperationRecord{}
	for _, record := range records {
		if len(record.Migrations) > 0 {
			known[record.ID] = record
		}
	}
	items, err := fs.ListDirectory(root, control(root, "migrations"))
	if err != nil {
		return err
	}
	for _, item := range items {
		record, ok := known[item.Name]
		if !ok || item.Kind != fs.KindDirectory {
			return kanbanError("board.transaction_invalid", item.Name)
		}
		stages, err := fs.ListDirectory(root, control(root, "migrations", item.Name))
		if err != nil {
			return err
		}
		for _, stage := range stages {
			matched := false
			for _, migration := range record.Migrations {
				if stage.Name == filepath.Base(migration.To) && stage.Kind == fs.KindDirectory && record.Phase == "prepared" {
					matched = true
				}
			}
			if !matched {
				return kanbanError("board.transaction_conflict", stage.Name)
			}
		}
	}
	return nil
}

func requireMigrationWindow(root string, records []OperationRecord, acknowledged bool) error {
	if acknowledged {
		return nil
	}
	needsWindow := false
	var active []string
	for _, record := range records {
		if record.Phase != "prepared" || (record.Purpose != "migration" && len(record.Migrations) == 0) {
			continue
		}
		needsWindow = true
		for _, migration := range record.Migrations {
			if maintenanceRequired(strings.Split(filepath.ToSlash(migration.From), "/")[0]) {
				active = append(active, migration.From)
			}
		}
		for _, file := range record.Files {
			if maintenanceRequired(strings.Split(filepath.ToSlash(file.Path), "/")[0]) {
				active = append(active, file.Path)
			}
		}
	}
	if !needsWindow {
		return nil
	}
	b, err := scan(root)
	if err != nil {
		return err
	}
	for _, e := range b.Entries {
		if maintenanceRequired(e.State) {
			active = append(active, e.TaskID)
		}
	}
	if len(active) > 0 {
		sort.Strings(active)
		return kanbanError("board.migration_maintenance", strings.Join(uniqueKeepOrder(active), ", "))
	}
	return nil
}
