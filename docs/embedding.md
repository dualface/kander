# Embedding Kander v2

Another Go module can import `github.com/dualface/kander/v2/api`, `github.com/dualface/kander/v2/server`, `github.com/dualface/kander/v2/client`, and `github.com/dualface/kander/v2/cli`. Require `github.com/dualface/kander/v2` in `go.mod` with `go get github.com/dualface/kander/v2@<commit or tag>`. The `v2.0.0-beta.1` tag was cut before the module path changed and cannot be required. Use a later 2.x tag or a pseudo-version for a later commit on `v2`. Pin the Kander revision used by the host and update them together. The Go embedding API does not promise backward compatibility. The standalone executable and its unhooked `/api/v1` service keep their existing behavior.

## Open the server

Call `server.Open(server.Options{DataDir: absolutePath, Hooks: hooks})`. It takes the exclusive data-directory lock and recovers projects before returning. It does not start a listener, register signals, or exit the process. Zero lease durations use the normal service defaults. Set `Version` if the host needs to identify its embedded build in the version response.

Board remarks from opening and registering projects, serving requests, and online imports go to the instance's `Options.Logger`. Each record has the message `board remark` and the attributes `code` and `text`. The text has trailing line breaks removed. The code `board_journal_partitioned` logs at Info, and all other remark codes log at Warn. A nil logger uses the default JSON logger on stderr.

Authenticate requests in the host and store the authenticated identity in their contexts. Wrap `service.Handler()` with that middleware and mount it at the root of the service URL space. For a prefixed mount, strip the prefix before invoking the Kander handler.

`service.Close()` waits for operations holding a project lock before releasing the data-directory lock. New project requests and those still waiting for a project lock receive `not_ready`. Stop accepting requests and drain in-flight handlers before calling `Close` if you want to avoid those failures.

The host owns authentication, accounts, permissions, and audit policy. A handler without hooks trusts its network, the same as the standalone service.

## Request hooks

- `Hooks.Authorize(ctx, req)` runs once before an eligible request opens or locks a board or decodes its body. Domain requests must first pass the existing protocol and readiness gates. The server info and shutdown routes also run the hook, without a protocol handshake. A nil hook makes no additional permission decision.
- A hook error denies the request with HTTP 403 and code `forbidden`. Its text stays private to the host. Denial does not run the domain handler, reserve an idempotency key, or modify the board.
- The only registered routes that skip the hooks are `GET /api/v1/health`, `GET /api/v1/ready`, and `GET /api/v1/version`. The host can still protect them with its outer middleware.

Use `Request.Pattern` as the route-level policy key. It is the registered Go HTTP pattern, such as `POST /api/v1/projects/{project}/tasks`, not the concrete request URL. Reject unknown patterns by default. `Request.Method` is the actual request method, so a HEAD request matched by a GET route still reports HEAD.

`server.Routes()` lists the patterns registered by `Handler()` without opening a data directory. A route's `Authorized` flag means eligible requests go through `Hooks.Authorize` if it is set. Compare your policy table with `Routes()` in a test so that a Kander upgrade cannot silently leave a new pattern unclassified.

`Request.ProjectID`, `Task`, and `Group` come from the matching path parameters. `Request.Project` is the registered project name resolved from that ID without accessing the board. It is empty when the route has no project parameter or the ID is unknown. `Authorize` does not receive query strings, request bodies, headers, or board-derived membership. In particular, `GET /api/v1/projects`, `POST /api/v1/projects`, and `POST /api/v1/server/import` have no path-bound project and need a host-level policy.

These project routes can select tasks via the body without a `{task}` path parameter. Authorize them at project scope, not for any one task. The table uses the common prefix `/api/v1/projects/{project}`.

| Method and suffix | Body selection |
| --- | --- |
| `POST /check` and `POST /snapshot` | Task selection, and group selection for a snapshot |
| `POST /reviews/plan` and `POST /reviews/members` | Task members |
| `POST /reviews/close` | Tasks named in mechanical assessments |
| `POST /reviews/actions/{action}` | Task members or a task ID, depending on the action |
| `POST /reviews/runtime/prepare` and `POST /reviews/runtime/{run}/{action}` | Review input or run task members for actions that carry them |
| `POST /publications` and `POST /publications/{publication}/confirm` | Publication members or the confirming task |
| `POST /integrations/{integration}/claim` | Integration members |
| `POST /issues/results/view` and `POST /issues/results/save` | The issue's anchor task |

The same project-level limit applies to routes that resolve task membership from a review plan, run, batch, publication, issue, or integration record. The `POST /groups/{group}/claim`, `/facts`, `/reconcile`, `/remove-member`, and `/recover` routes expose the group scope but not individual members selected in their bodies. Project list, creation and event routes are not scoped to a task. The host must not infer task permission from an ID found outside the matched route metadata.

### Assignment authorization

