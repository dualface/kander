package server

import (
	"encoding/json"
	"net/http"

	"github.com/dualface/kander/v2/internal/board"
)

func (p *project) stageOperationReceipt(tx *board.Transaction, key, operation string, request, body any) error {
	return p.stageOperationReceiptStatus(tx, key, operation, request, http.StatusOK, body)
}

func (p *project) stageOperationReceiptStatus(tx *board.Transaction, key, operation string, request any, status int, body any) error {
	return p.stageReceiptAt(tx, keyReceipt(key), key, operation, request, status, body)
}

// stageReceiptAt stages the done receipt of one request at at.
func (p *project) stageReceiptAt(tx *board.Transaction, at receiptAt, key, operation string, request any, status int, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return stageJSON(tx, at.name, receipt{Schema: receiptSchema, Key: key, Operation: operation, Digest: requestDigest(operation, request), Status: receiptDone, HTTP: status, Body: data, CreatedAt: p.server.now()})
}
