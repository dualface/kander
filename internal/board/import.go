package board

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/dualface/kander/internal/fs"
)

// MaxImportSlug bounds the readable middle part of a generated import task ID.
// Callers truncate longer slugs and keep a deterministic suffix; the board
// rejects a longer value instead of creating an unmanageable directory name.
const MaxImportSlug = 80

const (
	maxImportSourceKeyBytes = 512
	maxImportLanguageRunes  = 64
	maxImportTitleRunes     = 1024
)

// importFields lists every metadata field a section body must never rewrite.
var importFields = []string{
	FieldType, FieldSize, FieldTaskGroup, FieldLanguage, FieldCreatedAt,
	FieldOwner, FieldSession, FieldWindow, FieldStartedAt, FieldFinishedAt,
	FieldTaskBranch, FieldResult,
}

// importRecordMarkers are the record markers an imported contract must not
// carry: the review and prerequisite gates read them as completed human work.
var importRecordMarkers = []string{MarkerSelfReview, MarkerCardReview, MarkerPrerequisites}

// ImportFile is one card-relative attachment published together with an
// imported card. Names use forward slashes and stay inside the card directory.
type ImportFile struct {
	Name string
	Data []byte
}

// ImportContract carries the author-written bodies of the fixed card sections.
// The remote issue text never lands here: these bodies state the importing
// agent's scope, and the board rejects anything that could change the parsed
// document structure.
type ImportContract struct {
	Goal               string
	UserDecisions      string
	ExpectedOutcome    string
	AcceptanceCriteria string
	ThreatModel        string
	OutOfScope         string
	Discussion         string
}

// ImportRequest describes one atomic "create card plus source attachments".
type ImportRequest struct {
	// SourceKey is the canonical, caller-defined identity of the imported
	// source. It is opaque to the board and only feeds the deterministic
	// task-ID fallback when the readable ID is already taken.
	SourceKey string
	// Slug is the readable middle part of the generated task ID.
	Slug string
	// Title is the card title. Callers pass already sanitized text and the
	// board re-validates the single-line shape.
	Title string
	// Kind is one of TaskTypes().
	Kind string
	// Language is the LANGUAGE field value, already validated by the caller.
	Language string
	// Large selects the SIZE field only; imported cards are always directory
	// cards so a later large card can grow plan.md and report.md in place.
	Large bool
	// Contract holds the seven fixed section bodies.
	Contract ImportContract
	// Files are the source attachments. At least one is required so a failed
	// publication can never leave a card without its source.
	Files []ImportFile
	// Existing resolves the source key against the cards visible in the
	// transaction. The board calls it under the exclusive board lock before
	// any write; a match returns the existing card instead of a duplicate.
	Existing func(board Board) (Entry, bool, error)
}

// ImportResult reports the canonical card of one import.
type ImportResult struct {
	TaskID    string
	State     string
	Path      string
	Existing  bool
	SourceKey string
}