`Hooks.AuthorizeAssignment(ctx, assignment)` is optional and applies only to `POST /tasks/{task}/assignee` under a project. It runs after route authorization and decoding, but before taking the board lock or handling idempotency. `assignment.Request` is the same scope as in `Authorize`. `assignment.Assignee` is the normalized value that would be written, or the empty string to clear the assignment. A malformed body or an assignee that fails the board's validation does not call the hook.

Return or wrap `server.ErrAssigneeRejected` to deny with HTTP 400 and `invalid_request`. Other errors deny with HTTP 403 and `forbidden`. The response never includes the hook's error text. A denial does not modify the board or write a receipt. The hook runs again on idempotent replay. A denial then takes precedence over the stored response and leaves that receipt unchanged.

Like `Authorize`, this hook runs outside the board lock and can read `TaskAccess`. Task facts can change after the hook returns. Include `expected_revision` to reject the write if the task changes between the revision you checked and the write.

### Caller attribution

`Hooks.Attribution(ctx)` takes the accepted request's context. Operations that need an attribution use these rules:

1. No hook configured. Use the value reported by the client.
2. Hook returns `(value, true)`. Use that value even if the client reports something else.
3. Hook returns `(_, false)`. Refuse the attributed operation with HTTP 403 and code `attribution_required`. Never fall back to the client value.

Task creation and issue import use this attribution to populate the immutable `SUBMITTER` header. The two decision write routes use the same attribution for `recorded_by` and also reject a blank attribution. The host can authorize human and Leader writes separately. See [Task decision records](decision-records.md). Task assignment is separate from attribution. `Authorize` receives its task scope and `AuthorizeAssignment` can check the proposed assignee.

### Administration

`POST /api/v1/server/shutdown` still requires a loopback peer and the private administration token even when the hook allows it. In embedded mode it cancels the service's shutdown context and wakes event readers. It does not stop the host's listener, release the data-directory lock, or exit the process. Hosts should normally deny the route and manage shutdown themselves.

## Read task access facts in process

`service.TaskAccess(ctx, projectID, taskID)` reads the task's submitter,
assignee, decision mode, state, revision, and whether it is a request card
(`TaskAccess.Request`). It may be called from either
authorization hook without invoking the hooks again or reading the card files. A missing
project or task returns `not_found`. An untrusted index refuses the
query with `index_rebuilding` until reconciliation succeeds. Host
policy should deny access when the query returns any error.

The facts describe a point-in-time read. The host still owns policy,
including whether to recheck permission or disconnect an existing
connection when the task changes.

## Subscribe in process

`service.Subscribe(ctx, projectID, cursor)` uses the same project events and cursors as HTTP `GET …/events`. Use a cursor from a list or snapshot for a race-free start after that board view. Events include their task IDs and revisions when present in the HTTP stream and arrive in order until a reset.

An empty, stale, or invalid cursor produces a reset. A log epoch change, including a failed event append, also produces a reset. A slow subscriber has a bounded buffer. When it fills, Kander drops that subscriber's backlog and signals a reset without blocking board writes or resetting other subscribers. On reset, discard the stale view, refresh the board, and resume from the new view's cursor. The channel closes on cancellation or service shutdown. In-process subscription does not invoke HTTP hooks. The host must authorize any forwarding of these events.

## Embed the client and CLI

Install a tunnel or other `http.RoundTripper` with `client.SetDefaultTransport(transport)` before making clients. All subsequent Kander client constructions in the process use it, including those inside native CLI commands. Existing clients retain their transport. Passing nil restores the standard HTTP default for new clients.

The typed client keeps `Project.List(ctx, state)` for an unpaginated list.
Use `Project.ListWithOptions(ctx, api.TaskListOptions{...})` for filters,
title search, and page continuations. The host should pin its client and
server revisions together before using the new query options.

Use `Project.Document(ctx, taskID, path)` to read `spec.md`, `plan.md`, `report.md`, or an attachment at a path relative to the card directory. A missing document returns an `*api.Error` with the code `not_found`.

Set `api.NewTask.Description` in `Project.NewTask` to create a request card.
Use `Project.Document(ctx, taskID, "request.md")` to read the protected
record. `TaskSummary.Request` and `TaskAccess.Request` are both true for a
request card and false for an ordinary card. The existing authorization and
attribution hooks still govern creation. See [Request cards](request-cards.md).

The public CLI builds the same tree as the standalone executable, including the bare terminal board. For example, given a transport `tunnel` and an unattached Cobra command `hostCommand`:

```go
root, err := cli.NewRoot(cli.Options{
    Transport: tunnel,
    Commands:  []*cobra.Command{hostCommand},
})
if err != nil {
    return err
}
exitCode := cli.Execute(root, os.Args)
```

`NewRoot` reports a name or alias collision with existing or additional commands. Leaving `Transport` nil preserves the process default. Alternatively, install the transport separately and use `cli.Run(os.Args)` for the unextended tree. Both execution entry points return an exit code.

Transport configuration is process-local. Kander can start the same executable as `kander worker` and for other subprocesses. The embedding binary must install its transport on every process entry, including those paths, before Kander constructs a client.
