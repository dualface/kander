package server

import (
	"errors"
	"net/http"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/issue"
)

// boardCodes maps board message keys to stable API codes. A board key not
// listed here is a domain rejection (CodeBoardRejected); the client renders
// it from BoardCode with the same localized text as the v1 command.
var boardCodes = map[string]string{
	"board.transaction_conflict":        api.CodeRevisionConflict,
	"board.transaction_pending":         api.CodeNotReady,
	"board.task_does_not_exist":         api.CodeNotFound,
	"board.invalid_task_id":             api.CodeInvalidRequest,
	"board.person_field_invalid":        api.CodeInvalidRequest,
	"board.request_description_invalid": api.CodeInvalidRequest,
	"board.decision_invalid":            api.CodeInvalidRequest,
	"board.decision_reserved":           api.CodeDecisionReserved,
	"board.confirmation_required":       api.CodeConfirmationRequired,
	"board.confirmer_required":          api.CodeInvalidRequest,
	"board.review_compacted":            api.CodeReviewCompacted,
	"board.review_run_compacted":        api.CodeReviewCompacted,
}

var codeStatus = map[string]int{
	api.CodeInvalidRequest:          http.StatusBadRequest,
	api.CodeForbidden:               http.StatusForbidden,
	api.CodeAttributionRequired:     http.StatusForbidden,
	api.CodeNotFound:                http.StatusNotFound,
	api.CodeRevisionConflict:        http.StatusConflict,
	api.CodeDecisionReserved:        http.StatusConflict,
	api.CodeConfirmationRequired:    http.StatusConflict,
	api.CodeHeldByOther:             http.StatusConflict,
	api.CodeTakenOver:               http.StatusConflict,
	api.CodeLeaseInvalid:            http.StatusConflict,
	api.CodeLeaseActive:             http.StatusConflict,
	api.CodeClaimRequired:           http.StatusConflict,
	api.CodeEvidenceIncomplete:      http.StatusUnprocessableEntity,
	api.CodeProtocolIncompatible:    http.StatusPreconditionFailed,
	api.CodeIdempotencyMismatch:     http.StatusUnprocessableEntity,
	api.CodeProjectMismatch:         http.StatusConflict,
	api.CodeProjectNotEmpty:         http.StatusConflict,
	api.CodeBoardRejected:           http.StatusUnprocessableEntity,
	api.CodeNotReady:                http.StatusServiceUnavailable,
	api.CodeIndexRebuilding:         http.StatusServiceUnavailable,
	api.CodeReviewCompacted:         http.StatusGone,
	api.CodeGCRunning:               http.StatusConflict,
	api.CodeImportCanceled:          http.StatusServiceUnavailable,
	api.CodePolicyRevisionConflict:  http.StatusConflict,
	api.CodePolicyRevisionStale:     http.StatusConflict,
	api.CodePolicyRequirementUnmet:  http.StatusBadRequest,
	api.CodeHostNameConflict:        http.StatusConflict,
	api.CodeRemoteRequired:          http.StatusUnprocessableEntity,
	api.CodeTaskNotTodo:             http.StatusConflict,
	api.CodeHostUnknown:             http.StatusNotFound,
	api.CodeHostOffline:             http.StatusConflict,
	api.CodeHostUnsupported:         http.StatusUnprocessableEntity,
	api.CodeHostProjectMissing:      http.StatusUnprocessableEntity,
	api.CodeHostAgentUnavailable:    http.StatusUnprocessableEntity,
	api.CodeHostLauncherUnavailable: http.StatusUnprocessableEntity,
	api.CodeStartOrderOpen:          http.StatusConflict,
	api.CodeStartOrderRefused:       http.StatusConflict,
	api.CodeInternal:                http.StatusInternalServerError,
}

func apiError(code, message string) *api.Error {
	return &api.Error{Code: code, Message: message, Status: codeStatus[code]}
}

// asAPIError classifies any failure. Unknown errors are internal; their text
// stays in the server log and the response carries a generic message.
func asAPIError(err error) *api.Error {
	var e *api.Error
	if errors.As(err, &e) {
		if e.Status == 0 {
			e.Status = codeStatus[e.Code]
		}
		if e.Status == 0 {
			e.Status = http.StatusInternalServerError
		}
		return e
	}
	var issueError *issue.Error
	if errors.As(err, &issueError) {
		return apiError(api.CodeInvalidRequest, issue.Message(issueError))
	}
	var b *board.Error
	if errors.As(err, &b) && b.Code != "" {
		code, ok := boardCodes[b.Code]
		if !ok {
			code = api.CodeBoardRejected
		}
		return &api.Error{Code: code, Message: b.Message, BoardCode: b.Code, Status: codeStatus[code]}
	}
	return &api.Error{Code: api.CodeInternal, Message: "internal server error", Status: http.StatusInternalServerError}
}
