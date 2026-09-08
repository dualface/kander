package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/dualface/kander/internal/fs"
)

// Version is an operation-local cursor. Copies of an Entry share the cursor only
// within that operation; a new snapshot always receives a separate cursor.
type Version struct {
	warnings      *WarningLog
	mu            sync.Mutex
	revision      uint64
	authorization ExecutionAuthorization
}

// Snapshot is a committed, consistent task document and its optimistic version.
type Snapshot struct {
	Entry       Entry  `json:"entry"`
	Revision    uint64 `json:"revision"`
	Text        string `json:"text"`
	OperationID string `json:"operation_id"`
}

// FileChange is a redo step. Paths are board-relative, never cached absolute paths.
// Before=nil means create-only; non-nil requires an existing regular file.
type FileChange struct {
	Path   string  `json:"path"`
	Before *string `json:"before,omitempty"`
	After  string  `json:"after"`
}

// EntryChange creates or renames an entry under an exclusive board lock.
type EntryChange struct {
	From string `json:"from,omitempty"`
	To   string `json:"to"`
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
}

// OperationRecord is the versioned recovery format. A prepared record always
// rolls forward on init. Readers reject prepared records without repairing them.
type OperationRecord struct {
	LinkRelocation bool              `json:"link_relocation,omitempty"`
	Purpose        string            `json:"purpose,omitempty"`
	Schema         int               `json:"schema"`
	ID             string            `json:"operation_id"`
	Phase          string            `json:"phase"`
	Revisions      map[string]uint64 `json:"revisions"`
	Groups         []string          `json:"groups,omitempty"`
	Directories    []string          `json:"directories,omitempty"`
	Files          []FileChange      `json:"files,omitempty"`
	Entries        []EntryChange     `json:"entries,omitempty"`
	Migrations     []FormMigration   `json:"migrations,omitempty"`
}

// Transaction stages multi-file publications under a fixed lock set. Callers
// must use its Snapshot and Put methods, never call locking board APIs inside it.
type Transaction struct {
	root   string
	scope  LockScope
	record OperationRecord
}

// WithTransaction commits all staged writes, or leaves a prepared recovery record
// on publication failure. No user callback runs during recovery.
func WithTransaction(root string, scope LockScope, fn func(*Transaction) error) (err error) {
	return withTransaction(nil, root, scope, fn)
}

// WithTransactionContext bounds lock contention and preparation. Once a redo
// intent is published, completion follows the existing non-cancellable journal
// protocol; OS file operations and crash recovery retain their existing limits.
func WithTransactionContext(ctx context.Context, root string, scope LockScope, fn func(*Transaction) error) error {
	return withTransaction(ctx, root, scope, fn)
}

func withTransaction(ctx context.Context, root string, scope LockScope, fn func(*Transaction) error) (err error) {
	if err = ensureLayout(root); err != nil {
		return err
	}
	var locks lockSet
	if ctx == nil {
		locks, err = acquire(root, scope)
	} else {
		locks, err = acquireContext(ctx, root, scope)
	}
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, locks.close()) }()
	if err = pendingContext(ctx, root, append(append([]string(nil), scope.Tasks...), scope.Groups...), scope.warnings); err != nil {
		return err
	}
	id, err := operationID()
	if err != nil {
		return err
	}
	tx := &Transaction{root: root, scope: scope, record: OperationRecord{Schema: 1, ID: id, Phase: "prepared", Revisions: map[string]uint64{}, Groups: append([]string(nil), scope.Groups...)}}
	if err = fn(tx); err != nil {
		return err
	}
	if ctx != nil {
		if err = ctx.Err(); err != nil {
			return err
		}
	}
	if len(tx.record.Files) == 0 && len(tx.record.Entries) == 0 {
		return nil
	}
	if scope.ReadOnly {
		return kanbanError("board.transaction_invalid", "read-only")
	}
	// Finalize moved document identity after all producer staging, regardless of
	// whether Put or Relocate was called first.
	for i := range tx.record.Entries {
		entry := &tx.record.Entries[i]
		if entry.From == "" {
			continue
		}
		document := entry.From
		if entry.Kind == "large" {
			document = filepath.Join(document, "spec.md")
		}
		for _, file := range tx.record.Files {
			if file.Path == document {
				entry.Text = file.After
			}
		}
	}
	if err = validateRecord(root, &tx.record); err != nil {
		return err
	}
	path := control(root, "operations", "pending", id+".json")
	if err = writeOperation(root, path, tx.record, false); err != nil {
		return err
	}
	return applyRecord(root, path, &tx.record, scope.warnings)
}
func (tx *Transaction) owns(id string) bool {
	for _, v := range tx.scope.Tasks {
		if v == id {
			return true
		}
	}
	return false
}
func (tx *Transaction) touch(id string) error {
	if !tx.owns(id) || tx.scope.ReadOnly {
		return kanbanError("board.transaction_invalid", id)
	}
	if _, ok := tx.record.Revisions[id]; !ok {
		v, err := revision(tx.root, id)
		if err != nil {
			return err
		}
		if v == ^uint64(0) {
			return kanbanError("board.transaction_invalid", "revision overflow")
		}
		tx.record.Revisions[id] = v + 1
	}
	return nil
}

