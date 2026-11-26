// Package client provides typed access to an embedded or remote Kander service.
package client

import (
	"context"
	"net/http"
	"time"

	"github.com/dualface/kander/v2/api"
	core "github.com/dualface/kander/v2/internal/client"
)

// SetDefaultTransport installs the transport for subsequent New calls
// throughout this process, including the Kander CLI's clients. Existing
// clients keep their transport. Pass nil to restore net/http's default.
func SetDefaultTransport(transport http.RoundTripper) { core.SetDefaultTransport(transport) }

// Client talks to one Kander service with a single client identity.
type Client struct {
	impl *core.Client
}

// New validates the URL and client identity and creates a service client.
func New(serverURL, clientID string) (*Client, error) {
	c, err := core.New(serverURL, clientID)
	if err != nil {
		return nil, err
	}
	return &Client{impl: c}, nil
}

// Version returns the server version and supported wire protocol range.
func (c *Client) Version(ctx context.Context) (api.VersionInfo, error) {
	return c.impl.Version(ctx)
}

// Register registers a project with its name and optional common Git remote.
func (c *Client) Register(ctx context.Context, name, remote string) (api.Project, error) {
	return c.impl.Register(ctx, name, remote)
}

// ProjectByName looks up a project without registering it.
func (c *Client) ProjectByName(ctx context.Context, name string) (api.Project, error) {
	return c.impl.ProjectByName(ctx, name)
}

// Project returns a handle for a registered project ID, without making a request.
func (c *Client) Project(id string) *Project { return &Project{impl: c.impl.Project(id)} }

// Project is a handle for project-scoped requests.
type Project struct {
	impl *core.Project
}

// List lists tasks in a state, or all states when state is empty.
func (p *Project) List(ctx context.Context, state string) (api.TaskList, error) {
	return p.impl.List(ctx, state)
}

// ListWithOptions lists tasks using filters, title search and pagination.
func (p *Project) ListWithOptions(ctx context.Context, options api.TaskListOptions) (api.TaskList, error) {
	return p.impl.ListWithOptions(ctx, options)
}

// Show reads a task by ID.
func (p *Project) Show(ctx context.Context, id string) (api.Task, error) {
	return p.impl.Show(ctx, id)
}

// Document reads a card document at a path relative to the card directory.
func (p *Project) Document(ctx context.Context, id, path string) (api.Document, error) {
	return p.impl.Document(ctx, id, path)
}

// NewTask creates a backlog task, with a protected request.md record when
// req.Description is non-empty. Reuse key when retrying the same operation.
// An empty key generates a new key for this call.
func (p *Project) NewTask(ctx context.Context, key string, req api.NewTask) (api.Task, error) {
	return p.impl.NewTask(ctx, key, req)
}

// Assign changes the person assigned to a task. Reuse key when retrying the
// same operation. An empty key generates a new key for this call.
func (p *Project) Assign(ctx context.Context, key, id string, req api.Assign) (api.Task, error) {
	return p.impl.Assign(ctx, key, id, req)
}

// Confirm admits a request card to todo at the expected revision. Reuse key
// when retrying the same operation. An empty key generates a new key for this call.
func (p *Project) Confirm(ctx context.Context, key, id string, req api.Confirm) (api.Task, error) {
	return p.impl.Confirm(ctx, key, id, req)
}

// Events reads project events after cursor, waiting up to wait on the server.
func (p *Project) Events(ctx context.Context, cursor string, wait time.Duration) (api.EventPage, error) {
	return p.impl.Events(ctx, cursor, wait)
}
