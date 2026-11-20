package remote

import (
	"context"

	"github.com/dualface/kander/internal/api"
)

// IsolateGitConfig lets the tests of package remote_test share the Git
// configuration isolation of package remote.
var IsolateGitConfig = isolateGitConfig

// PublicationID is the ID the coordinator's client gives the joint publication of an operation
// over tasks, before any publication exists.
func PublicationID(ctx context.Context, s *Session, operation string, payload any, tasks []string) (string, error) {
	raw, err := s.publicationPayload(operation, payload)
	if err != nil {
		return "", err
	}
	var members []api.PublicationMember
	if members, _, err = s.liveReviewMembers(ctx, tasks); err != nil {
		return "", err
	}
	if raw, err = s.freezePublicationMaterial(operation, raw, members); err != nil {
		return "", err
	}
	return publicationID(operation, raw, members)
}
