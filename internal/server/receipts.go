package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/dualface/kander/internal/api"
)

// receipt is the durable outcome of one idempotent write. A pending receipt
// is written before the domain operation runs; a crash leaves it pending, and
// the retry asks the operation whether its effect is already visible before
// running it again, so an applied write is never applied twice.
type receipt struct {
	Key       string          `json:"key"`
	Operation string          `json:"operation"`
	Digest    string          `json:"digest"`
	Status    string          `json:"status"`
	HTTP      int             `json:"http,omitzero"`
	Body      json.RawMessage `json:"body,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

const (
	receiptPending = "pending"
	receiptDone    = "done"
)

// outcome is what an operation answers: a status and a JSON body, or an error.
type outcome struct {
	status int
	body   any
	err    error
}

func receiptPath(key string) string {
	sum := sha256.Sum256([]byte(key))
	return servicePath("receipts", hex.EncodeToString(sum[:])+".json")
}

func requestDigest(operation string, request any) string {
	data, _ := json.Marshal(request)
	sum := sha256.Sum256(append([]byte(operation+"\n"), data...))
	return hex.EncodeToString(sum[:])
}

// idempotent runs one write under the project lock. applied inspects the
// current state for the effect of an interrupted earlier attempt; it returns
// ok=false when the effect is not visible and the operation may run.
func (p *project) idempotent(key, operation string, request any, run func() outcome, applied func() (outcome, bool)) outcome {
	if !api.ValidIdentity(key) {
		return outcome{err: &api.Error{Code: api.CodeInvalidRequest, Message: "Idempotency-Key header is required and must be an identity"}}
	}
	digest := requestDigest(operation, request)
	rel := receiptPath(key)
	var r receipt
	found, err := readJSON(p.root, rel, &r)
	if err != nil {
		return outcome{err: err}
	}
	if found {
		if r.Digest != digest || r.Operation != operation {
			return outcome{err: &api.Error{Code: api.CodeIdempotencyMismatch, Message: "idempotency key reused with a different request"}}
		}
		if r.Status == receiptDone {
			return outcome{status: r.HTTP, body: r.Body}
		}
		if out, ok := applied(); ok {
			return p.finish(rel, r, out)
		}
	} else {
		r = receipt{Key: key, Operation: operation, Digest: digest, Status: receiptPending, CreatedAt: time.Now().UTC()}
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
	found, err := readJSON(p.root, rel, &committed)
	if err != nil {
		return outcome{err: err}
	}
	if found && committed.Status == receiptDone {
		if committed.Operation != r.Operation || committed.Digest != r.Digest {
			return outcome{err: apiError(api.CodeIdempotencyMismatch, "receipt changed during its operation")}
		}
		return outcome{status: committed.HTTP, body: committed.Body}
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
