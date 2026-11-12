package server

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/dualface/kander/internal/board"
)

func (p *project) stageOperationReceipt(tx *board.Transaction, key, operation string, request, body any) error {
	return p.stageOperationReceiptStatus(tx, key, operation, request, http.StatusOK, body)
}

func (p *project) stageOperationReceiptStatus(tx *board.Transaction, key, operation string, request any, status int, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return stageJSON(tx, filepath.Join("receipts", filepath.Base(receiptPath(key))), receipt{Schema: receiptSchema, Key: key, Operation: operation, Digest: requestDigest(operation, request), Status: receiptDone, HTTP: status, Body: data, CreatedAt: p.server.now()})
}
