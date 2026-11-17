package server

import (
	"reflect"

	"github.com/dualface/kander/internal/api"
)

// integrationContainmentEdges checks that the containment fields of a request
// agree with each other and returns the ancestry edges that tie the request to
// its target. The service never runs Git: the client proves the edges, and the
// service only requires the exact set a containment kind needs.
func integrationContainmentEdges(input api.IntegrationRequest) ([]api.IntegrationEdge, error) {
	invalid := apiError(api.CodeInvalidRequest, "integration containment fields disagree")
	if input.Purpose != "deliver" && (input.Containment != "" || input.ViaCommit != "" || input.ViaAutoMerge || input.ViaSource != "" || input.Replay != nil) {
		return nil, invalid
	}
	containment := input.EffectiveContainment()
	if containment != api.IntegrationVia && (input.ViaAutoMerge || input.ViaSource != "") {
		return nil, invalid
	}
	switch containment {
	case api.IntegrationFastForward:
		if input.AlreadyContained || input.ViaCommit != "" || input.Replay != nil {
			return nil, invalid
		}
	case api.IntegrationContained:
		if !input.AlreadyContained || input.ViaCommit != "" || input.Replay != nil {
			return nil, invalid
		}
		if input.ExpectedTarget == "" {
			return nil, apiError(api.CodeEvidenceIncomplete, "contained-source reconciliation requires an existing target")
		}
	case api.IntegrationVia:
		if !api.CommitSHA(input.ViaCommit) || input.AlreadyContained != (input.ViaCommit == input.ExpectedTarget) || input.Replay != nil {
			return nil, invalid
		}
		if input.ViaAutoMerge && input.AlreadyContained {
			return nil, invalid
		}
		// A source needs a via commit the caller did not give: the generated
		// merge, or the target that already contains the caller's commit.
		if input.ViaSource != "" && (!api.CommitSHA(input.ViaSource) || input.ViaSource == input.ViaCommit || !input.ViaAutoMerge && !input.AlreadyContained) {
			return nil, invalid
		}
	case api.IntegrationReplayed:
		replay := input.Replay
		if replay == nil || !input.AlreadyContained || input.ViaCommit != "" || replay.ReviewTarget != input.Commit {
			return nil, invalid
		}
		for _, commit := range []string{replay.ReviewBase, replay.ReviewTarget, replay.ReplayBase, replay.ReplayTarget} {
			if !api.CommitSHA(commit) {
				return nil, invalid
			}
		}
	default:
		return nil, invalid
	}
	return input.ContainmentEdges(), nil
}

// integrationEdgesMatch requires the request to carry exactly the containment
// edges plus one edge per contributing delivery: either the delivery is an
// ancestor of the reviewed commit, or it is an ancestor of the target after
// the integration. A delivery with both edges, or with neither, is refused. A
// containment edge that is also a delivery edge counts as the delivery's edge.
// A request without containment (an older client, or a purpose other than
// deliver) keeps the earlier rule: the edge to the reviewed commit, always.
func integrationEdgesMatch(input api.IntegrationRequest, containment []api.IntegrationEdge, deliveries []string) bool {
	given := map[api.IntegrationEdge]bool{}
	for _, edge := range input.Edges {
		given[edge] = true
	}
	required := append([]api.IntegrationEdge(nil), containment...)
	final := input.FinalCommit()
	for _, delivery := range deliveries {
		options := []api.IntegrationEdge{{Ancestor: delivery, Descendant: input.Commit}}
		if input.Containment == "" {
			required = append(required, options...)
			continue
		}
		if final != input.Commit {
			options = append(options, api.IntegrationEdge{Ancestor: delivery, Descendant: final})
		}
		var present []api.IntegrationEdge
		for _, option := range options {
			if given[option] {
				present = append(present, option)
			}
		}
		if len(present) != 1 {
			return false
		}
		required = append(required, present...)
	}
	return reflect.DeepEqual(normalizedIntegrationEdges(required), normalizedIntegrationEdges(input.Edges))
}
