package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/dualface/kander/api"
	"github.com/dualface/kander/internal/board"
	"github.com/dualface/kander/internal/i18n"
	"github.com/dualface/kander/internal/server/taskindex"
)

const taskListRoute = "GET /api/v1/projects/{project}/tasks"

func parseTaskList(raw string) (api.TaskListOptions, error) {
	q, err := url.ParseQuery(raw)
	if err != nil {
		return api.TaskListOptions{}, listQueryError("server.index_list_invalid_query")
	}
	for _, name := range []string{"state", "assignee", "submitter", "group", "q", "limit", "cursor"} {
		if len(q[name]) > 1 {
			return api.TaskListOptions{}, listQueryError("server.index_list_invalid_query")
		}
	}
	query := api.TaskListOptions{
		State: q.Get("state"), Assignee: q.Get("assignee"), Submitter: q.Get("submitter"),
		Group: q.Get("group"), Search: q.Get("q"), Cursor: q.Get("cursor"),
	}
	if q.Has("limit") {
		query.Limit, err = strconv.Atoi(q.Get("limit"))
		if err != nil || query.Limit < 1 || query.Limit > api.MaxTaskListLimit {
			return query, listQueryError("server.index_list_limit", api.MaxTaskListLimit)
		}
	}
	for _, value := range []string{query.Search, query.Assignee, query.Submitter, query.Group} {
		if len(value) > 1024 || !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return query, listQueryError("server.index_list_invalid_query")
		}
	}
	if q.Has("cursor") && (query.Limit == 0 || query.Cursor == "") {
		return query, listQueryError("server.index_list_cursor")
	}
	return query, nil
}

type taskListCursor struct {
	Scope    string
	Position taskindex.Position
}

func (p *project) pageScope(query api.TaskListOptions) string {
	raw, _ := json.Marshal([6]string{p.info.ID, query.State, query.Assignee, query.Submitter, query.Group, query.Search})
	return digest(string(raw))
}

func (p *project) pagePosition(query api.TaskListOptions) (*taskindex.Position, error) {
	if query.Cursor == "" {
		return nil, nil
	}
	if len(query.Cursor) > 2048 {
		return nil, listQueryError("server.index_list_cursor")
	}
	cursor, err := base64.RawURLEncoding.DecodeString(query.Cursor)
	var c taskListCursor
	if err != nil || json.Unmarshal(cursor, &c) != nil || c.Scope != p.pageScope(query) ||
		c.Position.StateRank < 0 || c.Position.StateRank >= len(board.States) {
		return nil, listQueryError("server.index_list_cursor")
	}
	if id, err := board.NormalizeTaskID(c.Position.TaskID); err != nil || id != c.Position.TaskID {
		return nil, listQueryError("server.index_list_cursor")
	}
	return &c.Position, nil
}

func listQueryError(key string, args ...any) *api.Error {
	return apiError(api.CodeInvalidRequest, i18n.Text("en", key, args...))
}

func (p *project) list(ctx context.Context, query api.TaskListOptions) (api.TaskList, error) {
	if err := p.lockIndex(); err != nil {
		return api.TaskList{}, err
	}
	defer p.unlockBoard()
	after, err := p.pagePosition(query)
	if err != nil {
		return api.TaskList{}, err
	}
	// Capture the stream cursor under the same project lock, before SQL.
	cursor := p.events.cursor()
	rows, more, err := p.index.List(ctx, query, after)
	if err != nil {
		return api.TaskList{}, p.indexReadError(ctx, err)
	}
	warnings, err := p.index.Warnings(ctx)
	if err != nil {
		return api.TaskList{}, p.indexReadError(ctx, err)
	}
	out := makeTaskList(rows, warnings, cursor, p.server.now())
	if more {
		last := rows[len(rows)-1].Position()
		raw, _ := json.Marshal(taskListCursor{Scope: p.pageScope(query), Position: last})
		out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
	}
	return out, nil
}

func makeTaskList(rows []taskindex.Row, warnings []string, cursor string, now time.Time) api.TaskList {
	out := api.TaskList{ObservedAt: now, Cursor: cursor, Warnings: warnings, Tasks: make([]api.TaskSummary, 0, len(rows))}
	for _, row := range rows {
		task := row.Summary
		if row.Claim != nil && row.Claim.Status != claimPending {
			task.Claim = visibleClaim(*row.Claim, now)
		}
		out.Tasks = append(out.Tasks, task)
	}
	return out
}