// TaskTypes returns the accepted TYPE tokens in canonical order.
func TaskTypes() []string {
	out := make([]string, 0, len(typeNames))
	for name := range typeNames {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// ReadCardFile reads one card-relative attachment from a scanned Entry. It does
// not take the board lock: callers either hold a consistent view already or
// accept the file as an operation-local read. A missing entry or file reports
// exists=false, and file cards never carry attachments.
func ReadCardFile(entry Entry, name string) ([]byte, bool, error) {
	if !entry.IsDirectory() {
		return nil, false, nil
	}
	p, err := documentPath(entry, name)
	if err != nil {
		return nil, false, err
	}
	return fs.ReadRegularFileIfExists(boardRootFromEntry(entry), p)
}

// ImportTask creates one backlog card with its source attachments in a single
// recoverable transaction, or returns the card already bound to the source key.
// Concurrent imports of the same source serialize on the exclusive board lock:
// exactly one card is published and every other call returns that card.
func ImportTask(root string, request ImportRequest) (ImportResult, error) {
	return importTask(nil, root, request)
}

// importTask takes an optional publication checkpoint so interruption tests can
// stop between the journal write and the commit.
func importTask(checkpoint func(string) error, root string, request ImportRequest) (result ImportResult, err error) {
	if err = validateImportRequest(request); err != nil {
		return result, err
	}
	prefix := todayPrefix()
	canonical := prefix + "-" + request.Slug + "-task"
	alternative := prefix + "-" + request.Slug + "-" + importHash(request.SourceKey) + "-task"
	scope := LockScope{Tasks: []string{alternative, canonical}, ExclusiveBoard: true}
	size := "small"
	if request.Large {
		size = "large"
	}
	err = withTransactionCheckpoint(nil, root, scope, checkpoint, func(tx *Transaction) error {
		view, err := scan(root)
		if err != nil {
			return err
		}
		if request.Existing != nil {
			entry, found, err := request.Existing(view)
			if err != nil {
				return err
			}
			if found {
				result = ImportResult{
					TaskID: entry.TaskID, State: entry.State, Path: entry.Path,
					Existing: true, SourceKey: request.SourceKey,
				}
				return nil
			}
		}
		id := importTaskID(canonical, alternative, view)
		if id == "" {
			return kanbanError("board.task_already_exists", canonical)
		}
		if err = tx.touch(id); err != nil {
			return err
		}
		target := filepath.Join(root, "backlog", id)
		entry := Entry{Path: target, Kind: "large"}
		relTarget, err := filepath.Rel(root, target)
		if err != nil {
			return err
		}
		// The card directory is staged before its attachments: files are written
		// before entries, and a card directory may not exist yet.
		if !containsID(tx.record.Directories, relTarget) {
			tx.record.Directories = append(tx.record.Directories, relTarget)
		}
		for _, file := range request.Files {
			p, err := documentPath(entry, file.Name)
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(root, p)
			if err != nil {
				return err
			}
			for _, staged := range tx.record.Files {
				if strings.EqualFold(staged.Path, rel) {
					return kanbanError("board.import_attachment_name_must_be_unique", file.Name)
				}
			}
			if err = tx.stageParents(target, filepath.Dir(p)); err != nil {
				return err
			}
			tx.record.Files = append(tx.record.Files, FileChange{Path: rel, After: string(file.Data)})
		}
		rel, err := filepath.Rel(root, target)
		if err != nil {
			return err
		}
		text := renderImportedContract(request, size)
		tx.record.Entries = append(tx.record.Entries, EntryChange{To: rel, Kind: "large", Text: text})
		result = ImportResult{TaskID: id, State: "backlog", Path: target, SourceKey: request.SourceKey}
		return nil
	})
	if err != nil {
		return ImportResult{}, err
	}
	return result, nil
}

// importTaskID prefers the readable ID and falls back to a stable hash of the
// source key, so two distinct sources with the same readable slug never collide.
func importTaskID(canonical, alternative string, view Board) string {
	if importIDFree(canonical, view) {
		return canonical
	}
	if importIDFree(alternative, view) {
		return alternative
	}
	return ""
}

func importIDFree(id string, view Board) bool {
	if _, taken := view.Entries[id]; taken {
		return false
	}
	_, blocked := view.Blocked[id]
	return !blocked
}

func importHash(sourceKey string) string {
	sum := sha256.Sum256([]byte(sourceKey))
	return hex.EncodeToString(sum[:4])
}

func validateImportRequest(request ImportRequest) error {
	if err := validateImportSourceKey(request.SourceKey); err != nil {
		return err
	}
	if !slugRe.MatchString(request.Slug) {
		return kanbanError("board.slug_may_contain_only_lowercase_ascii_letters_digits_and")
	}
	if len(request.Slug) > MaxImportSlug {
		return kanbanError("board.import_slug_is_too_long", request.Slug)
	}
	if err := validateImportTitle(request.Title); err != nil {
		return err
	}
	if _, ok := typeNames[request.Kind]; !ok {
		return kanbanError("board.unknown_task_type", request.Kind)
	}
	if err := validateImportLanguage(request.Language); err != nil {
		return err
	}
	if len(request.Files) == 0 {
		return kanbanError("board.import_requires_at_least_one_source_attachment")
	}
	for _, file := range request.Files {
		if !utf8.Valid(file.Data) {
			return kanbanError("board.import_attachment_is_not_valid_utf_8", file.Name)
		}
	}
	sections := []struct {
		name string
		body string
	}{
		{SectionGoal, request.Contract.Goal},
		{SectionUserDecisions, request.Contract.UserDecisions},
		{SectionExpectedOutcome, request.Contract.ExpectedOutcome},
		{SectionAcceptanceCriteria, request.Contract.AcceptanceCriteria},
		{SectionThreatModel, request.Contract.ThreatModel},
		{SectionOutOfScope, request.Contract.OutOfScope},
		{SectionDiscussion, request.Contract.Discussion},
	}
	for _, section := range sections {
		if err := validateImportSection(section.name, section.body); err != nil {
			return err
		}
	}
	if !checkboxRe.MatchString(request.Contract.AcceptanceCriteria) {
		return kanbanError("board.import_acceptance_criteria_requires_items")
	}
	return nil
}

func validateImportSourceKey(value string) error {
	if value == "" || len(value) > maxImportSourceKeyBytes || !utf8.ValidString(value) {
		return kanbanError("board.import_source_key_is_invalid")
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return kanbanError("board.import_source_key_is_invalid")
		}
	}
	return nil
}

func validateImportTitle(value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") || !utf8.ValidString(value) {
		return kanbanError("board.title_must_not_be_empty_or_contain_newlines")
	}
	if utf8.RuneCountInString(value) > maxImportTitleRunes {
		return kanbanError("board.import_title_is_too_long")
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return kanbanError("board.title_must_not_be_empty_or_contain_newlines")
		}
	}
	return nil
}

func validateImportLanguage(value string) error {
	if strings.TrimSpace(value) == "" || strings.ContainsAny(value, "\r\n") || !utf8.ValidString(value) {
		return kanbanError("board.import_language_is_invalid")
	}
	if utf8.RuneCountInString(value) > maxImportLanguageRunes {
		return kanbanError("board.import_language_is_invalid")
	}
	for _, char := range value {
		if char < 0x20 || char == 0x7f {
			return kanbanError("board.import_language_is_invalid")
		}
	}
	return nil
}

// validateImportSection rejects a body that could alter how the card is parsed:
// a new ## heading, a rewritten metadata field, or a record marker that a gate
// treats as completed human work.
func validateImportSection(name, body string) error {
	if strings.TrimSpace(body) == "" {
		return kanbanError("board.import_contract_section_must_not_be_empty", name)
	}
	if ContainsPlaceholder(body) {
		return kanbanError("board.import_contract_section_must_not_contain_placeholders", name)
	}
	if headingRe.MatchString(body) {
		return kanbanError("board.import_contract_section_must_not_contain_headings", name)
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "- "))
		for _, field := range importFields {
			for _, accepted := range AcceptedNames(field) {
				if strings.HasPrefix(value, accepted+":") {
					return kanbanError("board.import_contract_section_must_not_rewrite_metadata", name)
				}
			}
		}
		for _, marker := range importRecordMarkers {
			if HasMarkerPrefix(trimmed, marker) {
				return kanbanError("board.import_contract_section_must_not_carry_records", name)
			}
		}
	}
	return nil
}
