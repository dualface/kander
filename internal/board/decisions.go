package board

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"time"
	"unicode/utf8"
)

type DecisionMode string

const (
	DecisionModeLeader   DecisionMode = "leader"
	DecisionModeAssignee DecisionMode = "assignee"
)

func (m DecisionMode) Valid() bool { return m == DecisionModeLeader || m == DecisionModeAssignee }

// DecisionModeOf reads the managed header. Older cards without it delegate.
func DecisionModeOf(text string) DecisionMode {
	if len(fieldLines(text, FieldDecisionMode)) == 0 {
		return DecisionModeLeader
	}
	return DecisionMode(MetadataFrom(text, FieldDecisionMode))
}

type DecisionKind string

const (
	DecisionHuman  DecisionKind = "human"
	DecisionLeader DecisionKind = "leader"
)

func (k DecisionKind) Valid() bool { return k == DecisionHuman || k == DecisionLeader }

type DecisionOption struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

type DecisionAuthorization struct {
	GrantedBy string `json:"granted_by"`
	Wording   string `json:"wording"`
	Date      string `json:"date"`
}

// DecisionInput carries the caller-provided material. The service replaces
// RecordedBy with the host attribution when a hook is configured.
type DecisionInput struct {
	Question      string                 `json:"question"`
	Options       []DecisionOption       `json:"options"`
	Answer        string                 `json:"answer"`
	RecordedBy    string                 `json:"recorded_by"`
	Channel       string                 `json:"channel"`
	Authorization *DecisionAuthorization `json:"authorization,omitempty"`
}

type DecisionRecord struct {
	DecisionInput
	Schema    int          `json:"schema"`
	ID        string       `json:"id"`
	TaskID    string       `json:"task_id"`
	CreatedAt time.Time    `json:"created_at"`
	Kind      DecisionKind `json:"kind"`
}

type decisionIndexEntry struct {
	ID         string       `json:"id"`
	Kind       DecisionKind `json:"kind"`
	CreatedAt  time.Time    `json:"created_at"`
	RecordedBy string       `json:"recorded_by"`
	Path       string       `json:"path"`
}

func decisionPath(id string) string { return "decisions/" + id + ".json" }

func validateDecision(kind DecisionKind, in DecisionInput) error {
	if !kind.Valid() {
		return kanbanError("board.decision_invalid", "kind")
	}
	switch in.Channel {
	case "web", "cli", "pane", "notify":
	default:
		return kanbanError("board.decision_invalid", "channel")
	}
	for _, field := range []struct {
		name, value string
		limit       int
	}{
		{"question", in.Question, 4096}, {"answer", in.Answer, 4096},
	} {
		if !decisionText(field.value, field.limit) {
			return kanbanError("board.decision_invalid", field.name)
		}
	}
	if in.Options == nil || len(in.Options) > 32 {
		return kanbanError("board.decision_invalid", "options")
	}
	keys := make(map[string]bool, len(in.Options))
	for _, option := range in.Options {
		if !decisionText(option.Key, 128) || !decisionText(option.Label, 512) || keys[option.Key] {
			return kanbanError("board.decision_invalid", "options")
		}
		keys[option.Key] = true
	}
	if kind == DecisionHuman {
		if in.Authorization != nil {
			return kanbanError("board.decision_invalid", "authorization")
		}
		return nil
	}
	a := in.Authorization
	if a == nil {
		return kanbanError("board.decision_invalid", "authorization")
	}
	if v, err := personField("authorization.granted_by", a.GrantedBy); err != nil || v == "" {
		return kanbanError("board.decision_invalid", "authorization.granted_by")
	}
	if !decisionText(a.Wording, 8192) {
		return kanbanError("board.decision_invalid", "authorization.wording")
	}
	if date, err := time.Parse(time.DateOnly, a.Date); err != nil || date.Format(time.DateOnly) != a.Date {
		return kanbanError("board.decision_invalid", "authorization.date")
	}
	return nil
}

func decisionText(value string, limit int) bool {
	return len(value) <= limit && utf8.ValidString(value) && !strings.ContainsRune(value, 0) && strings.TrimSpace(value) != ""
}

func decisionWritable(s Snapshot) error {
	if err := requireNotArchived(s); err != nil {
		return err
	}
	if s.Entry.State == "trash" {
		return kanbanError("board.decision_trash", s.Entry.TaskID)
	}
	return nil
}

