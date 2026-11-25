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

// Containment values record how a reviewed commit reaches the target branch.
// A via request names the commit the target branch holds afterwards in
// ViaCommit. ViaAutoMerge marks that commit as a merge the client generated
// from the observed target and the reviewed commit, or from the caller's via
// commit when ViaSource records it. ViaSource also appears when the target
// already contains the caller's via commit and ViaCommit is the target.
const (
	IntegrationFastForward = "fast_forward"
	IntegrationContained   = "contained"
	IntegrationVia         = "via"
	IntegrationReplayed    = "replayed"
)

// IntegrationReplay records the review range and the commits of the target
// branch whose patches equal it, one for one and in order.
type IntegrationReplay struct {
	ReviewBase   string `json:"review_base"`
	ReviewTarget string `json:"review_target"`
	ReplayBase   string `json:"replay_base"`
	ReplayTarget string `json:"replay_target"`
}

type IntegrationRequest struct {
	AlreadyContained   bool                `json:"already_contained,omitzero"`
	Containment        string              `json:"containment,omitzero"`
	ViaCommit          string              `json:"via_commit,omitzero"`
	ViaAutoMerge       bool                `json:"via_auto_merge,omitzero"`
	ViaSource          string              `json:"via_source,omitzero"`
	Replay             *IntegrationReplay  `json:"replay,omitzero"`
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

// EffectiveContainment reads a request that predates the containment field
// through its already_contained flag.
func (r IntegrationRequest) EffectiveContainment() string {
	switch {
	case r.Containment != "":
		return r.Containment
	case r.AlreadyContained:
		return IntegrationContained
	default:
		return IntegrationFastForward
	}
}

// FinalCommit is the commit the target branch holds, or descends from, once the
// request completes: the reviewed commit after a fast-forward, the carrier
// commit for a via request, and the observed target otherwise.
func (r IntegrationRequest) FinalCommit() string {
	switch r.EffectiveContainment() {
	case IntegrationFastForward:
		return r.Commit
	case IntegrationVia:
		return r.ViaCommit
	default:
		return r.ExpectedTarget
	}
}

// PushCommit is the commit a request that needs a push writes to the target.
func (r IntegrationRequest) PushCommit() string {
	if r.ViaCommit != "" {
		return r.ViaCommit
	}
	return r.Commit
}

// ConfirmAncestor is the commit that the observed target must contain to
// confirm the request: the replayed range ends before the reviewed commit.
func (r IntegrationRequest) ConfirmAncestor() string {
	if r.EffectiveContainment() == IntegrationReplayed && r.Replay != nil {
		return r.Replay.ReplayTarget
	}
	return r.Commit
}

// ContainmentEdges are the ancestry facts that tie the request commit to the
// observed target for its containment kind. A via request that records the
// caller's own commit as its source adds the edge from that source.
func (r IntegrationRequest) ContainmentEdges() []IntegrationEdge {
	switch r.EffectiveContainment() {
	case IntegrationContained:
		return []IntegrationEdge{{Ancestor: r.Commit, Descendant: r.ExpectedTarget}}
	case IntegrationVia:
		edges := []IntegrationEdge{{Ancestor: r.ExpectedTarget, Descendant: r.ViaCommit}, {Ancestor: r.Commit, Descendant: r.ViaCommit}}
		if r.ViaSource != "" {
			edges = append(edges, IntegrationEdge{Ancestor: r.ViaSource, Descendant: r.ViaCommit})
		}
		return edges
	case IntegrationReplayed:
		if r.Replay == nil {
			return nil
		}
		return []IntegrationEdge{{Ancestor: r.Replay.ReplayTarget, Descendant: r.ExpectedTarget}}
	default:
		return []IntegrationEdge{{Ancestor: r.BaseCommit, Descendant: r.Commit}}
	}
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
