package remote

import (
	"context"
	"encoding/json"
	"time"

	"github.com/dualface/kander/internal/api"
	"github.com/dualface/kander/internal/client"
)

// ProjectService is the typed domain boundary consumed by client commands and
// views. Implementations perform REST calls; there is no local board adapter.
// Git, terminal control and private execution files remain client concerns.
type ProjectService interface {
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
