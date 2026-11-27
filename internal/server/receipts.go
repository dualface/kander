package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
)

// receipt is the durable outcome of one idempotent write. A pending receipt
// is written before the domain operation runs; a crash leaves it pending, and
// the retry asks the operation whether its effect is already visible before
// running it again, so an applied write is never applied twice.
type receipt struct {
	Schema    int             `json:"schema,omitzero"`
	Key       string          `json:"key"`
	Operation string          `json:"operation"`
	Digest    string          `json:"digest"`
	Status    string          `json:"status"`
	HTTP      int             `json:"http,omitzero"`
	Body      json.RawMessage `json:"body,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

const (
	receiptSchema  = 1
	receiptPending = "pending"
	receiptDone    = "done"
)

// outcome is what an operation answers: a status and a JSON body, or an error.
type outcome struct {
	status int
	body   any
	err    error
	// stream, when set, writes the whole response itself and returns the
	// status to log. Only the import route streams.
	stream func(http.ResponseWriter) int
}

func receiptPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return servicePath("receipts", hex.EncodeToString(sum[:])+".json")
}

// liveReceiptDir holds the receipt slots of the liveness operations, below
// the receipts directory.
const liveReceiptDir = "live"

// liveReceiptName is the one receipt slot of a liveness operation for the
// execution whose claim token the request presents, relative to the service
// group. The token is derived from the execution and its generation, so each
// execution keeps one slot however many observations it sends, and a request
// with another token never replaces the slot of the current holder. The slot
// holds the receipt of the latest key only: a retry of that key replays it,
// and an older key runs again, which the holder, generation, and lease checks
// of the operation keep from moving a claim back.
func liveReceiptName(operation, token string) string {
	sum := sha256.Sum256([]byte(operation + "\n" + tokenHash(token)))
	return filepath.Join("receipts", liveReceiptDir, hex.EncodeToString(sum[:])+".json")
}

// receiptAt is where idempotent keeps the receipt of one request.
type receiptAt struct {
	// name is the receipt file relative to the service group.
	name string
	// slot marks a liveness slot: a receipt of another key there belongs to
	// an earlier request, and the request runs as a new one.
	slot bool
}

func keyReceipt(key string) receiptAt {
	return receiptAt{name: filepath.Join("receipts", filepath.Base(receiptPath(key)))}
}

func liveReceipt(operation, token string) receiptAt {
	return receiptAt{name: liveReceiptName(operation, token), slot: true}
}

// rel is the receipt file below the project directory.
func (at receiptAt) rel() string { return servicePath(at.name) }

func requestDigest(operation string, request any) string {
	data, _ := json.Marshal(request)
	sum := sha256.Sum256(append([]byte(operation+"\n"), data...))
	return hex.EncodeToString(sum[:])
}

// idempotent runs one write under the project lock. applied inspects the
// current state for the effect of an interrupted earlier attempt; it returns
// ok=false when the effect is not visible and the operation may run.
func (p *project) idempotent(key, operation string, request any, run func() outcome, applied func() (outcome, bool)) outcome {
	return p.idempotentAt(keyReceipt(key), key, operation, request, nil, run, applied)
}

// idempotentChecked is idempotent with a check that runs only when the
// operation is about to run: after the receipt lookup, before a new pending
// receipt is written or a pending one is run again. A replayed done receipt
// and an applied pending one skip it. A refused check answers its error and
// writes no receipt, so the caller may send the same key again once it fixed
// what the check refused.
func (p *project) idempotentChecked(key, operation string, request any, check func() error, run func() outcome, applied func() (outcome, bool)) outcome {
	return p.idempotentAt(keyReceipt(key), key, operation, request, check, run, applied)
}

// idempotentAt is idempotent with the receipt kept at at and an optional check
// (see idempotentChecked).
func (p *project) idempotentAt(at receiptAt, key, operation string, request any, check func() error, run func() outcome, applied func() (outcome, bool)) outcome {
	if !api.ValidIdentity(key) {
		return outcome{err: &api.Error{Code: api.CodeInvalidRequest, Message: "Idempotency-Key header is required and must be an identity"}}
	}
	digest := requestDigest(operation, request)
	rel := at.rel()
	var r receipt
	found, err := readJSON(p.root, rel, &r)
	if err != nil {
		return outcome{err: err}
	}
	if found && at.slot && r.Key != key {
		found = false
	}
	if !found && at.slot {
		// A receipt a server before the slots wrote for this key still answers
		// it. It is only read: neither moved nor removed, and never written over
		// the slot.
		var legacy receipt
		ok, err := readJSON(p.root, keyReceipt(key).rel(), &legacy)
		if err != nil {
			return outcome{err: err}
		}
		if ok && legacy.Status == receiptDone && legacy.Key == key && legacy.Operation == operation && legacy.Digest == digest {
			return p.replayReceipt(legacy)
		}
	}
	if found {
		if r.Digest != digest || r.Operation != operation {
			return outcome{err: &api.Error{Code: api.CodeIdempotencyMismatch, Message: "idempotency key reused with a different request"}}
		}
		if r.Status == receiptDone {
			return p.replayReceipt(r)
		}
		// Earlier creation intents did not bind a result to the card redo.
		// Their mutable board state cannot prove whether creation committed.
		// Preserve that uncertain intent for inspection instead of guessing.
		if operation == "new" && r.Schema != receiptSchema {
			return outcome{err: apiError(api.CodeEvidenceIncomplete, "legacy pending creation has no atomic result; inspect the original operation before retrying")}
		}
		if out, ok := applied(); ok {
			return p.finish(rel, r, out)
		}
		if check != nil {
			if err := check(); err != nil {
				return outcome{err: err}
			}
		}
	} else {
		if check != nil {
			if err := check(); err != nil {
				return outcome{err: err}
			}
		}
		if at.slot {
			// The slot may hold the receipt an earlier request staged in a board
			// transaction that waits for recovery, which needs the slot as that
			// transaction left it. Such a transaction names the service group;
			// the board lock taken by the caller keeps a new one from starting.
			if err := board.RequireNoPendingFor(p.board, serviceGroup); err != nil {
				return outcome{err: err}
			}
		}
		r = receipt{Schema: receiptSchema, Key: key, Operation: operation, Digest: digest, Status: receiptPending, CreatedAt: time.Now().UTC()}
		if err := writeJSON(p.root, rel, r); err != nil {
			return outcome{err: err}
		}
	}
	return p.finish(rel, r, run())
}

// finish records a final outcome. Internal failures stay pending, so a retry
// re-inspects instead of replaying a stored internal error forever.
func (p *project) finish(rel string, r receipt, out outcome) outcome {
	// Domain producers may already have committed their exact response with
	// the mutation. Never replace it with a later observation after unlock.
	var committed receipt
	found, err := p.readReceiptAfterOperation(rel, &committed)
	if err != nil {
		return outcome{err: err}
	}
	if found && committed.Status == receiptDone {
		if committed.Operation != r.Operation || committed.Digest != r.Digest {
			return outcome{err: apiError(api.CodeIdempotencyMismatch, "receipt changed during its operation")}
		}
		return p.replayReceipt(committed)
	}

	status, body := out.status, out.body
	if out.err != nil {
		e := asAPIError(out.err)
		if e.Code == api.CodeInternal || e.Code == api.CodeNotReady {
			return out
		}
		status, body = e.Status, e
	}
	data, err := json.Marshal(body)
	if err != nil {
		return outcome{err: err}
	}
	r.Status, r.HTTP, r.Body = receiptDone, status, data
	if err := writeJSON(p.root, rel, r); err != nil {
		return outcome{err: err}
	}
	return outcome{status: status, body: json.RawMessage(data)}
}

func (p *project) readReceiptAfterOperation(rel string, target *receipt) (bool, error) {
	if p.receiptReadFault != nil {
		if err := p.receiptReadFault(rel); err != nil {
			return false, err
		}
	}
	return readJSON(p.root, rel, target)
}

func (p *project) replayReceipt(r receipt) outcome {
	if id, confirm := strings.CutPrefix(r.Operation, confirmOperation); confirm && r.HTTP == http.StatusOK {
		if err := p.requirePublishedConfirmation(id, r.Body); err != nil {
			return outcome{err: err}
		}
	}
	return outcome{status: r.HTTP, body: r.Body}
}
