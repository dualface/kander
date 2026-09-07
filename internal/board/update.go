package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

func baseEntryName(e Entry) string { return filepath.Base(e.Path) }
func setMetadata(text, field, value string) (string, error) {
	if strings.ContainsAny(value, "\n\r") {
		return "", kanbanError("board.transaction_invalid", field)
	}
	re := FieldLineRe(field)
	matches := re.FindAllStringIndex(text, -1)
	if len(matches) > 1 {
		return "", kanbanError("board.transaction_invalid", field)
	}
	line := RenderField(field, value)
	if len(matches) == 1 {
		return re.ReplaceAllStringFunc(text, func(string) string { return line }), nil
	}
	at := strings.Index(text, "\n## ")
	if at < 0 {
		at = len(text)
	}
	return text[:at] + "\n" + line + "\n" + text[at:], nil
}
func moveMetadata(text, from, to string, o MoveOptions) (string, error) {
	var err error
	if o.Owner != "" {
		if to != "working" {
			return "", kanbanError("board.transaction_invalid", "--owner")
		}
		text, err = setMetadata(text, FieldOwner, o.Owner)
		if err != nil {
			return "", err
		}
		text, err = setMetadata(text, FieldStartedAt, nowStamp())
		if err != nil {
			return "", err
		}
	}
	if o.Result != "" {
		valid := to == "done" && o.Result == "completed" || to == "trash" && o.Result == "trashed" || to == "archived" && (from == "done" && o.Result == "completed" || from != "done" && (o.Result == "cancelled" || o.Result == "duplicate" || o.Result == "wontfix"))
		if !valid {
			return "", kanbanError("board.transaction_invalid", "--result")
		}
		text, err = setMetadata(text, FieldResult, o.Result)
		if err != nil {
			return "", err
		}
	}
	if to == "trash" || to == "archived" {
		if strings.TrimSpace(o.Reason) == "" || strings.TrimSpace(o.Decision) == "" {
			return "", kanbanError("board.transaction_decision_required")
		}
		if to == "archived" && from != "done" && MetadataFrom(text, FieldResult) == "completed" {
			return "", kanbanError("board.transaction_invalid", "completed")
		}
		if MetadataFrom(text, FieldResult) == "duplicate" {
			if _, err = NormalizeTaskID(o.DuplicateOf); err != nil {
				return "", err
			}
		}
		record := map[string]string{"at": nowStamp(), "reason": o.Reason, "decision_reference": o.Decision, "duplicate_of": o.DuplicateOf}
		b, _ := json.Marshal(record)
		text = appendRecordSection(text, "LIFECYCLE_DECISION", string(b))
	} else if o.Reason != "" || o.Decision != "" || o.DuplicateOf != "" {
		return "", kanbanError("board.transaction_invalid", "termination options")
	}
	return text, nil
}

// UpdateOptions constrains an agent-authored replacement to one expected revision.
type UpdateOptions struct {
	Document         string
	Text             string
	ExpectedRevision uint64
	ContractDecision string
	Authorization    ExecutionAuthorization
}

var managedFields = []string{FieldOwner, FieldSession, FieldWindow, FieldStartedAt, FieldFinishedAt, FieldResult, FieldLanguage, FieldCreatedAt, "REVISION", "OPERATION_ID", "EXECUTION_EPOCH", "DISPATCH_ID", "REVIEWS"}
var frozenSections = []string{SectionGoal, SectionUserDecisions, SectionExpectedOutcome, SectionAcceptanceCriteria, SectionOutOfScope}

func frozenContract(text string) map[string]string {
	fields := map[string]string{"TASK_GROUP": TaskGroupFrom(text), "SIZE": MetadataFrom(text, "SIZE")}
	for _, h := range frozenSections {
		body, ok := SectionBody(text, h)
		if ok {
			fields[h] = body
		}
	}
	// Keep dependency records frozen even when the rest of DISCUSSION is edited.
	discussion, _ := SectionBody(text, SectionDiscussion)
	for _, line := range strings.Split(discussion, "\n") {
		if HasMarkerPrefix(line, MarkerPrerequisites) {
			fields["PREREQUISITES"] = line
		}
	}
	return fields
}
func fieldLines(text, name string) []string { return FieldLineRe(name).FindAllString(text, -1) }
func validSpecUpdate(old, next, state, decision string) (string, error) {
	// Duplicate headings and metadata create ambiguous contracts across readers.
	for _, field := range append(append([]string(nil), managedFields...), FieldTaskGroup, "SIZE", FieldType, FieldTaskBranch) {
		if len(fieldLines(next, field)) > 1 {
			return "", kanbanError("board.transaction_invalid", field)
		}
	}
	for _, section := range append(append([]string(nil), frozenSections...), SectionDiscussion, "REVIEWS", "DISPATCHES", "CONTRACT_DECISIONS", "LIFECYCLE_DECISION") {
		re := regexp.MustCompile(`(?m)^## ` + TokenPattern(section) + `[ \t\r]*$`)
		if len(re.FindAllStringIndex(next, -1)) > 1 {
			return "", kanbanError("board.transaction_invalid", section)
		}
	}
	if _, err := prerequisiteIDsFrom(next, "update"); err != nil {
		return "", err
	}

	for _, field := range managedFields {
		if !reflect.DeepEqual(fieldLines(old, field), fieldLines(next, field)) {
			return "", kanbanError("board.transaction_managed", field)
		}
	}
	for _, section := range []string{"REVIEWS", "DISPATCHES", "REVIEW_DISPOSITIONS", "CONTRACT_DECISIONS", "LIFECYCLE_DECISION"} {
		a, ok := SectionBody(old, section)
		b, present := SectionBody(next, section)
		if ok != present || a != b {
			return "", kanbanError("board.transaction_managed", section)
		}
	}
	before, after := frozenContract(old), frozenContract(next)
	if state != "backlog" && !reflect.DeepEqual(before, after) {
		if strings.TrimSpace(decision) == "" {
			return "", kanbanError("board.transaction_decision_required")
		}
		record := struct {
			At            string `json:"at"`
			Decision      string `json:"decision"`
			Before, After map[string]string
		}{nowStamp(), decision, before, after}
		b, _ := json.MarshalIndent(record, "", "  ")
		// Store the authoritative authorization and diff in the operation's staged
		// body; this section is protected on subsequent agent updates.
		next = appendRecordSection(next, "CONTRACT_DECISIONS", "```json\n"+string(b)+"\n```")
	}
	return next, nil
}

