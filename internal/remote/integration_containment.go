package remote

import (
	"context"
	"errors"
	"os"

	"github.com/dualface/kander/internal/api"
)

// integrationContainment decides how the reviewed commit reaches the observed
// target and records the decision in the request. Delivery tries, in order, a
// via commit when the caller gave one, the commit already in the target, a
// fast-forward, and a replay of the review range. Other purposes keep the
// plain contained or fast-forward decision.
func (s *Session) integrationContainment(ctx context.Context, dir string, options IntegrationOptions, request *api.IntegrationRequest) error {
	target := request.ExpectedTarget
	if options.Purpose != "deliver" {
		contained, err := s.integrationContains(ctx, dir, options.Commit, target)
		request.AlreadyContained = contained
		return err
	}
	if options.ViaCommit != "" {
		return s.viaContainment(ctx, dir, options, request)
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
		return integrationFailure("remote.integration_target_diverged")
	}
	request.Containment, request.AlreadyContained, request.Replay = api.IntegrationReplayed, true, replay
	return nil
}

// viaContainment accepts a carrier commit only when the common remote has it
// and it descends from both the reviewed commit and the observed target.
func (s *Session) viaContainment(ctx context.Context, dir string, options IntegrationOptions, request *api.IntegrationRequest) error {
	via, target := options.ViaCommit, request.ExpectedTarget
	if err := s.FetchCommit(ctx, dir, target); err != nil {
		return err
	}
	// The target was observed on the remote; any other carrier must be served by it.
	if via != target {
		if err := s.FetchCommit(ctx, dir, via); err != nil {
			return errors.Join(err, integrationFailure("remote.integration_via_unavailable"))
		}
		if err := remoteServes(ctx, s.Info.Remote, via); err != nil {
			return errors.Join(err, integrationFailure("remote.integration_via_unavailable"))
		}
	}
	for _, ancestor := range []string{options.Commit, target} {
		descends, err := gitAncestor(ctx, dir, ancestor, via)
		if err != nil {
			return err
		}
		if !descends {
			return integrationFailure("remote.integration_via_invalid")
		}
	}
	request.Containment, request.ViaCommit, request.AlreadyContained = api.IntegrationVia, via, via == target
	return nil
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

// integrationMemberEdge ties one contributing delivery to the integration. A
// delivery that is an ancestor of the reviewed commit keeps the plain edge;
// otherwise it must already be in the target the integration leaves behind.
// Assembly and initialization keep the plain edge, which the edge check proves.
func (s *Session) integrationMemberEdge(ctx context.Context, dir string, request api.IntegrationRequest, delivery string) (api.IntegrationEdge, error) {
	edge := api.IntegrationEdge{Ancestor: delivery, Descendant: request.Commit}
	if request.Purpose != "deliver" {
		return edge, nil
	}
	reviewed, err := gitAncestor(ctx, dir, delivery, request.Commit)
	if err != nil || reviewed {
		return edge, err
	}
	final := request.FinalCommit()
	if final == request.Commit {
		return edge, integrationFailure("remote.integration_delivery_not_contained")
	}
	if err := s.FetchCommit(ctx, dir, final); err != nil {
		return edge, err
	}
	kept, err := gitAncestor(ctx, dir, delivery, final)
	if err != nil {
		return edge, err
	}
	if !kept {
		return edge, integrationFailure("remote.integration_delivery_not_contained")
	}
	return api.IntegrationEdge{Ancestor: delivery, Descendant: final}, nil
}

// remoteServes proves that the common remote itself holds a commit. A plain
// fetch succeeds without contact when the object is already in the clone, so
// the probe fetches one shallow commit into an empty scratch repository.
func remoteServes(ctx context.Context, remote, commit string) error {
	scratch, err := os.MkdirTemp("", "kander-remote-probe-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)
	if _, err := gitCommand(ctx, scratch, "init", "-q", "--bare"); err != nil {
		return err
	}
	_, err = gitCommand(ctx, scratch, "fetch", "-q", "--no-tags", "--depth=1", "--filter=blob:none", remote, commit)
	return err
}
