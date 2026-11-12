# Distributed server

Kander v2 splits the tool into a resident board server, `kander-server`, and the `kander` client. Every host that works on one Git repository talks to the same server over REST, so all hosts see one board, task ownership is decided in one place, and each host executes its own tasks. This document is the design contract of the v2 branch. Parts marked **planned** are not implemented yet.

## Roles

| Side | Owns | Never owns |
| --- | --- | --- |
| `kander-server` | Registered projects, card documents and attachments, claims and leases, handoff records, collaboration requests, idempotency receipts, the event log, published review evidence | Code, Git repositories, worktrees, agent sessions, terminals |
| `kander` client and worker | Agent and terminal control, repository location, worktrees, sessions, temporary logs, unpublished output, the private action and request journals | The board; a client never writes a local `kanban/` in normal operation |

The first version trusts its network: there is no login and no role model, one server instance serves one data directory, and storage reuses the board package's file transactions instead of a database. Claim tokens and generations isolate concurrent executions; they do not defend against a malicious client on the network. The server binds `127.0.0.1:7878` by default; serving a LAN is an explicit `--listen` choice.

## Data directory

```text
<data>/
  server.lock            exclusive lock; a second instance is refused
  secret.json            server secret that derives claim tokens (0600)
  projects.json          registry: id, name, remote, created_at
  projects/<id>/
    board/               a board in the v1 layout (backlog/ ... trash/)
      .kander/groups/00000000-service-group/
        claims/<task>.json       task claim and handoff history
        group-claims/<group>.json coordinator lease and handoffs
        deliveries/<task>/<commit>.json current authenticated Git publication
        delivery-history/<task>/<digest>.json immutable earlier attestations
        receipts/<sha256>.json   idempotency receipts
        requests/<task>/<id>.json generation-bound collaboration requests
        publications/<id>.json   immutable joint material and confirmations
        review-writers/<id>.json private uploader binding and execution generations
        integrations/<target-id>/current.json target lease and observed Git phase
        integrations/<target-id>/executions/<execution>.json historical target result
        task-integrations/<task>/<generation>.json final delivery receipt
        issue-results/<source-key-digest>.json result CAS and durable external intents
      .kander/groups/00000000-review-archive-group/  review inputs and originals
    control/
      events.jsonl               bounded event diagnostic log
```

A project name is an identifier (`[A-Za-z0-9][A-Za-z0-9._-]{0,127}`), never a path; the server maps it to an internal ID. The first registration of a name may bind a Git remote URL; a later registration with a different remote is refused with `project_mismatch`, so two repositories cannot share a board by a name collision. No record keys on a client's absolute path.

`kander-server` opens the data directory, takes `server.lock`, recovers every project's interrupted board transactions (`board.RecoverTransactions`), settles claims from the committed card state, and only then reports ready. Until ready, domain routes answer `not_ready` (503).

## Offline card maintenance

After an explicit import, legacy file cards stay readable and read-only. Pause executors and external writers, stop the server, and run `kander-server maintain --data <data> --project <name>`. If active file cards or a pending active migration exist, the command refuses until `--maintenance` acknowledges the write stop. A serving instance holds the same exclusive data-directory lock, so maintenance cannot run beside it. This command never becomes ready or serves requests.

Maintenance reuses the board migration redo journal, checks the final board and returns JSON with `project`, `changed_tasks`, and the final `snapshot` digest. An interrupted migration resumes from the exact prepared mapping and original operation ID. Original import identity, the source backup and historical report bytes remain unchanged. Repeating a completed migration reports zero changes. Resume writers only after success and a fresh service snapshot. This operation does not prune historical evidence. Normal startup recovers ordinary transactions but refuses an active pending form migration until explicit offline maintenance acknowledges the paused writers.

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
| `POST /projects/{p}/tasks/{t}/deliveries`, `GET .../deliveries/{sha}` | Authenticated publication facts for a commit available from the common remote |
| `GET .../tasks/{t}/dispatches/{id}`, `POST .../dispatches/actions/{action}` | Controlled dispatch preparation, attempts, reauthorization, terminal decisions, settlement and wrap-up grants |
| `POST /projects/{p}/tasks/{t}/move` | State moves other than claims |
| `POST /projects/{p}/tasks/{t}/claim`, `/takeover`, `/renew` | Ownership |
| `GET`, `POST /projects/{p}/tasks/{t}/requests`, `POST .../requests/{r}/answer` | Collaboration requests |
| `POST /projects/{p}/tasks/{t}/execution` | Launcher facts of the holder: window, session, confirm, rollback |
| `POST /projects/{p}/tasks/{t}/observation` | Generation-bound agent presence, independent of worker liveness |
| `POST /projects/{p}/snapshot`, `/check` | Coordinated read snapshots and server-side structural/evidence checks |
| `POST /projects/{p}/reviews/plan`, `POST .../reviews/batches/{b}/aggregate`, `GET .../reviews/batches/{b}/view`, `POST .../reviews/close`, `GET .../reviews/progress/{t}` | Review gate evidence |
| `GET /projects/{p}/groups/{g}`, `POST .../claim`, `/renew`, `/observation`, `/release` | Private coordinator lease and its public checkpoint |
| `POST /projects/{p}/groups/{g}/facts`, `/reconcile`, `/recover` | Controlled coordinator producers and client Git verdicts |
| `POST /projects/{p}/issues/results/view`, `/save` | Completed work-set evidence and versioned result publication/decision history |
| `GET /projects/{p}/events?cursor=&wait=` | Long-polled change log |

