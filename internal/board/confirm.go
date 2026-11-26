package board

import (
	"encoding/json"
	"regexp"
	"strings"
	"time"
)

var submitterQuestionsRe = regexp.MustCompile(`(?m)^(?:- )?` + MarkerSubmitterQuestions + `[ \t]*:[ \t]*\S`)

// ConfirmInput binds the confirmation to the revision the submitter read.
type ConfirmInput struct {
	ExpectedRevision uint64
	ConfirmedBy      string
	At               time.Time
}

type confirmationRecord struct {
	At       time.Time `json:"at"`
	By       string    `json:"by"`
	Revision uint64    `json:"revision"`
}

// ConfirmRequest commits the confirmation and the move to todo together.
func ConfirmRequest(root, id string, input ConfirmInput, extensions ...TransactionExtension) error {
	s, err := ReadSnapshot(root, id)
	if err != nil {
		return err
	}
	_, err = moveWithOptions(s.Entry, root, "todo", MoveOptions{
		ExpectedRevision: &input.ExpectedRevision, confirmation: &input,
	}, extensions...)
	return err
}

func checkConfirmation(s Snapshot) error {
	if !IsRequestCard(s.Text) {
		return kanbanError("board.confirm_requires_request_card")
	}
	if s.Entry.State != "backlog" {
		return kanbanError("board.confirm_requires_backlog")
	}
	discussion, _ := rawSectionBody(s.Text, SectionDiscussion)
	if submitterQuestionsRe.MatchString(discussion) {
		return kanbanError("board.request_questions_open")
	}
	return nil
}

func appendConfirmation(text string, input ConfirmInput) (string, error) {
	discussion, _ := SectionBody(text, SectionDiscussion)
	if !cardReviewRe.MatchString(discussion) {
		return "", kanbanError("board.task_requires_card_review_record")
	}
	actor, err := personField("confirmed_by", input.ConfirmedBy)
	if err != nil {
		return "", err
	}
	if actor == "" {
		return "", kanbanError("board.confirmer_required")
	}
	record, err := json.Marshal(confirmationRecord{At: input.At.UTC(), By: actor, Revision: input.ExpectedRevision})
	if err != nil {
		return "", err
	}
	return appendRecordSection(lfLineEndings(text), SectionConfirmations, string(record)), nil
}

// LastConfirmer reads the actor from the most recent recorded confirmation.
func LastConfirmer(text string) (string, bool) {
	body, _ := SectionBody(text, SectionConfirmations)
	var record confirmationRecord
	if err := json.Unmarshal([]byte(body[strings.LastIndexByte(body, '\n')+1:]), &record); err != nil || record.By == "" {
		return "", false
	}
	return record.By, true
}
