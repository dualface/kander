package server

import (
	"encoding/json"
	"net/http"

	"github.com/dualface/kander/v2/internal/board"
)

func (p *project) stageOperationReceipt(tx *board.Transaction, scope receiptScope, operation string, request, body any) error {
	return p.stageOperationReceiptStatus(tx, scope, operation, request, http.StatusOK, body)
}

// stageOperationReceiptStatus stages the done receipt of one request where
// idempotent looks for it: the scoped receipt of operation.
func (p *project) stageOperationReceiptStatus(tx *board.Transaction, scope receiptScope, operation string, request any, status int, body any) error {
	return p.stageReceiptAt(tx, scope.receipt(operation), scope, operation, request, status, body)
}

// stageReceiptAt stages the done receipt of one request at at.
func (p *project) stageReceiptAt(tx *board.Transaction, at receiptAt, scope receiptScope, operation string, request any, status int, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return stageJSON(tx, at.name, receipt{Schema: receiptSchema, Key: scope.key, Operation: operation, Digest: requestDigest(operation, request), Status: receiptDone, HTTP: status, Body: data, CreatedAt: p.server.now()})
}
