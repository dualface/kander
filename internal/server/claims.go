package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strconv"
	"time"

	"github.com/dualface/kander/internal/api"
)

// claimRecord is the server-side ownership of one task. It lives outside the
// card, so a renewal never changes the card revision. Status is pending while
// a claim or takeover is between its record write and the board move; startup
// reconciliation settles a pending record from the card state.
type claimRecord struct {
	TaskID      string        `json:"task_id"`
	ClientID    string        `json:"client_id"`
	ExecutionID string        `json:"execution_id"`
	Owner       string        `json:"owner"`
	Generation  uint64        `json:"generation"`
	TokenHash   string        `json:"token_hash"`
	Status      string        `json:"status"`
	ExpiresAt   time.Time     `json:"expires_at"`
	RenewedAt   time.Time     `json:"renewed_at"`
	Handoffs    []api.Handoff `json:"handoffs,omitempty"`
	// OriginalText is the card before a launcher start claim, kept so a
	// failed start can return the card to todo.
	OriginalText string `json:"original_text,omitempty"`
}

const claimPending = "pending"

// holder is the claim context a write presents in its headers.
type holder struct {
	token      string
	generation uint64
	clientID   string
}

func claimPath(task string) string { return servicePath("claims", task+".json") }

func (p *project) readClaim(task string) (claimRecord, bool, error) {
	var r claimRecord
	found, err := readJSON(p.root, claimPath(task), &r)
	return r, found, err
}

func (p *project) writeClaim(r claimRecord) error { return writeJSON(p.root, claimPath(r.TaskID), r) }

// token derives the bearer secret of one execution from the server secret, so
// a replayed claim receipt can return it without the server storing it.
func (s *Server) token(project, task, execution string, generation uint64) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(project + "\x00" + task + "\x00" + execution + "\x00" + strconv.FormatUint(generation, 10)))
	return hex.EncodeToString(mac.Sum(nil))
}

func tokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// view renders the public claim with the lease status the server clock decides.
func (r claimRecord) view(now time.Time) *api.ClaimView {
	status := r.Status
	if status == api.ClaimActive && now.After(r.ExpiresAt) {
		status = api.ClaimLost
	}
	return &api.ClaimView{
		ClientID: r.ClientID, ExecutionID: r.ExecutionID, Owner: r.Owner,
		Generation: r.Generation, Status: status, ExpiresAt: r.ExpiresAt, RenewedAt: r.RenewedAt,
	}
}

// claimedState reports whether a board state keeps an owner.
func claimedState(state string) bool { return state == "working" || state == "review" }

// requireHolder checks that h is the live holder of a claimed task. Callers
// hold the project lock, so the check and the following board write are one
// atomic step with respect to every other server operation.
func (p *project) requireHolder(task string, h holder) (claimRecord, error) {
	r, found, err := p.readClaim(task)
	if err != nil {
		return r, err
	}
	if !found || r.Status != api.ClaimActive {
		return r, apiError(api.CodeLeaseInvalid, "task "+task+" has no active claim; an explicit takeover is required")
	}
	if h.token == "" || subtle.ConstantTimeCompare([]byte(tokenHash(h.token)), []byte(r.TokenHash)) != 1 {
		return r, apiError(api.CodeHeldByOther, "task "+task+" is claimed by another execution")
	}
	if h.generation != r.Generation {
		return r, apiError(api.CodeLeaseInvalid, "claim generation "+strconv.FormatUint(h.generation, 10)+" is not current")
	}
	if p.server.now().After(r.ExpiresAt) {
		return r, apiError(api.CodeLeaseInvalid, "the lease of task "+task+" expired; an explicit takeover is required")
	}
	return r, nil
}