Errors are JSON `{code, message, board_code}` with stable codes: `invalid_request`, `not_found`, `revision_conflict`, `held_by_other`, `lease_invalid`, `lease_active`, `claim_required`, `evidence_incomplete`, `protocol_incompatible`, `idempotency_mismatch`, `project_mismatch`, `project_not_empty`, `board_rejected`, `not_ready`, `internal`. `board_code` carries the board package message key of a domain rejection so the client renders the same localized text as v1.

### Idempotency

Every write carries an `Idempotency-Key`. The server writes a pending receipt before the operation. Card updates, moves, launcher facts, claims and takeovers stage the completed receipt in the same board redo transaction as the card and claim. A successful board recovery therefore also recovers the exact answer. Review producers, runtime finalization and collaboration requests also stage their exact result receipts in the domain transaction. Dispatch and coordinator producers also stage their exact receipts in the same domain transaction. The same key with the same request returns the stored answer; the same key with a different request is refused with `idempotency_mismatch`. A pending receipt (a crash between the operation and its answer) makes the retry inspect the current state for the operation's effect first and answer from it; the operation runs again only when its effect is not visible. Internal failures stay pending so a retry re-inspects. The client retries transport failures and `not_ready` with the same key.

### Documents

`PUT .../documents/{path}` carries the full text, its SHA-256, the expected card revision, and an optional contract decision. The server checks the digest of the received bytes before publishing, so an interrupted upload never becomes a visible partial document. Publication goes through `board.UpdateDocument`, which keeps every v1 rule: frozen contract fields, managed fields, managed paths, line endings.

## Claims and leases

A claim is the server-side ownership of one task by one execution, kept in `board/.kander/groups/00000000-service-group/claims/<task>.json` outside the card:

- `client_id` (the persistent host identity), `execution_id` (one execution on that host), `owner` (the agent), `generation` (monotonic per task, distinct from the dispatch execution epoch), the lease expiry, and a token.
- The token is an HMAC of the project, task, execution, and generation under the server secret. The server stores only its hash; a replayed claim receipt derives it again, so the token is never written to disk or to a log.
- `POST .../claim` moves a `todo` card into `working` with the owner and grants generation `n+1` in one step under the project lock. A plain move into `working` is refused with `claim_required`; `review -> working` stays a move of the live holder.
- `POST .../renew` with the claim headers (`Kander-Claim-Token`, `Kander-Claim-Generation`) extends the lease by the TTL. A renewal writes no card, so it never changes the card revision. Default TTL 90 s and renewal interval 15 s; the server announces both in the grant and decides time with its own clock.
- A lease past its expiry reads as `lost`. It is never renewed again and the task is never reassigned automatically: `POST .../takeover` with a decision reference and the generation the caller observed hands the task to a new execution with generation `n+1`, appends a handoff record, and supersedes the open requests of older generations. In the same project lock the card's execution ledger hands off (`board.BeginTakeover`): `OWNER`, `SESSION`, and `WINDOW` name the new execution, the old record is retired as handed off, and the new executor reports and confirms its start through `POST .../execution` like a start. A live lease refuses a foreign takeover with `lease_active`. Its current client can perform an explicit owned handoff with the old private proof, exact card revision and a service-clock ticket around a fresh stopped observation. A claimed card without any claim record (a migrated board) can be taken over from generation 0.
- Every write of a `working` or `review` card (document, move, request answer) checks inside the project lock that the presented token and generation are the live claim; another token is `held_by_other`, a stale generation or an expired lease is `lease_invalid`. Card updates and moves additionally run the ownership check under the board transaction locks. Their receipts and any claim release are staged in that same redo record. Server read routes share the project lock, so a snapshot cannot mix a newly committed card with an older claim. A presented execution credential is also checked when its card is no longer claimed; a released execution cannot publish a late result to a todo card. A user-authorized `archived`/`trash` termination may end a task whose lease is lost; a live lease still refuses it.
- A move into `done`, `archived`, or `trash` stages the claim release in the card transaction. Startup recovers the whole redo record before serving reads. It also reconciles pending records left by the earlier prototype.

