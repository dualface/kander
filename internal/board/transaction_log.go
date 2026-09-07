package board

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/dualface/kander/internal/fs"
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
	return pendingContext(nil, root, ids)
}

func pendingContext(ctx context.Context, root string, ids []string) error {
	var records []OperationRecord
	var err error
	if ctx == nil {
		records, err = operationRecords(root)
	} else {
		var locks lockSet
		if err = locks.takeSharedContext(ctx, root, control(root, "locks", "journal.lock")); err != nil {
			return err
		}
		records, err = readOperationRecords(root)
		err = errors.Join(err, locks.close())
	}
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

// operationRecords holds the journal read lock through enumeration and the
// closing of all read handles. Windows readers must not overlap replacement.
func operationRecords(root string) (records []OperationRecord, err error) {
	err = withJournalLock(root, true, func() error {
		var readErr error
		records, readErr = readOperationRecords(root)
		return readErr
	})
	return records, err
}
func readOperationRecords(root string) ([]OperationRecord, error) {
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

// Journal locks are terminal locks: acquire after board/group/task locks and
// release before attempting any further board/group/task lock. No callback may
// reenter a board API. Writers hold the lock until atomic-write handles close.
func withJournalLock(root string, shared bool, fn func() error) (err error) {
	var locks lockSet
	if err = locks.take(root, control(root, "locks", "journal.lock"), shared); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, locks.close()) }()
	return fn()
}
func writeOperation(root, path string, record any, replace bool) error {
	return withJournalLock(root, false, func() error { return writeJSON(root, path, record, replace) })
}
