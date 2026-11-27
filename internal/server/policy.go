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

// policyAuditRecord is one line of the audit log: a write, or the void of a
// write that never took effect.
type policyAuditRecord struct {
	Epoch          string          `json:"epoch,omitempty"`
	Revision       uint64          `json:"revision,omitempty"`
	At             time.Time       `json:"at"`
	UpdatedBy      string          `json:"updated_by,omitempty"`
	ClientID       string          `json:"client_id,omitempty"`
	IdempotencyKey string          `json:"idempotency_key,omitempty"`
	OldValues      json.RawMessage `json:"old_values,omitempty"`
	NewValues      json.RawMessage `json:"new_values,omitempty"`
	Void           *policyVoid     `json:"void,omitempty"`
	Reason         string          `json:"reason,omitempty"`
}

// policyVoid names the write a void line cancels.
type policyVoid struct {
	Epoch          string `json:"epoch"`
	Revision       uint64 `json:"revision"`
	IdempotencyKey string `json:"idempotency_key,omitempty"`
}

func (r policyAuditRecord) policy() api.ProjectPolicy {
	return api.ProjectPolicy{Epoch: r.Epoch, Revision: r.Revision, Values: r.NewValues, UpdatedAt: r.At, UpdatedBy: r.UpdatedBy}
}

// appliedTo reports whether the write of r is part of current: the same epoch
// and a revision current has reached.
func (r policyAuditRecord) appliedTo(current api.ProjectPolicy) bool {
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
// effect. Offline maintenance only reads, so it changes no policy file.
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
	if p.server.maintenance != nil {
		return nil
	}
	return p.voidUnappliedPolicy(current)
}

// voidUnappliedPolicy appends a void line for every write of the audit log
// that current does not contain and no void line cancels yet.
func (p *project) voidUnappliedPolicy(current api.ProjectPolicy) error {
	records, err := p.readPolicyAudit()
	if err != nil {
		return err
	}
	voided := voidedPolicyWrites(records)
	for _, r := range records {
		if r.Void != nil || voided[policyWriteID(r.Epoch, r.Revision, r.IdempotencyKey)] || r.appliedTo(current) {
			continue
		}
		if err := p.appendPolicyAudit(policyAuditRecord{
			At: p.server.now(), Void: &policyVoid{Epoch: r.Epoch, Revision: r.Revision, IdempotencyKey: r.IdempotencyKey}, Reason: policyVoidReason,
		}); err != nil {
			return err
		}
		p.log().Warn("voided a project policy write that did not take effect", "epoch", r.Epoch, "revision", r.Revision)
	}
	return nil
}

func policyWriteID(epoch string, revision uint64, key string) string {
	data, _ := json.Marshal([]any{epoch, revision, key})
	return string(data)
}

func voidedPolicyWrites(records []policyAuditRecord) map[string]bool {
	voided := map[string]bool{}
	for _, r := range records {
		if r.Void != nil {
			voided[policyWriteID(r.Void.Epoch, r.Void.Revision, r.Void.IdempotencyKey)] = true
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
	if len(data) > 0 && data[len(data)-1] != '\n' && p.server.maintenance == nil {
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

func (p *project) appendPolicyAudit(r policyAuditRecord) error {
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
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

// putPolicy is PUT /api/v1/projects/{project}/policy: a compare-and-swap of
// the whole policy on its revision, with an audit line per write.
func (p *project) putPolicy(key string, req api.PolicyWrite, submitter, clientID string) outcome {
	if err := p.lockBoard(); err != nil {
		return outcome{err: err}
	}
	defer p.unlockBoard()
	if p.policyBroken {
		return outcome{err: apiError(api.CodeNotReady, "project "+p.info.Name+" has a project policy write that could not be settled; restart the server to recover it")}
	}
	run := func() outcome { return p.writePolicy(key, req, submitter, clientID) }
	return p.idempotent(key, policyOperation, req, run, func() (outcome, bool) { return p.policyApplied(key) })
}

func (p *project) writePolicy(key string, req api.PolicyWrite, submitter, clientID string) outcome {
	values, err := config.ValidatePolicyValues(req.Values)
	if err != nil {
		return outcome{err: apiError(api.CodeInvalidRequest, err.Error())}
	}
	updatedBy, err := board.NormalizePersonField("submitter", submitter)
	if err != nil {
		return outcome{err: err}
	}
	current := p.policyView()
	if req.ExpectedRevision != current.Revision {
		return outcome{err: apiError(api.CodePolicyRevisionConflict, "the project policy is at revision "+strconv.FormatUint(current.Revision, 10)+", not "+strconv.FormatUint(req.ExpectedRevision, 10))}
	}
	canonical, err := json.Marshal(values)
	if err != nil {
		return outcome{err: err}
	}
	epoch := current.Epoch
	if current.Revision == 0 {
		if epoch, err = newPolicyEpoch(); err != nil {
			return outcome{err: err}
		}
	}
	record := policyAuditRecord{
		Epoch: epoch, Revision: current.Revision + 1, At: p.server.now(), UpdatedBy: updatedBy, ClientID: clientID,
		IdempotencyKey: key, OldValues: current.Values, NewValues: canonical,
	}
	if err := p.appendPolicyAudit(record); err != nil {
		return outcome{err: err}
	}
	p.policyFault("audit-appended")
	next := record.policy()
	if err := writeJSON(p.root, policyFile, next); err != nil {
		// The audit line names a write that did not happen: void it now, or
		// keep later writes out until startup recovery does.
		if voidErr := p.appendPolicyAudit(policyAuditRecord{
			At: p.server.now(), Void: &policyVoid{Epoch: record.Epoch, Revision: record.Revision, IdempotencyKey: key}, Reason: policyVoidReason,
		}); voidErr != nil {
			p.policyBroken = true
		}
		return outcome{err: err}
	}
	p.policy.Store(&next)
	p.policyFault("policy-replaced")
	return outcome{status: http.StatusOK, body: next}
}

// policyApplied answers the retry of a write whose receipt stayed pending:
// the write took effect when the audit log holds a line with its key that no
// void line cancels. The answer is the policy that write produced, even when
// later writes changed it since.
func (p *project) policyApplied(key string) (outcome, bool) {
	records, err := p.readPolicyAudit()
	if err != nil {
		return outcome{err: err}, true
	}
	voided := voidedPolicyWrites(records)
	for _, r := range records {
		if r.Void == nil && r.IdempotencyKey == key && !voided[policyWriteID(r.Epoch, r.Revision, key)] {
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

func newPolicyEpoch() (string, error) {
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
		var req api.PolicyWrite
		if err := decode(r, &req); err != nil {
			return outcome{err: err}
		}
		submitter, err := attribution(r, req.Submitter)
		if err != nil {
			return outcome{err: err}
		}
		return p.putPolicy(r.Header.Get(api.HeaderIdempotencyKey), req, submitter, r.Header.Get(api.HeaderClientID))
	})
}
