package remote

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/client"
)

// ProjectService is the typed domain boundary consumed by client commands and
// views. Implementations perform REST calls; there is no local board adapter.
// Git, terminal control and private execution files remain client concerns.
type ProjectService interface {
	ExecutionState(context.Context, string, client.Claim) (api.ExecutionState, error)
	PublishDelivery(context.Context, string, api.DeliveryEvidence, client.Claim) (api.DeliveryEvidence, error)
	Delivery(context.Context, string, string) (api.DeliveryEvidence, error)
	Dispatch(context.Context, string, string) (board.Dispatch, error)
	DispatchAction(context.Context, string, string, string, api.DispatchCommand, client.Claim) (board.Dispatch, error)
	DispatchRange(context.Context, string) (board.ReviewGitEdge, error)
	ValidateDispatch(context.Context, string, string) error
	ObserveGroup(context.Context, string, string, api.AgentObservation, client.Claim) (api.Renewal, error)
	ReleaseGroup(context.Context, string, string, api.GroupRelease, client.Claim) (api.Renewal, error)
	Group(context.Context, string) (api.GroupView, error)
	ClaimGroup(context.Context, string, string, api.GroupClaim, *client.Claim) (api.GroupGrant, error)
	RenewGroup(context.Context, string, client.Claim) (api.Renewal, error)
	CoordinatorFacts(context.Context, string, board.CoordinatorReconcile) ([]board.CoordinatorGitFacts, error)
	CoordinatorAction(context.Context, string, string, string, any, client.Claim) (board.CoordinatorCheckpoint, error)
	Check(context.Context, api.CheckQuery) (api.CheckResult, error)
	Snapshot(context.Context, api.SnapshotQuery) (api.BoardSnapshot, error)
	ObserveAgent(context.Context, string, string, api.AgentObservation, client.Claim) (api.ClaimView, error)
	ReviewRuntime(context.Context, string, string, string, any, *client.Claim) (json.RawMessage, error)
	ReviewEvidence(context.Context, string, string, string) (json.RawMessage, error)
	ReviewRemaining(context.Context, []string) ([]string, error)
	PreparePublication(context.Context, api.PublicationPrepare) (api.Publication, error)
	Publication(context.Context, string) (api.Publication, error)
	ConfirmPublication(context.Context, string, api.PublicationApproval, client.Claim) (api.Publication, error)
	ReviewAction(context.Context, string, string, any, *client.Claim) (json.RawMessage, error)
	ReviewResource(context.Context, string, string) (json.RawMessage, error)
	ImportIssue(context.Context, string, api.IssueImport) (json.RawMessage, error)
	IssueIndex(context.Context) (json.RawMessage, error)
	ID() string
	List(context.Context, string) (api.TaskList, error)
	Show(context.Context, string) (api.Task, error)
	NewTask(context.Context, string, api.NewTask) (api.Task, error)
	Document(context.Context, string, string) (api.Document, error)
	PutDocument(context.Context, string, string, api.Document, *client.Claim) (api.Task, error)
	Move(context.Context, string, string, api.Move, *client.Claim) (api.Task, error)
	Claim(context.Context, string, string, api.ClaimRequest) (api.ClaimGrant, error)
	Takeover(context.Context, string, string, api.ClaimRequest) (api.ClaimGrant, error)
	Execution(context.Context, string, string, api.ExecutionUpdate, client.Claim) (api.Task, error)
	Renew(context.Context, string, client.Claim) (api.Renewal, error)
	Requests(context.Context, string) ([]api.CollabRequest, error)
	CreateRequest(context.Context, string, string, api.NewCollabRequest) (api.CollabRequest, error)
	AnswerRequest(context.Context, string, string, string, api.CollabAnswer, client.Claim) (api.CollabRequest, error)
	Events(context.Context, string, time.Duration) (api.EventPage, error)
	ReviewPlan(context.Context, string, any, *client.Claim) (json.RawMessage, error)
	ReviewAggregate(context.Context, string, string, *client.Claim) (json.RawMessage, error)
	ReviewBatchView(context.Context, string) (json.RawMessage, error)
	ReviewClose(context.Context, string, any, *client.Claim) (json.RawMessage, error)
	ReviewProgress(context.Context, string) (json.RawMessage, error)
}

var _ ProjectService = (*client.Project)(nil)

// ClaimsFor resolves each task independently. A process-bound execution cannot
// borrow a later generation or another execution from the latest pointer.
func (s *Session) ClaimsFor(tasks []string) (*client.Claim, error) {
	proofs := map[string]api.ClaimProof{}
	for _, task := range tasks {
		credential, found, err := s.Credential(task)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		if credential.Lost {
			return nil, &api.Error{Code: api.CodeLeaseInvalid, Message: "this execution lost its lease"}
		}
		proofs[task] = api.ClaimProof{Token: credential.Token, Generation: credential.Generation}
	}
	if len(proofs) == 0 {
		return nil, nil
	}
	return &client.Claim{Members: proofs}, nil
}
