# Distributed server

Kander v2 splits the tool into a resident board server, `kander-server`, and the `kander` client. Every host that works on one Git repository talks to the same server over REST, so all hosts see one board, task ownership is decided in one place, and each host executes its own tasks. This document is the design contract of the v2 branch. Parts marked **planned** are not implemented yet.

## Roles

| Side | Owns | Never owns |
| --- | --- | --- |
| `kander-server` | Registered projects, card documents and attachments, claims and leases, handoff records, collaboration requests, idempotency receipts, the event log, published review evidence (**planned**) | Code, Git repositories, worktrees, agent sessions, terminals |
| `kander` client and worker | Agent and terminal control, repository location, worktrees, sessions, temporary logs, unpublished output, the private pending-write queue (**planned**) | The board; a client never writes a local `kanban/` in normal operation |

The first version trusts its network: there is no login and no role model, one server instance serves one data directory, and storage reuses the board package's file transactions instead of a database. Claim tokens and generations isolate concurrent executions; they do not defend against a malicious client on the network. The server binds `127.0.0.1:7878` by default; serving a LAN is an explicit `--listen` choice.

## Data directory

```text
<data>/
  server.lock            exclusive lock; a second instance is refused
  secret.json            server secret that derives claim tokens (0600)
  projects.json          registry: id, name, remote, created_at
  projects/<id>/
    board/               a board in the v1 layout (backlog/ ... trash/)
    control/
      claims/<task>.json         claim record and handoff history
      requests/<task>/<id>.json  collaboration requests
      receipts/<sha256>.json     idempotency receipts
      events.jsonl               append-only change log
      events-epoch.json          log identity for cursors
```

A project name is an identifier (`[A-Za-z0-9][A-Za-z0-9._-]{0,127}`), never a path; the server maps it to an internal ID. The first registration of a name may bind a Git remote URL; a later registration with a different remote is refused with `project_mismatch`, so two repositories cannot share a board by a name collision. No record keys on a client's absolute path.

`kander-server` opens the data directory, takes `server.lock`, recovers every project's interrupted board transactions (`board.RecoverTransactions`), settles claims from the committed card state, and only then reports ready. Until ready, domain routes answer `not_ready` (503).

## Protocol

All routes live below `/api/v1`. Every domain request sends `Kander-Protocol: <n>`; a version outside the server's `[min, current]` range is refused with `protocol_incompatible` (412) before any domain operation. `GET /api/v1/version` returns both ranges so a client can refuse an incompatible server first.

| Route | Purpose |
| --- | --- |
| `GET /health`, `GET /ready`, `GET /version` | Liveness, readiness, protocol range |
| `GET /projects?name=`, `POST /projects` | Resolve or register a project |
| `GET /projects/{p}/tasks[?state=]` | Task list with claim views and the event cursor it is consistent with |
| `POST /projects/{p}/tasks` | `kander new` |
| `GET /projects/{p}/tasks/{t}` | `kander show` |
| `GET`, `PUT /projects/{p}/tasks/{t}/documents/{path}` | Read or publish `spec.md`, `plan.md`, `report.md`, attachments |
| `POST /projects/{p}/tasks/{t}/move` | State moves other than claims |
| `POST /projects/{p}/tasks/{t}/claim`, `/takeover`, `/renew` | Ownership |
| `GET`, `POST /projects/{p}/tasks/{t}/requests`, `POST .../requests/{r}/answer` | Collaboration requests |
| `POST /projects/{p}/tasks/{t}/execution` | Launcher facts of the holder: window, session, confirm, rollback |
| `POST /projects/{p}/reviews/plan`, `POST .../reviews/batches/{b}/aggregate`, `GET .../reviews/batches/{b}/view`, `POST .../reviews/close`, `GET .../reviews/progress/{t}` | Review gate evidence |
| `GET /projects/{p}/events?cursor=&wait=` | Long-polled change log |

Errors are JSON `{code, message, board_code}` with stable codes: `invalid_request`, `not_found`, `revision_conflict`, `held_by_other`, `lease_invalid`, `lease_active`, `claim_required`, `evidence_incomplete`, `protocol_incompatible`, `idempotency_mismatch`, `project_mismatch`, `project_not_empty`, `board_rejected`, `not_ready`, `internal`. `board_code` carries the board package message key of a domain rejection so the client renders the same localized text as v1.

### Idempotency

Every write carries an `Idempotency-Key`. The server writes a pending receipt before the operation and the final answer after it. The same key with the same request returns the stored answer; the same key with a different request is refused with `idempotency_mismatch`. A pending receipt (a crash between the operation and its answer) makes the retry inspect the current state for the operation's effect first and answer from it; the operation runs again only when its effect is not visible. Internal failures stay pending so a retry re-inspects. The client retries transport failures and `not_ready` with the same key.

### Documents

