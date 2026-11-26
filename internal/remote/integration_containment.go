package remote

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dualface/kander/v2/api"
)

// integrationContainment decides how the reviewed commit reaches the observed
// target and records the decision in the request. Delivery tries, in order, a
// via commit when the caller gave one, the commit already in the target, a
// fast-forward, a replay of the review range, and a merge of the target with the
// reviewed commit that the client generates. Other purposes keep the plain
// contained or fast-forward decision.
func (s *Session) integrationContainment(ctx context.Context, dir string, options IntegrationOptions, request *api.IntegrationRequest, carried *api.IntegrationRequest) error {
	target := request.ExpectedTarget
	if options.Purpose != "deliver" {
		contained, err := s.integrationContains(ctx, dir, options.Commit, target)
		request.AlreadyContained = contained
		return err
	}
	if options.ViaCommit != "" {
		return s.viaContainment(ctx, dir, options, request, carried)
	}
	contained, err := s.integrationContains(ctx, dir, options.Commit, target)
	if err != nil {
		return err
	}
	if contained {
		request.Containment, request.AlreadyContained = api.IntegrationContained, true
		return nil
	}
	forward, err := gitAncestor(ctx, dir, target, options.Commit)
	if err != nil {
		return err
	}
	if forward {
		request.Containment = api.IntegrationFastForward
		return nil
	}
	replay, err := s.reviewReplay(ctx, dir, request, target)
	if err != nil {
		return err
	}
	if replay == nil {
		return s.autoMerge(ctx, dir, options, request, options.Commit, carried)
	}
	request.Containment, request.AlreadyContained, request.Replay = api.IntegrationReplayed, true, replay
	return nil
}

// viaContainment accepts a carrier commit only when the common remote has it
// and it descends from the reviewed commit. A carrier that descends from the
// observed target too is used as given. When the target moved past the carrier,
// the target itself carries it if it contains the carrier, and otherwise the
// target and the carrier are merged. Either way the request records the
// caller's carrier as the via source.
func (s *Session) viaContainment(ctx context.Context, dir string, options IntegrationOptions, request *api.IntegrationRequest, carried *api.IntegrationRequest) error {
	via, target := options.ViaCommit, request.ExpectedTarget
	if err := s.FetchCommit(ctx, dir, target); err != nil {
		return err
	}
	// The target was observed on the remote; any other carrier must be served by it.
	if via != target {
		if err := s.FetchCommit(ctx, dir, via); err != nil {
			return errors.Join(err, integrationFailure("remote.integration_via_unavailable"))
		}
		if err := remoteServes(ctx, dir, s.Info.Remote, via); err != nil {
			return errors.Join(err, integrationFailure("remote.integration_via_unavailable"))
		}
	}
	reviewed, err := gitAncestor(ctx, dir, options.Commit, via)
	if err != nil {
		return err
	}
	if !reviewed {
		return integrationFailure("remote.integration_via_invalid")
	}
	forward, err := gitAncestor(ctx, dir, target, via)
	if err != nil {
		return err
	}
	if forward {
		request.Containment, request.ViaCommit, request.AlreadyContained = api.IntegrationVia, via, via == target
		return nil
	}
	contained, err := gitAncestor(ctx, dir, via, target)
	if err != nil {
		return err
	}
	if contained {
		request.Containment, request.ViaCommit, request.AlreadyContained, request.ViaSource = api.IntegrationVia, target, true, via
		return nil
	}
	return s.autoMerge(ctx, dir, options, request, via, carried)
}