// Producer-owned paths are excluded from ordinary updates and migration edits.
func managedDocumentPart(part string) bool {
	switch strings.ToLower(part) {
	case "reviews", "dispatches", "manifest.json", "checkpoint.json", "index.json":
		return true
	}
	return false
}

// UpdateDocument accepts only ordinary documents; lifecycle fields and producer
// records require their dedicated APIs, including on backlog cards.
func UpdateDocument(root, id string, o UpdateOptions) error {
	id, err := NormalizeTaskID(id)
	if err != nil {
		return err
	}
	if !utf8.ValidString(o.Text) || !utf8.ValidString(o.ContractDecision) {
		return kanbanError("board.task_document_is_not_valid_utf_8", o.Document)
	}
	return WithTransaction(root, LockScope{Tasks: []string{id}}, func(tx *Transaction) error {
		s, err := tx.Expect(id, "", o.ExpectedRevision)
		if err != nil {
			return err
		}
		if err = tx.requireExecution(s, o.Authorization, true); err != nil {
			return err
		}
		if err = tx.validateWrapUpUpdate(s, o); err != nil {
			return err
		}
		if _, err = documentPath(s.Entry, o.Document); err != nil {
			return err
		}
		for _, part := range strings.Split(strings.ToLower(o.Document), "/") {
			if managedDocumentPart(part) {
				return kanbanError("board.transaction_managed", o.Document)
			}
		}
		text := o.Text
		if o.Document == "spec.md" {
			state := s.Entry.State
			version, err := readVersion(root, id)
			if err != nil {
				return err
			}
			if version.ContractFrozen && state == "backlog" {
				state = "frozen"
			}
			if len(fieldLines(s.Text, FieldSize)) > 0 && len(fieldLines(text, FieldSize)) == 0 {
				return kanbanError("board.size_invalid", id)
			}
			text, err = validSpecUpdate(s.Text, text, state, o.ContractDecision)
			if err != nil {
				return err
			}
		} else if o.ContractDecision != "" {
			return kanbanError("board.transaction_invalid", "--contract-decision-file")
		}
		return tx.Put(id, o.Document, text)
	})
}

// RunUpdate implements the versioned UTF-8 body and attachment update entrance.
func RunUpdate(args []string) int {
	values := map[string]string{}
	for _, name := range []string{"--document", "--file", "--expect-revision", "--contract-decision-file", "--dispatch-id", "--execution-epoch"} {
		var err error
		args, values[name], _, err = takeValueFlag(args, name)
		if err != nil {
			return usageFail("update", "board.option_requires_a_value", name)
		}
	}
	if len(args) != 1 || values["--document"] == "" || values["--file"] == "" || values["--expect-revision"] == "" {
		return usageFail("update", "board.task_id_required")
	}
	rev, err := strconv.ParseUint(values["--expect-revision"], 10, 64)
	if err != nil {
		return fail(kanbanError("board.transaction_invalid", "revision"))
	}
	data, err := os.ReadFile(values["--file"])
	if err != nil {
		return fail(err)
	}
	decision := ""
	if path := values["--contract-decision-file"]; path != "" {
		b, e := os.ReadFile(path)
		if e != nil {
			return fail(e)
		}
		decision = string(b)
		if strings.TrimSpace(decision) == "" {
			return fail(kanbanError("board.transaction_decision_required"))
		}
	}
	root, err := requireRoot()
	if err != nil {
		return fail(err)
	}
	authorization, err := parseExecutionAuthorization(values)
	if err != nil {
		return fail(err)
	}
	if err = UpdateDocument(root, args[0], UpdateOptions{Document: values["--document"], Text: string(data), ExpectedRevision: rev, ContractDecision: decision, Authorization: authorization}); err != nil {
		return fail(err)
	}
	return 0
}

func appendRecordSection(text, heading, record string) string {
	re := regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta(heading) + `[ \t\r]*$`)
	at := re.FindStringIndex(text)
	if at == nil {
		return strings.TrimRight(text, "\n") + "\n\n## " + heading + "\n\n" + record + "\n"
	}
	end := len(text)
	if next := headingRe.FindStringIndex(text[at[1]:]); next != nil {
		end = at[1] + next[0]
	}
	return strings.TrimRight(text[:end], "\n") + "\n\n" + record + "\n\n" + text[end:]
}