A claim and its receipt are staged with the todo-to-working move and execution ledger. A takeover similarly stages the next claim generation, handoff and receipt with the new execution record. The transaction extension declares the service control lock before taking task locks. Startup retains recovery of legacy pending claims: a committed working card with that owner activates the claim; an uncommitted move releases it. Old executions therefore cannot overwrite a newer one: their token hash and generation no longer match, whatever order their late renewals, writes, rollbacks, or queued submissions arrive in.

## Starting an agent

`kander start` in server mode claims the `todo` card with a start input (initial session, window, resolved pins); the server runs `board.BeginTaskStart`, so the move, the start metadata, and the pending execution record land in one board transaction, and it keeps the launcher's board cursor in memory for the holder (a restart rebinds a cursor from the current execution record). The client then launches the agent on its own host with a fresh `KANDER_EXECUTION_ID` (in the environment of a spawned process, as an `env NAME=value` prefix of a POSIX container command, or a PowerShell assignment), reports `WINDOW` and the discovered session through `POST .../execution`, and confirms the start. A launch failure posts `rollback`: the server restores the card text from before the claim, returns it to `todo`, and releases the claim in the same transaction. Confirmation clears rollback authority in that transaction as well. The spawned process PID, kernel start token and boot identity are stored in the credential. Container launches retain their local window and resolved session there as well. Concurrent renewals merge these facts rather than erasing them.

The claim also persists the launch record ID, the launcher's last content revision and a monotonic executor-evidence flag. `GET .../execution` requires the current holder and returns the execution record and launch standing. After a server restart it reconstructs only the exact saved launcher cursor. Executor progress remains evidence even after subsequent launcher window/session writes; a rollback rechecks that evidence under the mutation transaction. A missing prototype cursor is treated conservatively as evidence.

Remote launches reserve uncertain delivery by the existing durable launch path. A failed command after it may have started the agent preserves the pane, task file, credential and executor updates, and records `unresolved` when the service is available. Failed or interrupted service reads preserve the executor. A superseded launch touches no successor. A repeated `start` cannot relaunch the already claimed task. Task launch intents now freeze the execution identity, claim body, idempotency key and private predecessor proof before calling the service. Recovery reuses the original request and checks the current execution instead of starting another agent. If the journal proves no physical launch boundary was crossed, recovery rolls back an initial claim or audit-releases a failed takeover generation. Otherwise it restores the private credential and retains the unresolved execution for inspection. A completed start is never physically replayed.

`resume --adopt --decision <reference>` uses the current host's private task credential and exact local process incarnation or terminal session. It does not read a local card path or probe a foreign pane. Adoption binds an explicit decision and the unchanged card revision to a short-lived HMAC observation ticket issued by the server before the local probe. The ticket uses the service clock and generation, so a client's wall-clock offset neither grants freshness nor prevents valid observation. Unknown local identity refuses adoption. `resume --agent <agent> --decision <reference>` starts a fresh generation from the latest verified shared delivery. A same-client owned restart additionally proves the old executor has stopped. `resume` without an agent queues collaboration while the agent is alive or unknown; a locally confirmed stopped executor resumes its native session through a new generation. A group review card still needs a durable dispatch except during unfinished-start recovery.

For Git-backed projects, start publishes an explicitly labelled `baseline` commit to its immutable generation reference, prepares a fresh private worktree/branch `kander/<project>/<task>/g<generation>`, and records that branch through the controlled execution producer before launching the agent. A baseline carries no completion authority. A working-to-review move needs a separate `delivery` attestation of the implementation's current content revision. The CLI and TUI publish the managed worktree's HEAD even when the invoking view runs in the main checkout.

Worktree intent files live in the client's project-private `worktree-intents/`; the worktrees live in `worktrees/<task>/g<generation>`. Creation records intent first, then reconciles Git's actual worktree registration, common directory, branch and base ancestry. A same-generation retry reuses the exact workspace and preserves dirty content. Partial or conflicting Git state stops with the intent path and retains every artifact. A later generation has a different branch and directory. An independent-clone takeover test verifies the successor starts from the fetched commit while the predecessor's uncommitted changes remain intact. Both fresh takeover and native continuation use this provisioning. Before native continuation, the client freezes the index patch, working-tree patch and non-ignored untracked regular files into a private snapshot. It verifies the stopped source again before handoff and restores into the new generation only. Ignored files remain in the predecessor workspace. Concurrent edits, oversized snapshots, untracked links or a partial restore stop with both worktrees retained. Restore records intent first and only recognizes an exact completed snapshot on retry; it never reapplies patches over changed content.

