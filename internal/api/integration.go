package api

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"
)

// IntegrationMember freezes one member's execution and optional contribution.
// Non-contributors still belong to the complete group topology.
type IntegrationMember struct {
	TaskID         string `json:"task_id"`
	Revision       uint64 `json:"revision"`
	Generation     uint64 `json:"generation"`
	DeliveryCommit string `json:"delivery_commit,omitempty"`
}

type IntegrationEdge struct {
	Ancestor   string `json:"ancestor"`
	Descendant string `json:"descendant"`
}

type IntegrationObservation struct {
	Integration *Integration `json:"integration,omitempty"`
	Ticket      string       `json:"ticket"`
}

type IntegrationRequest struct {
	AlreadyContained   bool                `json:"already_contained,omitzero"`
	ObservationTicket  string              `json:"observation_ticket"`
	ExpectedRevision   uint64              `json:"expected_revision"`
	Edges              []IntegrationEdge   `json:"edges"`
	Purpose            string              `json:"purpose"`
	TargetRef          string              `json:"target_ref"`
	BaseCommit         string              `json:"base_commit"`
	ExpectedTarget     string              `json:"expected_target,omitempty"`
	Commit             string              `json:"commit"`
	GroupID            string              `json:"group_id,omitempty"`
	ExecutionID        string              `json:"execution_id"`
	ActorExecutionID   string              `json:"actor_execution_id"`
	ExpectedGeneration uint64              `json:"expected_generation"`
	Decision           string              `json:"decision"`
	Members            []IntegrationMember `json:"members"`
}

type Integration struct {
	SourceRef           string             `json:"source_ref"`
	ID                  string             `json:"id"`
	Revision            uint64             `json:"revision"`
	Generation          uint64             `json:"generation"`
	ClientID            string             `json:"client_id"`
	Status              string             `json:"status"`
	Phase               string             `json:"phase"`
	Request             IntegrationRequest `json:"request"`
	AuthorityGeneration uint64             `json:"authority_generation"`
	ObservedTarget      string             `json:"observed_target,omitempty"`
	ObservedAt          time.Time          `json:"observed_at,omitzero"`
	RenewedAt           time.Time          `json:"renewed_at"`
	ExpiresAt           time.Time          `json:"expires_at"`
}

type IntegrationGrant struct {
	Integration   Integration `json:"integration"`
	Token         string      `json:"token"`
	RenewInterval Duration    `json:"renew_interval"`
	TTL           Duration    `json:"ttl"`
}

type IntegrationCommand struct {
	ObservationTicket string            `json:"observation_ticket,omitempty"`
	Edges             []IntegrationEdge `json:"edges,omitempty"`
	Action            string            `json:"action"`
	ExpectedRevision  uint64            `json:"expected_revision"`
	ObservedTarget    string            `json:"observed_target,omitempty"`
	ObservedAt        time.Time         `json:"observed_at,omitzero"`
	Decision          string            `json:"decision,omitempty"`
}

func IntegrationID(ref string) string {
	sum := sha256.Sum256([]byte(ref))
	return "i-" + hex.EncodeToString(sum[:])
}

// IntegrationTarget accepts a conservative complete heads ref. Git performs
// its own check-ref-format on the client before any network mutation.
func IntegrationTarget(ref string) bool {
	if !strings.HasPrefix(ref, "refs/heads/") || len(ref) > 512 || strings.HasSuffix(ref, "/") || strings.Contains(ref, "..") || strings.Contains(ref, "//") {
		return false
	}
	for _, c := range ref {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._-/", c)) {
			return false
		}
	}
	for part := range strings.SplitSeq(strings.TrimPrefix(ref, "refs/heads/"), "/") {
		if part == "" || strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".") || strings.HasSuffix(part, ".lock") {
			return false
		}
	}
	return true
}

// IntegrationSourceRef publishes a coordinator's composed commit without
// borrowing any member's delivery namespace or replacing a mutable branch.
func IntegrationSourceRef(project, execution, commit string) string {
	return "refs/heads/kander/" + project + "/integrations/" + execution + "/" + commit
}
