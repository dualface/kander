package remote

import (
	"context"
	"slices"

	"github.com/dualface/kander/internal/api"
)

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