The private credential registry retains a monotonic generation head even after release. Delayed initial saves cannot replace a newer interactive credential or revive an older pointer after the successor exits. Bound processes still read only their own execution record; they never borrow the current head's token. Concurrent worker writes merge the immutable worktree, branch and base facts with process/session observations.

## Review gate

The client runs every Git check of `kander review plan`, `aggregate`, `close`, and `progress` against its own repository, exactly as v1 does, and sends the board request together with the Git facts. The server revalidates structure and bindings with the board package and publishes atomically; it never runs Git. A single-card mutation supplies that card's claim proof. Multi-card operations first prepare immutable material and an exact revision/generation member set at `/projects/{p}/publications`. Each holder confirms its member through `POST .../publications/{id}/confirm`; partial confirmations survive restart. The coordinator then supplies `Kander-Publication` to publish the confirmed operation. The transaction checks every current lease and version, rejects changed membership and writes outside the confirmed set, and commits all member copies, the publication receipt and the operation result together. A completed publication returns its saved result on retry.

The client auto-confirms locally owned members and creates generation-bound collaboration requests for other holders. They inspect and confirm with `kander review confirm <absolute-CWD> <publication-id> <task-id>`. The `Kander-Claims` header supports a separate proof per task; one task's token is never authority for another. Author dispositions require only their assigned card's holder, even when the run has several members.

`extend-plan`, `advance`, `assign`, `disposition`, `interpret`, and `map-legacy` have controlled service producers. New server plans use `project:<id>` as their evidence identity rather than a client's absolute CWD. Clients still verify Git in their own repositories. Historical unplanned-batch resolution has a read-only facts producer that freezes plan/batch revisions, original report hashes, membership, evidence identity and ancestry edges. The client obtains required commits from the common remote and verifies every edge locally. Resolution rechecks those exact originals inside its transaction, applies current-holder or joint authority, and commits the audited plan resolution and exact receipt together. Migrated evidence keeps its historical CWD bytes; the client repository used for Git verification is separate. Reviewer invocations use the service for their frozen inputs, runtime phases, prompt artifacts, final originals and publication. `POST .../reviews/runtime/prepare` freezes a private uploader hash and the member generations. A prepared identity never authorizes another reviewer launch, including a retry of a successful joint preparation. Phase and artifact uploads require that private writer and the still-live generations; a takeover fences them independently of card content changes. Audited member exclusions remove only that member from writer validation; remaining generations must still be live, and publication still obtains current-member authority. Runtime uploads revalidate the writer before committing their receipts.

`POST .../reviews/runtime/{run}/finalize` validates every uploaded file digest and allowed filename, then commits all output bytes, immutable originals and its receipt together. A client saves the exact pending finalization in its private runtime directory before transport. After an outage, a retry uploads that saved result without rerunning the reviewer. An interrupted invocation without a completed saved result records interrupted facts. Formal run publication uses fresh holder confirmations and `PublishReviewRunAtomic`, committing all member copies and indexes together. Runtime output and writer secrets never create a local board. The client prints each answer through its board type, so the aggregate bytes a close request hashes are the same as in v1.

## Git delivery and dispatch

For a project registered with an origin, the normal CLI and TUI working-to-review move first publishes the chosen full SHA (HEAD when omitted). The client verifies the registered origin and its push URL, pushes only `refs/heads/kander/<project>/<task>/g<generation>/<commit>`, and rereads that exact remote ref. A retry inspects the ref before pushing and reconciles even a nonzero push result against the remote facts. This never advances the integration branch or force-rewrites another execution's reference. Shared remote identities refuse embedded passwords, HTTP userinfo, query tokens, ambiguous origins and executable Git helpers.

`POST .../deliveries` authenticates the holder and checks the execution, generation and inspected card revision under the same transaction as the publication receipt. The service records an authenticated client's transport observation; it does not independently query Git. The state-changing transaction checks those bindings again. A content edit or takeover invalidates an earlier attestation. Another clone fetches the recorded reference and confirms it still names the expected SHA before accepting the local object. Missing remote refs block it even when that SHA is cached locally. Review invocations fetch their exact base and target through the registered remote before launching a reviewer.