// reviewReplay finds the commits of the target that repeat the closed review
// range of the contributing members. Every contributor shares one range.
func (s *Session) reviewReplay(ctx context.Context, dir string, request *api.IntegrationRequest, target string) (*api.IntegrationReplay, error) {
	var base string
	for _, member := range request.Members {
		if member.DeliveryCommit == "" {
			continue
		}
		span, err := s.Project.DispatchRange(ctx, member.TaskID)
		if err != nil {
			return nil, err
		}
		if span.Descendant != request.Commit || base != "" && base != span.Ancestor {
			return nil, nil
		}
		base = span.Ancestor
	}
	if !api.CommitSHA(base) {
		return nil, nil
	}
	if err := s.FetchCommit(ctx, dir, base); err != nil {
		return nil, err
	}
	return findReplay(ctx, dir, base, request.Commit, target)
}

// integrationMemberEdges ties one contributing delivery to the integration and
// returns the edge to add, if any. A delivery that is an ancestor of the
// reviewed commit uses the edge to that commit; otherwise it must already be in
// the target the integration leaves. An edge that the request already carries
// serves the delivery, because the service counts exactly one such edge per
// delivery; the edge check proves it. Assembly and initialization keep the
// plain edge.
func (s *Session) integrationMemberEdges(ctx context.Context, dir string, request api.IntegrationRequest, delivery string) ([]api.IntegrationEdge, error) {
	toReviewed := api.IntegrationEdge{Ancestor: delivery, Descendant: request.Commit}
	if request.Purpose != "deliver" {
		return []api.IntegrationEdge{toReviewed}, nil
	}
	final := request.FinalCommit()
	toFinal := api.IntegrationEdge{Ancestor: delivery, Descendant: final}
	if slices.Contains(request.Edges, toReviewed) || slices.Contains(request.Edges, toFinal) {
		return nil, nil
	}
	reviewed, err := gitAncestor(ctx, dir, delivery, request.Commit)
	if err != nil || reviewed {
		return []api.IntegrationEdge{toReviewed}, err
	}
	if final == request.Commit {
		return nil, integrationFailure("remote.integration_delivery_not_contained")
	}
	if err := s.FetchCommit(ctx, dir, final); err != nil {
		return nil, err
	}
	kept, err := gitAncestor(ctx, dir, delivery, final)
	if err != nil {
		return nil, err
	}
	if !kept {
		return nil, integrationFailure("remote.integration_delivery_not_contained")
	}
	return []api.IntegrationEdge{toFinal}, nil
}

// remoteServes proves that the common remote itself holds a commit. A plain
// fetch succeeds without contact when the object is already in the clone. The
// negotiation asks the remote through the clone's own origin and prints every
// commit of the negotiation tip that the remote acknowledges; the commit itself
// is listed only when the remote has it. Negotiation needs protocol version 2,
// so a failure falls back to fetching the one commit into a scratch repository.
func remoteServes(ctx context.Context, dir, remote, commit string) error {
	acknowledged, err := gitCommand(ctx, dir, "fetch", "--negotiate-only", "--negotiation-tip="+commit, "origin")
	if err != nil {
		return scratchServes(ctx, dir, remote, commit)
	}
	if !slices.Contains(strings.Fields(acknowledged), commit) {
		return integrationFailure("remote.git_commit_unavailable")
	}
	return nil
}

func scratchServes(ctx context.Context, dir, remote, commit string) error {
	url, err := absoluteRemote(ctx, dir, remote)
	if err != nil {
		return err
	}
	scratch, err := os.MkdirTemp("", "kander-remote-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	if _, err := gitCommand(ctx, scratch, "init", "-q", "--bare"); err != nil {
		return err
	}
	_, err = gitCommand(ctx, scratch, "fetch", "-q", "--no-tags", "--depth=1", url, commit)
	return err
}

// absoluteRemote resolves a relative local remote against the work tree, where
// git itself resolves it, so a scratch repository reads the same repository.
func absoluteRemote(ctx context.Context, dir, remote string) (string, error) {
	if strings.Contains(remote, "://") || filepath.IsAbs(remote) {
		return remote, nil
	}
	if host, _, scp := strings.Cut(remote, ":"); scp && host != "" && !strings.Contains(host, "/") {
		return remote, nil
	}
	top, err := gitCommand(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Join(top, remote), nil
}
