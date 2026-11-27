package server

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"strconv"
	"time"

	"github.com/dualface/kander/v2/api"
	"github.com/dualface/kander/v2/internal/board"
	"github.com/dualface/kander/v2/internal/config"
	"github.com/dualface/kander/v2/internal/fs"
)

// The project policy lives beside the board, in the control directory of the
// project, so a board import or replacement never touches it:
// control/policy.json holds the current policy and control/policy-audit.jsonl
// one line per write. A write appends its audit line first and then replaces
// policy.json, so a crash between the two leaves an audit line that never
// took effect; startup recovery appends a void line for it and never
// rewrites an earlier line. See docs/project-policy.md.
var (
	policyFile      = filepath.Join("control", "policy.json")
	policyAuditFile = filepath.Join("control", "policy-audit.jsonl")
)

const (
	policyOperation  = "project-policy"
	policyVoidReason = "not_applied"
)

// policyAuditWrite is the audit line of one physical write attempt. Every field
// is written, an empty submitter and the null old values of a first write
// included. Attempt is a random ID of the attempt: a retry with the same
// idempotency key is another attempt, so a void line cancels exactly one.
type policyAuditWrite struct {
	Attempt        string          `json:"attempt"`
	Epoch          string          `json:"epoch"`
	Revision       uint64          `json:"revision"`
	At             time.Time       `json:"at"`
	UpdatedBy      string          `json:"updated_by"`
	ClientID       string          `json:"client_id"`
	IdempotencyKey string          `json:"idempotency_key"`
	OldValues      json.RawMessage `json:"old_values"`
	NewValues      json.RawMessage `json:"new_values"`
}

// policyAuditVoid is the audit line that cancels one attempt that never took
// effect.
type policyAuditVoid struct {
	At     time.Time  `json:"at"`
	Void   policyVoid `json:"void"`
	Reason string     `json:"reason"`
}

// policyVoid names the attempt a void line cancels.
type policyVoid struct {
	Attempt        string `json:"attempt"`
	Epoch          string `json:"epoch"`
	Revision       uint64 `json:"revision"`
	IdempotencyKey string `json:"idempotency_key"`
}

// policyAuditRecord reads either kind of audit line.
type policyAuditRecord struct {
	policyAuditWrite
	Void   *policyVoid `json:"void,omitempty"`
	Reason string      `json:"reason,omitempty"`
}

func (r policyAuditWrite) policy() api.ProjectPolicy {
	return api.ProjectPolicy{Epoch: r.Epoch, Revision: r.Revision, Values: r.NewValues, UpdatedAt: r.At, UpdatedBy: r.UpdatedBy}
}

func (r policyAuditWrite) void(at time.Time) policyAuditVoid {
	return policyAuditVoid{At: at, Void: policyVoid{Attempt: r.Attempt, Epoch: r.Epoch, Revision: r.Revision, IdempotencyKey: r.IdempotencyKey}, Reason: policyVoidReason}
}

// appliedTo reports whether the write of r is part of current: the same epoch
// and a revision current has reached.
func (r policyAuditWrite) appliedTo(current api.ProjectPolicy) bool {
	return current.Revision > 0 && r.Epoch == current.Epoch && r.Revision <= current.Revision
}

// policyView is the current policy of the project; revision 0 when it has
// none. It needs no lock: writes publish a new value atomically.
func (p *project) policyView() api.ProjectPolicy {
	if current := p.policy.Load(); current != nil {
		return *current
	}
	return api.ProjectPolicy{}
}

// view is the project as the project routes answer it: its registry entry
// with the current policy.
func (p *project) view() api.Project {
	info := p.info
	policy := p.policyView()
	info.Policy = &policy
	return info
}

// openPolicy loads the policy and voids the audit lines a crash left without
// effect. Offline maintenance never calls it (project.open), so a damaged
// policy file never blocks a board maintenance.
func (p *project) openPolicy() error {
	current := api.ProjectPolicy{}
	found, err := readJSON(p.root, policyFile, &current)
	if err != nil {
		return err
	}
	if found && (current.Revision == 0 || current.Epoch == "" || len(current.Values) == 0) {
		return errors.New("control record " + policyFile + " of project " + p.info.Name + " is damaged")
	}
	p.policy.Store(&current)
	return p.voidUnappliedPolicy(current)
}

