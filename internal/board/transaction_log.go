package board

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"github.com/dualface/kander/internal/fs"
	"strings"
)

// versionRecord persists the last commit identity and never unfreezes a contract.
type versionRecord struct {
	Revision       uint64 `json:"revision"`
	OperationID    string `json:"operation_id"`
	ContractFrozen bool   `json:"contract_frozen"`
}

func readVersion(root, id string) (v versionRecord, err error) {
	data, exists, err := fs.ReadRegularFileIfExists(root, control(root, "versions", id+".json"))
	if err != nil || !exists {
		return v, err
	}
	err = json.Unmarshal(data, &v)
	if err == nil && (v.Revision == 0 || v.OperationID == "") {
		err = kanbanError("board.transaction_invalid", id)
	}
	return v, err
}
func revision(root, id string) (uint64, error) {
	v, err := readVersion(root, id)
	return v.Revision, err
}
func operationID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func pending(root string, ids []string) error {
	records, err := operationRecords(root)
	if err != nil {
		return err
	}
	for _, r := range records {
		if r.Phase == "prepared" {
			for _, id := range ids {
				if _, ok := r.Revisions[id]; ok || containsID(r.Groups, id) {
					return kanbanError("board.transaction_pending", r.ID)
				}
			}
		}
	}
	return nil
}
func operationRecords(root string) ([]OperationRecord, error) {
	files, err := fs.ListDirectory(root, control(root, "operations"))
	if err != nil {
		return nil, err
	}
	var records []OperationRecord
	for _, f := range files {
		if strings.HasPrefix(f.Name, ".") {
			continue
		}
		if f.Kind != fs.KindFile || !strings.HasSuffix(f.Name, ".json") {
			return nil, kanbanError("board.transaction_invalid", f.Name)
		}
		data, err := fs.ReadRegularFile(root, control(root, "operations", f.Name))
		if err != nil {
			return nil, err
		}
		var r OperationRecord
		if err = json.Unmarshal(data, &r); err != nil {
			return nil, err
		}
		if r.Schema != 1 || r.ID+".json" != f.Name || (r.Phase != "prepared" && r.Phase != "committed") {
			return nil, kanbanError("board.transaction_invalid", f.Name)
		}
		records = append(records, r)
	}
	return records, nil
}

func writeJSON(root, path string, value any, replace bool) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return fs.WriteTextAtomic(root, path, string(b)+"\n", replace)
}