`PUT .../documents/{path}` carries the full text, its SHA-256, the expected card revision, and an optional contract decision. The server checks the digest of the received bytes before publishing, so an interrupted upload never becomes a visible partial document. Publication goes through `board.UpdateDocument`, which keeps every v1 rule: frozen contract fields, managed fields, managed paths, line endings.

## Claims and leases

A claim is the server-side ownership of one task by one execution, kept in `control/claims/<task>.json` outside the card:

- `client_id` (the persistent host identity), `execution_id` (one execution on that host), `owner` (the agent), `generation` (monotonic per task, distinct from the dispatch execution epoch), the lease expiry, and a token.
- The token is an HMAC of the project, task, execution, and generation under the server secret. The server stores only its hash; a replayed claim receipt derives it again, so the token is never written to disk or to a log.
- `POST .../claim` moves a `todo` card into `working` with the owner and grants generation `n+1` in one step under the project lock. A plain move into `working` is refused with `claim_required`; `review -> working` stays a move of the live holder.
- `POST .../renew` with the claim headers (`Kander-Claim-Token`, `Kander-Claim-Generation`) extends the lease by the TTL. A renewal writes no card, so it never changes the card revision. Default TTL 90 s and renewal interval 15 s; the server announces both in the grant and decides time with its own clock.
- A lease past its expiry reads as `lost`. It is never renewed again and the task is never reassigned automatically: `POST .../takeover` with a decision reference and the generation the caller observed hands the task to a new execution with generation `n+1`, appends a handoff record, and supersedes the open requests of older generations. In the same project lock the card's execution ledger hands off (`board.BeginTakeover`): `OWNER`, `SESSION`, and `WINDOW` name the new execution, the old record is retired as handed off, and the new executor reports and confirms its start through `POST .../execution` like a start. A live lease refuses a takeover with `lease_active`. A claimed card without any claim record (a migrated board) can be taken over from generation 0.
- Every write of a `working` or `review` card (document, move, request answer) checks inside the project lock that the presented token and generation are the live claim; another token is `held_by_other`, a stale generation or an expired lease is `lease_invalid`. All writes of a project share that lock with the board transaction, so the ownership check and the write are one atomic step. A user-authorized `archived`/`trash` termination may end a task whose lease is lost; a live lease still refuses it.
- A move into `done`, `archived`, or `trash` releases the claim. If the server stops between the card move and the release, startup releases the claim from the card state.

A claim is written `pending` before the board move and turned `active` after it. Startup turns a pending claim whose card is in `working` with that owner into an active lease, and releases any other pending claim. Old executions therefore cannot overwrite a newer one: their token hash and generation no longer match, whatever order their late renewals, writes, rollbacks, or queued submissions arrive in.

## Starting an agent

`kander start` in server mode claims the `todo` card with a start input (initial session, window, resolved pins); the server runs `board.BeginTaskStart`, so the move, the start metadata, and the pending execution record land in one board transaction, and it keeps the launcher's board cursor in memory for the holder (a restart rebinds a cursor from the current execution record). The client then launches the agent on its own host with a fresh `KANDER_EXECUTION_ID` (in the environment of a spawned process, as an `env NAME=value` prefix of a POSIX container command, or a PowerShell assignment), reports `WINDOW` and the discovered session through `POST .../execution`, and confirms the start. A launch failure posts `rollback`: the server restores the card text from before the claim, returns it to `todo`, and releases the claim. The managed agent PID of a spawned process is stored in the credential, so the worker can stop it when the lease is lost.

## Review gate

The client runs every Git check of `kander review plan`, `aggregate`, `close`, and `progress` against its own repository, exactly as v1 does, and sends the board request together with the Git facts. The server revalidates structure and bindings with the board package and publishes atomically; it never runs Git. Every claimed member of a plan or batch must be held by the caller. A plan over cards of several holders needs a joint confirmation (**planned**) and is refused with `held_by_other`. The other evidence commands (`extend-plan`, `advance`, `assign`, `disposition`, `interpret`, `map-legacy`) are not adapted yet (**planned**). The client prints each answer through its board type, so the aggregate bytes a close request hashes are the same as in v1.

## Client and worker

The client reads `server.url` and `project.name` from the scope config or the project overlay `.kander-config.json`. When `server.url` is set, `list`, `show`, `new`, `update`, `move`, `pick`, `start`, and the review gate commands above run against the server project; the other commands are not adapted yet (**planned**). The client directory (`~/.local/share/kander/client/`, or `KANDER_CLIENT_HOME`) is private to the user and never part of Git:

- `client-id`: the persistent host identity, created on first use.
- `executions/<project>/<task>/<execution>.json`: the credential of each claim this host made (server URL, generation, token, lease timings, the managed agent PID when known), plus `latest`.
- `worker.lock`, `worker.log`: the background worker.

