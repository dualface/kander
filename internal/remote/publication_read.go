package remote

import (
	"context"
	"slices"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/client"
	"github.com/dualface/kander/internal/config"
)

// readOnlyClientID names the client of a read-only session. It is no identity of this host: a
// read-only command creates no client identity file.
const readOnlyClientID = "readonly-session"

// OpenReadOnly resolves the project of cfg for a command that only reads. Open creates the client
// identity of this host and registers an unknown project, so a read that fails could leave both
// behind. OpenReadOnly sends only GET requests, writes nothing below the client home, and fails
// with not_found for a project the service does not know. It checks the protocol and, like Open,
// that the origin of the checkout at dir is the remote the project is bound to. The session has no
// client home, so it serves calls that need no host-private record.
func OpenReadOnly(ctx context.Context, cfg *config.Config, dir string) (*Session, error) {
	if cfg == nil || cfg.Project.Name == "" {
		return nil, ErrNotConfigured
	}
	c, err := client.New(config.ServerURL(cfg), readOnlyClientID)
	if err != nil {
		return nil, err
	}
	if _, err := c.CheckCompatible(ctx); err != nil {
		return nil, err
	}
	repository, err := ResolveGitRemote(ctx, dir)
	if err != nil {
		return nil, err
	}
	info, err := c.ProjectByName(ctx, cfg.Project.Name)
	if err != nil {
		return nil, err
	}
	if repository != "" && info.Remote != "" && info.Remote != repository {
		return nil, &api.Error{Code: api.CodeProjectMismatch, Message: "project " + cfg.Project.Name + " is bound to another Git remote"}
	}
	return &Session{ServerURL: config.ServerURL(cfg), Client: c, Project: c.Project(info.ID), Info: info}, nil
}

// MemberConfirmed reports whether the publication holds a confirmation of the
// member that names the digest of the publication and the member's own
// revision and generation. It describes what the publication stores and reads
// no card, so it also holds for a member whose card changed since.
func MemberConfirmed(publication api.Publication, member api.PublicationMember) bool {
	approval, found := publication.Approvals[member.TaskID]
	return found && approval.Digest == publication.Digest && approval.Revision == member.Revision && approval.Generation == member.Generation
}

// UnconfirmedMembers lists, in task ID order, the members that
// MemberConfirmed does not accept.
func UnconfirmedMembers(publication api.Publication) []string {
	pending := []string{}
	for _, member := range publication.Members {
		if !MemberConfirmed(publication, member) {
			pending = append(pending, member.TaskID)
		}
	}
	slices.Sort(pending)
	return pending
}

// MemberStale reads the member's card and reports whether the publication can
// no longer be confirmed or published for it: the card has another revision,
// another execution generation, or no live claim. A card that cannot be read
// is an error, not a guess.
func (s *Session) MemberStale(ctx context.Context, member api.PublicationMember) (bool, error) {
	task, err := s.Project.Show(ctx, member.TaskID)
	if err != nil {
		return false, err
	}
	if task.Claim == nil || !api.ClaimStands(task.Claim.Status) {
		return true, nil
	}
	return task.Revision != member.Revision || task.Claim.Generation != member.Generation, nil
}