// Snapshot relocates a task while its shared or exclusive task lock is held.
func (tx *Transaction) Snapshot(id string) (Snapshot, error) {
	if !tx.owns(id) {
		return Snapshot{}, kanbanError("board.transaction_invalid", id)
	}
	b, _ := scanTargetsOnce(tx.root, []string{id})
	e, err := Locate(b, id)
	if err != nil {
		return Snapshot{}, err
	}
	v, err := revision(tx.root, id)
	if err != nil {
		return Snapshot{}, err
	}
	text, err := readDocument(e)
	if err != nil {
		return Snapshot{}, err
	}
	e = attachSize(e, text)
	if !tx.scope.ReadOnly {
		if err := ValidateMutable(e, text); err != nil {
			return Snapshot{}, err
		}
	}
	e.Version = &Version{revision: v, authorization: authFrom(text), warnings: tx.scope.warnings}
	last, err := readVersion(tx.root, id)
	if err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Entry: e, Revision: v, Text: text, OperationID: last.OperationID}, nil
}

// Expect rejects stale revisions and states before staging a mutation.
func (tx *Transaction) Expect(id, state string, expected uint64) (Snapshot, error) {
	s, err := tx.Snapshot(id)
	if err != nil {
		return s, err
	}
	if s.Revision != expected || (state != "" && state != s.Entry.State) {
		return Snapshot{}, kanbanError("board.transaction_conflict", id)
	}
	return s, nil
}
func documentPath(e Entry, name string) (string, error) {
	if name == "" || strings.ContainsAny(name, "\\:\x00") || filepath.IsAbs(name) || filepath.ToSlash(filepath.Clean(name)) != name {
		return "", kanbanError("board.transaction_invalid", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." || part == "." || part == "" || strings.HasPrefix(part, ".") || strings.TrimRight(part, " .") != part {
			return "", kanbanError("board.transaction_invalid", name)
		}
	}
	if strings.EqualFold(name, "spec.md") && name != "spec.md" {
		return "", kanbanError("board.transaction_invalid", name)
	}
	if !e.IsDirectory() {
		if name != "spec.md" {
			return "", kanbanError("board.transaction_directory_required")
		}
		return e.Document, nil
	}
	return filepath.Join(e.Path, filepath.FromSlash(name)), nil
}

// Read returns an attachment from the same committed task snapshot.
func (tx *Transaction) Read(id, name string) (string, error) {
	s, err := tx.Snapshot(id)
	if err != nil {
		return "", err
	}
	p, err := documentPath(s.Entry, name)
	if err != nil {
		return "", err
	}
	b, err := fs.ReadRegularFile(tx.root, p)
	return string(b), err
}

// Put stages a file replacement or an attachment creation under an existing card.
// Dedicated producers may publish managed files; CLI Update applies its own gate.
func (tx *Transaction) Put(id, name, text string) error {
	if !utf8.ValidString(text) {
		return kanbanError("board.task_document_is_not_valid_utf_8", name)
	}
	return tx.putBytes(id, name, []byte(text))
}

// PutBytes preserves arbitrary attachment bytes through the redo journal.
func (tx *Transaction) PutBytes(id, name string, data []byte) error {
	if name == "spec.md" {
		return tx.Put(id, name, string(data))
	}
	return tx.putBytes(id, name, data)
}
func (tx *Transaction) putBytes(id, name string, data []byte) error {
	text := string(data)
	s, err := tx.Snapshot(id)
	if err != nil {
		return err
	}
	if name == "spec.md" {
		if _, err := taskSize(s.Entry, text); err != nil {
			return err
		}
	}
	p, err := documentPath(s.Entry, name)
	if err != nil {
		return err
	}
	if s.Entry.IsDirectory() {
		if err = tx.stageParents(s.Entry.Path, filepath.Dir(p)); err != nil {
			return err
		}
	}
	b, exists, err := fs.ReadRegularFileIfExists(tx.root, p)
	if err != nil {
		return err
	}
	if !exists && name == "spec.md" {
		return kanbanError("board.transaction_conflict", id)
	}
	rel, err := filepath.Rel(tx.root, p)
	if err != nil {
		return err
	}
	for _, f := range tx.record.Files {
		if strings.EqualFold(f.Path, rel) {
			return kanbanError("board.transaction_invalid", "duplicate file")
		}
	}
	var before *string
	if exists && name == "spec.md" && !utf8.Valid(b) {
		return kanbanError("board.task_document_is_not_valid_utf_8", p)
	}
	if exists {
		v := string(b)
		before = &v
	}
	if err = tx.touch(id); err != nil {
		return err
	}
	tx.record.Files = append(tx.record.Files, FileChange{rel, before, text})
	return nil
}