Dispatch lease generations and business epochs fence separate authorities. Preparation, attempt reservation, explicit terminal decisions, completed settlement and wrap-up grants reuse the board's existing evidence producers through REST. Their card changes and exact result receipts commit together. A dispatch is never accepted merely because a transport succeeded. New wrap-up evidence stores `project:<id>` while local verification uses the caller's checkout; historical CWD bytes remain unchanged. Explicit target-integration authority is described in [Distributed Git integration](distributed-integration.md).

A foreign `dispatch prepare` becomes a generation- and card-version-bound collaboration proposal with the original typed input. It returns a request object with `status=open`, not a prepared dispatch. The holder inspects it using `kander request show <task-id> <request-id> --json`. Only the live holder can prepare or settle formal dispatch evidence. Requests remain proposals until the holder takes a controlled action.

An explicit takeover of a bound task names the inspected dispatch ID, epoch and revision. It retains an unfinished binding without authorizing business replay, or settles a completed binding in the same transaction as the new lease and execution handoff. This avoids requiring an expired execution to settle before its successor can take over. An old execution identity cannot reacquire its own lost lease. Fresh stopped evidence is still required for accepted-round reauthorization; lease expiry alone proves no agent exit.

## Coordinator authority

A group's public checkpoint `authority.token` is a session label, not a bearer credential. The service keeps a separate private group lease in `group-claims/`. Concurrent claims have one winner; a live lease cannot be preempted. An explicit claim after expiry or a reported coordinator exit requires the observed generation and a decision, advances both lease ownership and checkpoint authority atomically, and records the handoff. Reusing the old execution ID cannot revive its lease. Checkpoint epochs and lease generations remain separate fields, including after migration.

`kander coordinator claim` stores the returned private credential and starts the same background worker used by tasks. Group renewals change no card or checkpoint version. Every checkpoint mutation checks that private lease inside its producer transaction. A recovery of a live member additionally requires that member's private credential and a fresh service-clock ticket around a confirmed local stop. Explicit recovery after lease loss uses the observed task revision and generation, the current private coordinator lease and the user decision. It atomically resets the business cycle, supersedes old requests and records a released audit generation; it neither starts an agent nor grants runnable ownership. The old scene remains recorded and its unprobed presence stays unknown. A pending task start or active dispatch must settle before recovery. Target-branch integration has a separate private lease.

For reconcile, the client first reads the exact Git facts the producer needs, verifies them in its own clone, and sends the facts with its verdicts. The server repeats the producer pass and accepts only a verdict for the exact current facts. Missing or changed Git facts block the member instead of manufacturing delivery. The server never executes Git. Whole-request CAS failures preserve the checkpoint; per-member failures retain their blocked mirrors and the existing partial/failed CLI exit statuses.

Distributed `orchestrate` reads cards and expands groups through a service snapshot. Its task file carries a fresh fixed execution identity in both `KANDER_COORDINATOR_EXECUTION_ID` and `KANDER_EXECUTION_ID`. Its PID incarnation or window/session is retained below the private client `sessions/` directory before later group claims. A group credential binds to that context, so the worker can observe coordinator exit independently of TUI lifetime. A separate task executor clears inherited coordinator authority and receives its own task execution ID. Neither orchestration nor checkpoint claim creates a local board or mutates member cards.

## Client and worker

The client reads `server.url` and `project.name` from the scope config or the project overlay `.kander-config.json`. The client always requires this binding. Use `kander init --server http://127.0.0.1:7878 --project <name>` in each checkout after starting the server. `init` validates protocol and canonical origin before saving only those fields to the project overlay. An explicit project-path argument targets that checkout. Missing configuration or an unreachable service fails without a local board fallback.

`list`, `show`, `new`, `update`, `move`, `pick`, `start`, `dispatch`, `notify`, `resume` (including `--adopt` and `--release`), `dismiss`, issue import/triage, the TUI board and start actions, `check`, `subscribe`, and the review gate commands above use the server project. Issue result reconciliation also uses the service; the remaining provider effects keep durable intents and central CAS receipts. Board-free orchestrators also keep private launch intents. A completed logical retry returns its recorded result, and an uncertain retry reports the retained receipt without creating another terminal. `orchestrate --operation-id <stable-id>` distinguishes an intentionally new confirmed session; a new ID does not settle any prior uncertain effect. The client directory (`~/.local/share/kander/client/`, or `KANDER_CLIENT_HOME`) is private to the user and never part of Git:

- `client-id`: the persistent host identity, created on first use.
- `executions/<project>/<task>/<execution>.json`: the credential of each claim this host made (server URL, generation, token, lease timings, the managed agent PID when known), plus `latest`. Per-execution locks merge renewals and launch facts; release tombstones prevent a late writer from recreating a removed credential or erasing a successor pointer.
- `worker.lock`, `worker.log`: the background worker.

