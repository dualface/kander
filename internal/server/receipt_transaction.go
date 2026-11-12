package server

import (
	"encoding/json"
	"github.com/dualface/kander/internal/board"
	"net/http"
	"path/filepath"
)

func (p *project) stageOperationReceipt(tx *board.Transaction, key, operation string, request, body any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return stageJSON(tx, filepath.Join("receipts", filepath.Base(receiptPath(key))), receipt{Key: key, Operation: operation, Digest: requestDigest(operation, request), Status: receiptDone, HTTP: http.StatusOK, Body: data, CreatedAt: p.server.now()})
}