// voidUnappliedPolicy appends a void line for every write attempt of the
// audit log that current does not contain and no void line cancels yet.
func (p *project) voidUnappliedPolicy(current api.ProjectPolicy) error {
	records, err := p.readPolicyAudit()
	if err != nil {
		return err
	}
	voided := voidedPolicyAttempts(records)
	for _, r := range records {
		if r.Void != nil || voided[r.Attempt] || r.appliedTo(current) {
			continue
		}
		if err := p.appendPolicyAudit(r.void(p.server.now())); err != nil {
			return err
		}
		p.log().Warn("voided a project policy write that did not take effect", "epoch", r.Epoch, "revision", r.Revision, "attempt", r.Attempt)
	}
	return nil
}

func voidedPolicyAttempts(records []policyAuditRecord) map[string]bool {
	voided := map[string]bool{}
	for _, r := range records {
		if r.Void != nil {
			voided[r.Void.Attempt] = true
		}
	}
	return voided
}

// readPolicyAudit reads the audit log. A last line without its newline is
// the torn tail of an append that a crash interrupted; it is ended, so the
// next line starts on its own, and read like the others when it decodes.
func (p *project) readPolicyAudit() ([]policyAuditRecord, error) {
	path := filepath.Join(p.root, policyAuditFile)
	data, found, err := fs.ReadRegularFileIfExists(p.root, path)
	if err != nil || !found {
		return nil, err
	}
	if len(data) > 0 && data[len(data)-1] != '\n' {
		if err := p.appendPolicyLine(nil); err != nil {
			return nil, err
		}
	}
	var records []policyAuditRecord
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64<<10), maxBody)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var r policyAuditRecord
		if err := json.Unmarshal(line, &r); err != nil {
			p.log().Warn("skipped a damaged project policy audit line", "error", err)
			continue
		}
		records = append(records, r)
	}
	return records, scanner.Err()
}

func (p *project) appendPolicyAudit(r any) error {
	line, err := json.Marshal(r)
	if err != nil {
		return err
	}
	return p.appendPolicyLine(line)
}

