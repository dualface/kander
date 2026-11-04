package server

import (
	"errors"
	"net/http"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
)

// boardCodes maps board message keys to stable API codes. A board key not
// listed here is a domain rejection (CodeBoardRejected); the client renders
// it from BoardCode with the same localized text as the v1 command.
var boardCodes = map[string]string{
	"board.transaction_conflict": api.CodeRevisionConflict,
	"board.task_does_not_exist":  api.CodeNotFound,
	"board.invalid_task_id":      api.CodeInvalidRequest,
}

var codeStatus = map[string]int{
	api.CodeInvalidRequest:       http.StatusBadRequest,
	api.CodeNotFound:             http.StatusNotFound,
	api.CodeRevisionConflict:     http.StatusConflict,
	api.CodeHeldByOther:          http.StatusConflict,
	api.CodeLeaseInvalid:         http.StatusConflict,
	api.CodeLeaseActive:          http.StatusConflict,
	api.CodeClaimRequired:        http.StatusConflict,
	api.CodeEvidenceIncomplete:   http.StatusUnprocessableEntity,
	api.CodeProtocolIncompatible: http.StatusPreconditionFailed,
	api.CodeIdempotencyMismatch:  http.StatusUnprocessableEntity,
	api.CodeProjectMismatch:      http.StatusConflict,
	api.CodeProjectNotEmpty:      http.StatusConflict,
	api.CodeBoardRejected:        http.StatusUnprocessableEntity,
	api.CodeNotReady:             http.StatusServiceUnavailable,
	api.CodeInternal:             http.StatusInternalServerError,
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