A process started for one execution carries `KANDER_EXECUTION_ID` and presents only that execution's credential, so an agent of an older generation cannot pick up a newer claim made on the same host. An interactive command without the variable uses the task's latest credential. `move <task> working --owner <agent>` claims a `todo` task; with `--decision` it takes over a lost claim.

After a claim the client starts `kander worker` detached unless one already holds `worker.lock`. One worker runs per client home, independent of the TUI and of the command that started it, and exits after two minutes without a live claim. On each tick it renews every credential whose renewal interval passed:

- A successful renewal records the local receipt time rather than comparing a client wall clock to the server timestamp. The worker observes the managed process incarnation or local terminal session separately. A confirmed exit is reported through `/observation` without changing the card revision. A healthy worker continues renewing working/review ownership after the agent exits; agent presence and task ownership are separate. `resume <task> --release --decision <reference>` explicitly releases a locally confirmed stopped executor using a fresh service-clock ticket, exact version and holder proof. Release records an audit event and makes the task lost for an explicit successor; it never reassigns automatically. Missing identity, observation failure, waiting for input, and lack of output never prove an exit. Public claim views retain the generation-bound observation.
- A refusal of a claim the server shows as released (the card reached `done`, `archived`, or `trash`) removes the credential.
- Any other refusal (taken over, expired, unknown) persists the lost state and a target-specific stop intent before attempting to stop the agent. Linux pins a pidfd and Windows verifies and terminates through one process handle; a reused PID never receives that signal. Other platforms refuse unpinned process termination and record the limitation. Terminal stops revalidate the locally recorded session, use observed process identity when supported or the configured agent exit command, and retain the container. Missing or uncertain identity records an unresolved stop; the service still refuses every later write of that generation. A later launch observation can trigger one stop attempt for its newly known target, but a worker restart never blindly repeats an uncertain external effect. Worktrees, branches and uncommitted changes stay untouched.
- A transport failure keeps retrying until a full TTL has passed since the last confirmed renewal; then the worker assumes the lease is gone and handles it as lost, because the server clock has already expired it.

## Collaboration requests

A non-holder asks the holder to act through `POST .../requests` with a kind (`fix`, `sync`, `review`, `stop`), a message, and the generation it targets; a request for a generation that is not current is refused. Only the live holder of that generation answers it (`accepted`, `done`, `rejected`). A takeover marks the open requests of older generations `superseded` instead of handing them to the new execution. `kander request <task> fix|sync|review|stop <message>` asks, `kander request list <task>` lists, and `kander request answer <task> <id> accepted|done|rejected [note]` answers with this host's execution credential. On each successful renewal the worker reads open requests for its generation. It first reserves delivery with an accepted answer and persists the request ID locally, then types one sanitized line into the matching local session. A failed or interrupted terminal delivery remains explicitly uncertain and is not automatically typed again. The holder can inspect the request and execution facts. Answer revisions support CAS; settled answers cannot regress to accepted. Request state, answer receipts and takeover supersession share the board redo journal without changing card content versions. A card without a usable terminal target keeps its request for manual inspection.

`kander notify` submits a request to the owning worker through REST. Plain working messages remain requests. Dispatch options add the full typed proposal and inspected card revision; a group review card still requires them. The JSON result describes the request's queue/answer state, never asserts dispatch business acceptance. A pane override is accepted only when it names the exact private execution on this host and never probes a foreign terminal. The holder can inspect every full message with `kander request show <task> <request> --json`.

A dispatch proposal ID identifies one immutable payload. Repeating a known ID reuses its original version and deadline; changing material requires a new ID. A relative acceptance window is stamped by the service, is bounded to 24 hours, and cannot be reset by a retry. The worker refuses stale or expired proposals before terminal effects. Reserving their first delivery checks the card revision, deadline, request revision and live generation in one transaction. Formal dispatch preparation checks the same frozen material and version, then stages its proposal acknowledgement with the dispatch and its exact receipt. Existing formal dispatch retries remain receipt-only reads.

The client keeps private request-operation receipts under `request-calls/` before transport. An uncertain response retains the original operation key and target generation; a restarted command inspects that exact request before resubmitting the saved domain operation. It never builds a new request for a successor from the old uncertainty. A successful response or matching central facts completes the private receipt. Unknown outcomes preserve it for inspection; known rejection permits a later deliberate request. This is domain recovery, not a claim that terminal effects are exactly once. The worker separately reserves each terminal attempt before typing and never automatically repeats an uncertain attempt.