A process started for one execution carries `KANDER_EXECUTION_ID` and presents only that execution's credential, so an agent of an older generation cannot pick up a newer claim made on the same host. An interactive command without the variable uses the task's latest credential. `move <task> working --owner <agent>` claims a `todo` task; with `--decision` it takes over a lost claim.

After a claim the client starts `kander worker` detached unless one already holds `worker.lock`. One worker runs per client home, independent of the TUI and of the command that started it, and exits after two minutes without a live claim. On each tick it renews every credential whose renewal interval passed:

- A successful renewal records the confirmed time.
- A refusal of a claim the server shows as released (the card reached `done`, `archived`, or `trash`) removes the credential.
- Any other refusal (taken over, expired, unknown) marks the credential lost and stops the managed agent. The worktree, the branch, and uncommitted changes stay untouched. If stopping fails the reason is recorded; the server still refuses every later write of that generation.
- A transport failure keeps retrying until a full TTL has passed since the last confirmed renewal; then the worker assumes the lease is gone and handles it as lost, because the server clock has already expired it.

## Collaboration requests

A non-holder asks the holder to act through `POST .../requests` with a kind (`fix`, `sync`, `review`, `stop`), a message, and the generation it targets; a request for a generation that is not current is refused. Only the live holder of that generation answers it (`accepted`, `done`, `rejected`). A takeover marks the open requests of older generations `superseded` instead of handing them to the new execution. Delivering a request to the agent is the worker's job (**planned**); external actions are not exactly-once, so a lost answer is settled by reading the request and the execution facts, never by restarting an agent.

## Events

Each project keeps `control/events.jsonl`. Events carry identities (`task_changed`, `claim_changed`, `request_changed` with task, state, revision, generation), never bodies. A cursor is `<epoch>:<seq>`; the task list returns the cursor it is consistent with. `GET .../events?cursor=&wait=` long-polls up to 60 s. An unknown epoch, a future sequence, or a cursor older than the retained window answers `reset`; the client re-reads the task list and continues from its cursor. The TUI and `kander subscribe` share this stream (**planned**).

## Logging

The server logs JSON lines with method, path, status, client ID, operation ID (derived from the idempotency key), claim generation, and duration; claim transitions log project, task, and generation. Claim tokens are never logged.

## Migrating a v1 board

`kander-server import [--data DIR] --project NAME [--remote URL] --from KANBAN_DIR` imports one v1 board into a project. It takes the data directory lock, so it refuses to run while the server serves that directory. Before running it, stop the v1 executing agents and other writers of the source board, as for `kander init --maintenance`.

1. It recovers the source board's interrupted transactions and takes a snapshot: every regular file, sorted, digested by path and SHA-256. A symlink, junction, or other entry refuses the import.
2. The project must have no tasks. Importing the same snapshot into the same project again answers with the earlier import; any other import into a project with tasks is refused with `project_not_empty`. Two existing boards are never merged.
3. Every file is copied byte for byte into a staging directory beside the project board, the staged snapshot must equal the source snapshot, the staged board must load with the same task set and pass `kander check --all`.
4. Only then is the staging directory published in one rename, and `control/import.json` records the snapshot.

The source board is never changed and stays as the local backup; review originals, journals, and their hashes keep their bytes. Claimed cards of the old board have no claim record, read as generation 0, and need an explicit takeover. Afterwards each clone sets `server.url` and `project.name` (for example in `.kander-config.json`) and stops using the local `kanban/`.

## Build and release

`make build` (and `make-windows.cmd`) builds `kander` and `kander-server`. The release workflow packages each binary for every platform in its own archive (`kander-<os>-<arch>` and `kander-server-<os>-<arch>`), because the client self-updater accepts an archive with exactly one `kander` entry. A tag with a suffix, such as `v2.0.0-alpha.N`, is published as a GitHub prerelease that is never marked latest and never syncs the Homebrew formula; the MSIX job already skips suffixed tags. The update check reads `releases/latest` and refuses prerelease and suffixed versions, so a v1 stable client never installs a v2 prerelease, and a v2 prerelease client is never offered the older v1 stable release.

Run the server with `kander-server --data <dir> --listen <host:port>` (defaults: `$XDG_DATA_HOME/kander-server` or `~/.local/share/kander-server`, `127.0.0.1:7878`); `--ttl` and `--renew` set the lease timings. Health: `GET /api/v1/health`, readiness: `GET /api/v1/ready`, protocol: `GET /api/v1/version`.

## Planned stages

1. Branch conventions, the wire contract, the server (this document).
2. Client configuration (`server.url`, `project.name`, the persistent client ID), the background worker (renewals, lease loss handling, request delivery), and the single-card loop over REST.
3. Review runs, dispatch, task groups, issue import, TUI, `subscribe`, and dsh over REST; joint review publication with holder confirmations.
4. Explicit migration of a v1 board into an empty project, two-binary build, install, update, and `v2.0.0-alpha.N` prereleases.