// Relocate stages a state move. Form migrations can extend EntryChange without
// changing the lock, version, visibility or recovery protocol.
func (tx *Transaction) Relocate(id, state string) error {
	if !tx.scope.ExclusiveBoard {
		return kanbanError("board.transaction_invalid", "exclusive board lock required")
	}
	if _, ok := stateSet[state]; !ok {
		return kanbanError("board.unknown_state", state)
	}
	s, err := tx.Snapshot(id)
	if err != nil {
		return err
	}
	if err = tx.touch(id); err != nil {
		return err
	}
	from, _ := filepath.Rel(tx.root, s.Entry.Path)
	to := filepath.Join(state, filepath.Base(s.Entry.Path))
	if from != to {
		for _, entry := range tx.record.Entries {
			if entry.From == from {
				return kanbanError("board.transaction_invalid", "duplicate move")
			}
		}
		text := s.Text
		for _, file := range tx.record.Files {
			if filepath.Join(tx.root, file.Path) == s.Entry.Document {
				text = file.After
			}
		}
		tx.record.Entries = append(tx.record.Entries, EntryChange{From: from, To: to, Kind: s.Entry.storageKind(), Text: text})
	}
	return nil
}

// ReadSnapshot provides show/update clients an atomic body, location and revision.
func ReadSnapshot(root, id string) (Snapshot, error) {
	return ReadSnapshotWithWarnings(root, id, nil)
}

// ReadSnapshotWithWarnings routes journal advisories to the operation log.
// The returned Entry retains the log for subsequent reads, moves and writes.
// A nil log preserves the CLI's stderr presentation.
func ReadSnapshotWithWarnings(root, id string, warnings *WarningLog) (s Snapshot, err error) {
	id, err = NormalizeTaskID(id)
	if err != nil {
		return s, err
	}
	err = WithTransaction(root, LockScope{Tasks: []string{id}, ReadOnly: true, warnings: warnings}, func(tx *Transaction) error { var e error; s, e = tx.Snapshot(id); return e })
	return s, err
}

func containsID(ids []string, id string) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}
func (tx *Transaction) stageParents(boundary, parent string) error {
	relative, err := filepath.Rel(boundary, parent)
	if err != nil {
		return err
	}
	if relative == "." {
		return nil
	}
	current := boundary
	for _, part := range strings.Split(relative, string(os.PathSeparator)) {
		if part == ".." {
			return kanbanError("board.transaction_invalid", parent)
		}
		current = filepath.Join(current, part)
		exists, err := fs.DirectoryExists(tx.root, current)
		if err != nil {
			return err
		}
		if !exists {
			rel, _ := filepath.Rel(tx.root, current)
			if !containsID(tx.record.Directories, rel) {
				tx.record.Directories = append(tx.record.Directories, rel)
			}
		}
	}
	return nil
}

// ReadGroup reads a producer-owned control document under the declared group lock.
func (tx *Transaction) ReadGroup(group, name string) ([]byte, bool, error) {
	p, err := tx.groupPath(group, name)
	if err != nil {
		return nil, false, err
	}
	return fs.ReadRegularFileIfExists(tx.root, p)
}
func (tx *Transaction) groupPath(group, name string) (string, error) {
	if !containsID(tx.scope.Groups, group) {
		return "", kanbanError("board.transaction_invalid", group)
	}
	entry := Entry{Path: control(tx.root, "groups", group), Kind: "large"}
	return documentPath(entry, name)
}

// PutGroup publishes a producer-owned checkpoint with task files under the same
// redo record. The producer validates its schema and expected document version.
func (tx *Transaction) PutGroup(group, name, text string) error {
	if !utf8.ValidString(text) {
		return kanbanError("board.task_document_is_not_valid_utf_8", name)
	}
	return tx.putGroupBytes(group, name, []byte(text))
}
func (tx *Transaction) putGroupBytes(group, name string, data []byte) error {
	text := string(data)
	if tx.scope.ReadOnly {
		return kanbanError("board.transaction_invalid", "read-only")
	}
	p, err := tx.groupPath(group, name)
	if err != nil {
		return err
	}
	if err = tx.stageParents(control(tx.root, "groups"), filepath.Dir(p)); err != nil {
		return err
	}
	b, exists, err := fs.ReadRegularFileIfExists(tx.root, p)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(tx.root, p)
	for _, file := range tx.record.Files {
		if strings.EqualFold(file.Path, rel) {
			return kanbanError("board.transaction_invalid", "duplicate file")
		}
	}
	var before *string
	if exists {
		v := string(b)
		before = &v
	}
	tx.record.Files = append(tx.record.Files, FileChange{Path: rel, Before: before, After: text})
	return nil
}