Worker delivery requires a usable local target and refuses server routing that differs from this execution's saved window/session. It strips control characters and Unicode line separators, links the full request for inspection, warns the agent to reject another generation or a superseded request, and ends with the card language reminder.

Event cursors resume within a running server's bounded window. A service restart always changes the epoch and requires a full snapshot, including when a crash occurred after a domain commit but before event append. An append failure also rotates the epoch and wakes observers; missing invalidation hints never silently preserve a current-looking cache.

## Client views and issue import

`internal/remote.ProjectService` is the typed REST boundary shared by client commands and views. It has no local board implementation. The TUI binds its board, detail, action and start callbacks to that service on every normal launch. A captured task-action revision fences the move at the server; a stale dialog cannot overwrite a newer task.

The TUI writes only a private list cache under the client home, keyed by the server URL and project ID. A transport failure may display that cache with a persistent stale label and its observation time. It never turns the cache into a writable board or queues an offline move. Detail reads and mutations still require the server. The executing worker is independent of the view lifetime.

The TUI keeps one bounded event poll from its last successful snapshot cursor. A change or reset wakes the asynchronous snapshot reader; it adopts the next snapshot cursor only after that read completes. The poll does not occupy pending terminal work. Error backoff survives successful list reads, stale caches use bounded snapshot retries, and quitting cancels only view reads. Periodic refresh remains a fallback for lease expiry and read recovery; see [TUI board refresh](tui-board-refresh.md#distributed-event-wakeups).

`kander list --json` keeps the schema-1 CLI snapshot consumed by dsh and adds project ID, event cursor and public claim observations. The change token ignores renewal timestamps and pure observation timestamps. Local terminal observation and focus require this host's exact saved execution, generation, session and window; a public client ID alone grants no routing. Foreign tasks display their owning worker's service observation; their terminal addresses never reach this host's backends. Lease loss remains visible but never invents evidence of process exit. Observation and renewal timestamps are excluded from the change token.

`subscribe` expands membership from a coordinated `/snapshot` response and resumes `/events` from that snapshot's cursor. Its in-memory board view contains resource identities and no write cursor; it creates no local card directories. Group expansion, task revisions, dispatch summaries and blocked members come from the service. Public liveness uses generation-bound worker observations and translates service-clock age for local display. Cursor resets trigger a fresh snapshot and an explicit `snapshot-reset` event. Claim generation/status changes emit `claim-change`. Group checkpoint or authority changes emit `coordinator-change` with public coordinator summaries; ordinary renewal timestamps do not. The bounded output worker cancels an outstanding event poll when output closes.

The ordinary `check` route performs the board's structural, prerequisite and review-evidence checks on the server and appends the reported agent observations on the client. Delivery and overlap checks continue to inspect local Git without a board.

dsh resolves the Git main worktree or a non-Git project's configuration directory, without requiring `kanban/`. Its subprocess environment removes inherited `KANBAN_DIR`; kander resolves the server binding from the repository and configuration. dsh keeps its existing schema-1 list consumer and asynchronous start boundary.

`POST /projects/{p}/issues/import` receives a bounded source snapshot, type choice, language and size. The server validates the canonical repository and issue identity, sanitizes the source again, authors the fixed contract itself, and reuses the exclusive-board source-key transaction. It never accepts client-authored contract sections or puts issue bodies/comments into them. Concurrent imports return one anchor with both source attachments. `GET .../issues/index` returns canonical bindings with resource locations instead of server filesystem paths.

Issue triage and refinement run on the client. Their refreshed or downloaded source evidence lives in the client's project-private data directory. The launch request distinguishes that evidence directory from the local Git repository; no local card-state directories are created. Result inspection, publication, consent and recovery use the independent central CAS described in [Distributed Issue results](distributed-issue-results.md).

## Events

Each project keeps `control/events.jsonl`. Events carry identities (`task_changed`, `claim_changed`, `request_changed`, `group_changed` with task or group, state, revision, generation), never bodies. A cursor is `<epoch>:<seq>`; the task list returns the cursor it is consistent with. `GET .../events?cursor=&wait=` long-polls up to 60 s. An unknown epoch, a future sequence, or a cursor older than the retained window answers `reset`; the client re-reads the task list and continues from its cursor. The TUI reads server snapshots and keeps a private read-only offline snapshot. `subscribe` resumes the stream and refreshes after a cursor reset. Event-driven TUI wakeups remain **planned**; its current refresh reads snapshots.

## Logging

The server logs JSON lines with method, path, status, client ID, operation ID (derived from the idempotency key), claim generation, and duration; claim transitions log project, task, and generation. Claim tokens are never logged.

## Migrating a v1 board

`kander-server import [--data DIR] --project NAME [--remote URL] --from KANBAN_DIR` imports one v1 board into a project. It takes the data directory lock, so it refuses to run while the server serves that directory. Before running it, stop the v1 executing agents and other writers of the source board, as for `kander init --maintenance`.

1. It refuses overlapping source/data paths, recovers the source board's interrupted transactions, then holds the board-exclusive lock while copying a consistent snapshot. Every regular file is digested by path and SHA-256. A symlink, junction, or other entry refuses the import. A second source digest detects non-cooperating writers before publication.
2. The project must have no tasks. Importing the same snapshot into the same project again answers with the earlier import; any other import into a project with tasks is refused with `project_not_empty`. Two existing boards are never merged.
3. Every file is copied byte for byte into a staging directory beside the project board. The staged snapshot must equal the source snapshot and pass the full structural, dependency, contract and immutable-evidence checks. An unresolved historical execution batch may remain pending business work; migration grants no closure or completion and still rejects damaged originals. New empty validation locks are removed before the exact snapshot is published.
4. Only then is `control/import-pending.json` committed. The old empty destination is renamed to a private backup, staging is renamed into place, and `control/import.json` records the exact snapshot before recovery material is removed. Startup completes an interrupted publication before board initialization and readiness. Missing, conflicting or changed prepared material refuses startup; it is never replaced with an empty board.

The source board is never changed and stays as the local backup; review originals, journals, and their hashes keep their bytes. Claimed cards of the old board have no claim record, read as generation 0, and need an explicit takeover. Afterwards each clone sets `server.url` and `project.name` (for example in `.kander-config.json`) and stops using the local `kanban/`.

## Build and release

`make build` (and `make-windows.cmd`) builds `kander` and `kander-server`. The release workflow packages each binary for every platform in its own archive (`kander-<os>-<arch>` and `kander-server-<os>-<arch>`), because the client self-updater accepts an archive with exactly one `kander` entry. A tag with a suffix, such as `v2.0.0-alpha.N`, is published as a GitHub prerelease that is never marked latest and never syncs the Homebrew formula; the MSIX job already skips suffixed tags. The update check reads `releases/latest` and refuses prerelease and suffixed versions, so a v1 stable client never installs a v2 prerelease, and a v2 prerelease client is never offered the older v1 stable release.

Run the server with `kander-server --data <dir> --listen <host:port>` (defaults: `$XDG_DATA_HOME/kander-server` or `~/.local/share/kander-server`, `127.0.0.1:7878`); `--ttl` and `--renew` set the lease timings. Health: `GET /api/v1/health`, readiness: `GET /api/v1/ready`, protocol: `GET /api/v1/version`.

## Planned stages

1. Branch conventions, the wire contract, the server (this document).
2. Client configuration (`server.url`, `project.name`, the persistent client ID), the background worker (renewals, lease loss handling, request delivery), and the single-card loop over REST.
3. Review runs, dispatch, task groups, issue import, TUI, `subscribe`, and dsh over REST; joint review publication with holder confirmations.
4. Explicit migration of a v1 board into an empty project, two-binary build, install, update, and `v2.0.0-alpha.N` prereleases.

## Retained local execution routing

After task completion or explicit release, the client removes the bearer credential and leaves a release tombstone. A separate private `retained-executions/` record keeps the final window, session, worktree, branch and base without a token. A delayed removal cannot erase those latest facts. Remote `dismiss` reads final state and the execution record through REST, then uses only the owning host's exact retained routing. It rechecks revision/generation and pane identity before its private exit intent. It never reverse-searches a stale pane. Completed cleanup replays its receipt; an uncertain cleanup recognizes an already absent target but never blindly repeats an exit or close. Worktrees remain intact.

## Joint material retry

A joint operation privately freezes its verified material against the operation and each member's current revision/generation. For closure and historical resolution, a retry may recheck identical immutable Git facts at another instant; the first verified payload, including its original timestamp, remains the material holders confirm. The REST client sends that exact frozen payload with its publication identity. A changed revision, generation or substantive fact produces a different proposal and needs new confirmation. This prevents a fresh verification timestamp from abandoning partial approvals on every retry.

The private review operation index also remembers prepared publication identities before sending them. Once one of them is published, a retry returns that exact frozen result before considering versions advanced by the publication itself or by later exclusions/takeovers. The service validates the operation and material digest, then replays the immutable result without executing a producer or requiring old holders again. Prepared publications still require the full current member/version/generation checks. This receipt path cannot grant a new write.