// SetDecisionMode stages only the managed policy field, independently of the
// task contract and execution claim.
func (tx *Transaction) SetDecisionMode(id string, mode DecisionMode) error {
	if !mode.Valid() {
		return kanbanError("board.decision_invalid", "mode")
	}
	s, err := tx.Snapshot(id)
	if err != nil {
		return err
	}
	if err := decisionWritable(s); err != nil {
		return err
	}
	text, err := setMetadata(s.Text, FieldDecisionMode, string(mode))
	if err != nil {
		return err
	}
	return tx.Put(id, "spec.md", text)
}

// RecordDecision stages one immutable decision and its index entry in the same
// write transaction. The mode check uses that transaction's card snapshot.
func (tx *Transaction) RecordDecision(id string, kind DecisionKind, in DecisionInput, now time.Time) (DecisionRecord, error) {
	var zero DecisionRecord
	s, err := tx.Snapshot(id)
	if err != nil {
		return zero, err
	}
	if err := decisionWritable(s); err != nil {
		return zero, err
	}
	if kind == DecisionLeader && (DecisionModeOf(s.Text) != DecisionModeLeader || len(fieldLines(s.Text, FieldDecisionMode)) > 1) {
		return zero, kanbanError("board.decision_reserved")
	}
	if err := validateDecision(kind, in); err != nil {
		return zero, err
	}
	// Validate the selected identity without rewriting the attribution.
	if value, err := personField("recorded_by", in.RecordedBy); err != nil || value == "" {
		return zero, kanbanError("board.decision_invalid", "recorded_by")
	}
	recordID, err := operationID()
	if err != nil {
		return zero, err
	}
	record := DecisionRecord{Schema: 1, ID: recordID, TaskID: id, Kind: kind, CreatedAt: now.UTC(), DecisionInput: in}
	path := decisionPath(record.ID)
	if _, err := tx.Read(id, path); !errors.Is(err, os.ErrNotExist) {
		if err == nil {
			err = kanbanError("board.decision_corrupt", id)
		}
		return zero, err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return zero, err
	}
	index, err := json.Marshal(decisionIndexEntry{record.ID, record.Kind, record.CreatedAt, record.RecordedBy, path})
	if err != nil {
		return zero, err
	}
	if err := tx.Put(id, path, string(data)+"\n"); err != nil {
		return zero, err
	}
	text := appendRecordSection(lfLineEndings(s.Text), SectionDecisions, string(index))
	if strings.Contains(s.Text, "\r\n") {
		text = strings.ReplaceAll(text, "\n", "\r\n")
	}
	if err := tx.Put(id, "spec.md", text); err != nil {
		return zero, err
	}
	return record, nil
}

// Decisions reads the canonical records in publication order, including after
// archive. The index and each attachment must come from the same snapshot.
func Decisions(root, id string) ([]DecisionRecord, error) {
	records := []DecisionRecord{}
	err := WithTransaction(root, LockScope{Tasks: []string{id}, ReadOnly: true}, func(tx *Transaction) error {
		s, err := tx.Snapshot(id)
		if err != nil {
			return err
		}
		section, _ := SectionBody(s.Text, SectionDecisions)
		seen := map[string]bool{}
		for line := range strings.SplitSeq(section, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			var item decisionIndexEntry
			if json.Unmarshal([]byte(line), &item) != nil || !validDecisionID(item.ID) || item.Path != decisionPath(item.ID) || seen[item.ID] {
				return kanbanError("board.decision_corrupt", id)
			}
			seen[item.ID] = true
			text, err := tx.Read(id, item.Path)
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return kanbanError("board.decision_corrupt", id)
				}
				return err
			}
			var record DecisionRecord
			if json.Unmarshal([]byte(text), &record) != nil || record.Schema != 1 || record.ID != item.ID ||
				record.TaskID != id || record.Kind != item.Kind || !record.CreatedAt.Equal(item.CreatedAt) ||
				record.RecordedBy != item.RecordedBy {
				return kanbanError("board.decision_corrupt", id)
			}
			records = append(records, record)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return records, nil
}

func validDecisionID(id string) bool {
	return len(id) == 32 && !strings.ContainsFunc(id, func(r rune) bool {
		return !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f')
	})
}