// appendPolicyLine appends line and a newline to the audit log and syncs it.
func (p *project) appendPolicyLine(line []byte) error {
	file, err := fs.OpenAppendFile(p.root, filepath.Join(p.root, policyAuditFile))
	if err != nil {
		return err
	}
	if _, err := file.Write(append(line, '\n')); err != nil {
		file.Close()
		return err
	}
	if p.policyAppendFault != nil {
		if err := p.policyAppendFault(); err != nil {
			file.Close()
			return err
		}
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// putPolicy is PUT /api/v1/projects/{project}/policy: a compare-and-swap of
// the whole policy on its revision, with an audit line per write attempt. The
// request is checked before the idempotency receipt exists, so a refused
// request leaves no receipt and the client may send a corrected one with the
// same key. The submitter comes from the Attribution hook when there is one,
// and its value can differ between a request and its retry, so it is checked
// only for a write that runs: a retry whose receipt exists replays the first
// result whatever the hook answers now.
func (p *project) putPolicy(key string, req api.PolicyWrite, submitter, clientID string) outcome {
	if !api.ValidIdentity(clientID) {
		return outcome{err: apiError(api.CodeInvalidRequest, api.HeaderClientID+" header is required and must be an identity")}
	}
	values, err := config.ValidatePolicyValues(req.Values)
	if err != nil {
		return outcome{err: apiError(api.CodeInvalidRequest, err.Error())}
	}
	canonical, err := json.Marshal(values)
	if err != nil {
		return outcome{err: err}
	}
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	if p.policyBroken {
		return outcome{err: apiError(api.CodeNotReady, "project "+p.info.Name+" has a project policy write that could not be settled; restart the server to recover it")}
	}
	if api.ValidIdentity(key) {
		var existing receipt
		found, err := readJSON(p.root, keyReceipt(key).rel(), &existing)
		if err != nil {
			return outcome{err: err}
		}
		if !found {
			// A new request: refuse a bad submitter before its receipt exists.
			if _, err := board.NormalizePersonField("submitter", submitter); err != nil {
				return outcome{err: err}
			}
		}
	}
	run := func() outcome {
		updatedBy, err := board.NormalizePersonField("submitter", submitter)
		if err != nil {
			return outcome{err: err}
		}
		return p.writePolicy(key, req.ExpectedRevision, canonical, updatedBy, clientID)
	}
	return p.idempotent(key, policyOperation, req, run, func() (outcome, bool) { return p.policyApplied(key) })
}

func (p *project) writePolicy(key string, expected uint64, values json.RawMessage, updatedBy, clientID string) outcome {
	current := p.policyView()
	if expected != current.Revision {
		return outcome{err: apiError(api.CodePolicyRevisionConflict, "the project policy is at revision "+strconv.FormatUint(current.Revision, 10)+", not "+strconv.FormatUint(expected, 10))}
	}
	epoch := current.Epoch
	if current.Revision == 0 {
		var err error
		if epoch, err = newPolicyID(); err != nil {
			return outcome{err: err}
		}
	}
	attempt, err := newPolicyID()
	if err != nil {
		return outcome{err: err}
	}
	record := policyAuditWrite{
		Attempt: attempt, Epoch: epoch, Revision: current.Revision + 1, At: p.server.now(), UpdatedBy: updatedBy, ClientID: clientID,
		IdempotencyKey: key, OldValues: current.Values, NewValues: values,
	}
	if err := p.appendPolicyAudit(record); err != nil {
		// The line may be in the log although the append failed (a failed sync or
		// close): void the attempt before anything may read it as a write.
		p.voidPolicyAttempt(record)
		return outcome{err: err}
	}
	p.policyFault("audit-appended")
	next := record.policy()
	if err := writeJSON(p.root, policyFile, next); err != nil {
		p.voidPolicyAttempt(record)
		return outcome{err: err}
	}
	p.policy.Store(&next)
	p.policyFault("policy-replaced")
	// Only a write that ran announces itself: a retry answered from its receipt
	// or from the audit log never reaches this point. A failed append rotates the
	// event log, so every subscriber gets a reset and reads the policy anyway.
	if err := p.events.append(api.Event{Kind: api.EventProjectPolicyChanged, Revision: next.Revision}); err != nil {
		p.log().Error("event append failed", "event", api.EventProjectPolicyChanged, "error", err)
	}
	return outcome{status: http.StatusOK, body: next}
}

// policyRenewal adds the current policy version to a lease renewal.
func (p *project) policyRenewal(renewal api.Renewal) api.Renewal {
	policy := p.policyView()
	renewal.PolicyEpoch, renewal.PolicyRevision = policy.Epoch, policy.Revision
	return renewal
}

// voidPolicyAttempt cancels an attempt that did not take effect. When even the
// void line cannot be written, policy writes of the project wait for the
// startup recovery of a restart, which voids it.
func (p *project) voidPolicyAttempt(record policyAuditWrite) {
	if err := p.appendPolicyAudit(record.void(p.server.now())); err != nil {
		p.policyBroken = true
	}
}

// policyApplied answers the retry of a write whose receipt stayed pending:
// the write took effect when the audit log holds an attempt with its key that
// no void line cancels. The answer is the policy that attempt produced, even
// when later writes changed it since.
func (p *project) policyApplied(key string) (outcome, bool) {
	records, err := p.readPolicyAudit()
	if err != nil {
		return outcome{err: err}, true
	}
	voided := voidedPolicyAttempts(records)
	for _, r := range records {
		if r.Void == nil && r.IdempotencyKey == key && !voided[r.Attempt] {
			return outcome{status: http.StatusOK, body: r.policy()}, true
		}
	}
	return outcome{}, false
}

func (p *project) policyFault(stage string) {
	if p.policyFaultHook != nil {
		p.policyFaultHook(stage)
	}
}

// newPolicyID is a random hexadecimal ID: a policy epoch or a write attempt.
func newPolicyID() (string, error) {
	var entropy [16]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(entropy[:]), nil
}

func (s *Server) policyRoutes(mux *routeTable) {
	const route = "/api/v1/projects/{project}/policy"
	s.projectRoute(mux, "GET "+route, func(p *project, _ *http.Request) outcome {
		return outcome{status: http.StatusOK, body: p.policyView()}
	})
	s.projectRoute(mux, "PUT "+route, func(p *project, r *http.Request) outcome {
		// expected_revision is required: a missing or null value must not
		// initialize the policy as an explicit 0 does.
		var wire struct {
			ExpectedRevision *uint64         `json:"expected_revision"`
			Values           json.RawMessage `json:"values"`
			Submitter        string          `json:"submitter,omitempty"`
		}
		if err := decode(r, &wire); err != nil {
			return outcome{err: err}
		}
		if wire.ExpectedRevision == nil {
			return outcome{err: apiError(api.CodeInvalidRequest, "expected_revision is required")}
		}
		req := api.PolicyWrite{ExpectedRevision: *wire.ExpectedRevision, Values: wire.Values, Submitter: wire.Submitter}
		submitter, err := attribution(r, req.Submitter)
		if err != nil {
			return outcome{err: err}
		}
		return p.putPolicy(r.Header.Get(api.HeaderIdempotencyKey), req, submitter, r.Header.Get(api.HeaderClientID))
	})
}
