# kander coordinator

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" (syntax) and `KANDER-KANBAN-RULES.md` "Coordinator Checkpoints" (semantics)

## Purpose and users

Keeps durable group checkpoints for the orchestrator: `claim` fences one coordinator, `reconcile` records every observation, `show` reads the committed checkpoint. `recover` is the audited last resort for a blocked member; it runs only on an explicit user decision. `remove-member` takes one unstarted member out of the group on an explicit user decision. The JSON shapes and the recovery protocol are in `snippets/coordinator-checkpoints.md` "Coordinator Checkpoints" and `snippets/orchestrate-start.md` "Durable Coordinator Recovery".

## Syntax

```text
kander coordinator show <group-id>
kander coordinator claim <absolute-claim.json>
kander coordinator reconcile <absolute-observations.json>
kander coordinator recover <absolute-request.json>
kander coordinator remove-member <absolute-request.json>
```

## Arguments

- `show <group-id>`: print the committed checkpoint.
- `claim <absolute-claim.json>`: group_id, expected_revision, expected_epoch, owner, unique token, basis, and the complete member list.
- `reconcile <absolute-observations.json>`: group_id, expected_revision, the authority from the claim, a members object keyed by task ID (each with its card revision and, when bound, dispatch_id, epoch, base, and completed delivery_commit), and an optional absolute `cwd` for read-only Git verification.
- `recover <absolute-request.json>`: group_id, member, expected_revision, expected_epoch, decision (free text that cites the user's decision; not the ID of a `kander decision` record), and reason. All six are required. The member's column selects the mode.
- `remove-member <absolute-request.json>`: the same six fields. `member` is the card that leaves the group.
- `release --end-execution <absolute-release.json>`: `group_id`, `reason`, and `decision`. `--end-execution` confirms that the release is final for this coordinator execution; it belongs to `release` only and comes before the path.
- `integrate <absolute-integration.json>`: integrate a reviewed commit into its target branch; the input fields and the containment rules are under "Additional Syntax".
- `integration-show <target-ref>`: print the public integration record of a target; `<target-ref>` is a full branch ref such as `refs/heads/develop`.

## Examples

```text
# Read the checkpoint of a group
kander coordinator show 20261118-example-group

# Claim a group as its coordinator
kander coordinator claim /tmp/claim.json

# Record the observations of the members
kander coordinator reconcile /tmp/observations.json

# Recover a blocked member after a user decision
kander coordinator recover /tmp/recover.json

# Remove an unstarted member from the group
kander coordinator remove-member /tmp/remove-member.json

# End the coordinator execution of a group; the release is final
kander coordinator release --end-execution /tmp/release.json

# Integrate a reviewed commit into its target
kander coordinator integrate /tmp/integration.json

# Show the integration state of a target ref
kander coordinator integration-show refs/heads/develop
```

## Preconditions

- A located board; the complete member set; a successful claim before any reconcile. A first unbound delivery needs the card in `review/`, its `TASK_BRANCH` recorded as the bare branch name, and the absolute `cwd` of a repository where that branch head equals the delivery commit. That `TASK_BRANCH` must not equal the card's `INTEGRATION_TARGET`.

- `recover` needs the member blocked in the checkpoint, and the current checkpoint revision and coordinator epoch. Run it only on a user decision: the orchestrator never recovers on its own, and Kander records the decision reference without verifying it.

- `remove-member` needs the current checkpoint revision and coordinator epoch, and, in a distributed project, the current private coordinator lease of the group. Run it only on a user decision, as for `recover`. The member must be unstarted: in `backlog/` or `todo/`, with no `STARTED_AT`, a checkpoint mirror still awaiting its start, no execution record or start attempt, no service claim record, no dispatch binding, no review run or plan, and no member exclusion. It also needs at least one other member to stay, and no remaining member may list it as a card ID in `PREREQUISITES` (that would hold the whole group for a card outside it). Its own `PREREQUISITES` may name members that stay: the card keeps that line and, as a standalone card, waits for them as for any other dependency.

## Effects

- `show`, `claim`, and `reconcile` store or read a checkpoint through the board transaction protocol. They never notify, move cards, integrate Git, or grant execution authority.
- In distributed mode, a `reconcile` whose result is `complete` then runs the completion cleanup of the group's Git evidence refs when the group is finished (`snippets/commands/dispatch.md` "Distributed wrap-up evidence"). A cleanup that fails only prints a warning; `result` and the exit status stay.
- `reconcile` judges each member alone. A member that fails stays at its last mirror with `blocked.reason`, and the other members advance. Each reconcile judges a blocked member again and clears the field when the member passes, so a stale observation ends with the next correct one. While any member is blocked, every wrap-up dispatch of the group is refused.
- A member whose execution cycle changed with no handoff record (a reclaim without a start attempt or a `LIFECYCLE_DECISION` handoff) never passes a later reconcile. Its `blocked.reason` keeps `execution cycle changed without lifecycle handoff` and adds the next step: ask the user first, and run `recover` only after the user decides (`snippets/failure-recovery.md` "Blocked Group Members").
- `recover` on a `working/` or `review/` member is a reset: it writes an execution record and a transition of kind `recover`, a new claim that changes the execution cycle, and moves a `review/` card to `working/`. The checkpoint mirror rebinds to the ledger. The old cycle's review evidence no longer satisfies the done gate, so the plan needs `rebind_cycles`. Then every role the plan required in any batch needs an actual review in a batch that closes after the recovery. A later reclaim does not end this requirement. The next step is `kander resume --agent <agent>`; plain `resume`, `notify`, and `dismiss` refuse the card.
- Before a reset, `recover` refuses a member whose current execution record is `pending`. An `unresolved` record has no such check; its reset follows the probe. The refusal names adoption only when it can work: for an old card shape, or an unmarked pane whose marker adoption cannot write (`snippets/commands/resume.md` "Adopting an Unfinished Start"), it names only the takeover, after ending any dispatch binding. Whether adoption can write the marker is judged from static facts only; the refusal runs no session discovery.
- Then `recover` probes the member's executor, and it searches a stale `WINDOW` by session. A Codex card with no session reference is resolved from its rollouts for that search, only when the recorded address is stale. The probe and the resolution stop after 10 seconds together and then read `unknown`. The result decides:

| Probe result | Reset |
| --- | --- |
| `alive`, `alive-orphaned` | refused |
| `drifted` (running at a new address) | refused |
| `unknown` with executor evidence | refused |
| `unknown` from a failed probe | proceeds, because `recover` is the last way out when the terminal cannot be probed |
| `identity-mismatch`, `unprobeable` | proceeds |
| `stopped` | proceeds |

- Executor evidence means that the probe finished and saw an executor it could not prove. There are four cases: a reverse lookup with 2 or more matches; a recorded or found pane whose session identity is uncertain for a reason other than the probe deadline; a recorded or found pane whose agent status cannot be classified; a tmux pane with the agent running and no session marker.
- A refusal of a member whose `unresolved` start has not ended also names `kander resume <task-id> --adopt --decision <reference>` when adoption can end it: the card shows the attempt's `OWNER` and `SESSION`, and the recorded pane reads `alive` or `alive-orphaned`, or is unmarked with a session adoption can mark (`snippets/commands/resume.md` "Adopting an Unfinished Start"). It uses the probe already made and static facts; it runs no session discovery and writes nothing.
- A failed probe means that the terminal is unavailable, the probe reached its deadline, the reverse lookup did not finish, or the session reference could not be resolved. `kander check` still shows both kinds of `unknown` the same way.
- A session with no reference and no way to resolve one (`session.mode=none`) has nothing to search by, so a drifted `WINDOW` then reads `stopped`.
- `recover` on a `done/`, `archived/`, or `trash/` member is a confirmation: it writes only the audit record and rebinds the mirror. It changes no card, execution cycle, or review evidence. Reconcile then accepts that member as it is for this cycle.
- Each recovery writes an immutable record that binds the checkpoint revision and the coordinator epoch. `kander update` cannot reach it, and `show` lists it under `recoveries`.
- `remove-member` is one transaction. The checkpoint drops the member and its revision rises by one; the coordinator epoch and the authority stay. The card's `TASK_GROUP` becomes empty (a group written into `DISCUSSION` by an older card is removed too), and a `CONTRACT_DECISIONS` entry records the decision reference, the old group, and the new empty value, which `kander update` cannot rewrite. An immutable record binds the checkpoint revision and the epoch, and `show` lists it under `removals`. The card is an ordinary standalone card afterwards: it can be picked and started, or grouped into a group that has no checkpoint with `kander update --contract-decision-file`. Later claims and reconciles name only the remaining members. A claim that names the removed card is refused, and so is an earlier claim request replayed byte for byte. Only an unstarted member can leave; a started one is archived, trashed, or recovered. Adding a member to a group that has a checkpoint is not supported.

## Output

- `show`, `claim`, `recover`, and `remove-member`: one JSON object with the checkpoint, its revision, and the authority.
- In a distributed project `show` also prints the optional top-level field `branch_snapshot` (`{branch_mode, policy_revision, scope}`, `scope` `group`) next to the checkpoint fields once the group has a branch snapshot ("Distributed project"); a group first claimed before snapshots existed has none.
- `release`: one JSON object `{claim}`, the group claim after the release (`execution_id`, `generation`, `status`, `owner`, `client_id`, `renewed_at`, `expires_at`). With `--json` the notice `coordinator_release_final` carries the execution ID, the group ID, and the generation. A `release` without `--end-execution`, or with the flag after the path, is a `usage` failure (exit 2) before any request; the message holds the consequences and the confirmed syntax.
- `integrate`, `integration-show`: one JSON object, the public integration record (`id`, `status`, `phase`, `generation`, `request`, `observed_target`, `renewed_at`, `expires_at`); `integrate` prints it when the operation ended.
- `reconcile`: the same object plus `result`. Exit 0 with `complete`: no member is blocked. Exit 3 with `partial`: the checkpoint is committed and some members are blocked. Exit 1 with `failed`: the checkpoint is committed and every member is blocked. Exit 1 with no JSON: the request was refused and the checkpoint is unchanged.
- With and without `--json` the objects are the same. With `--json`, a `reconcile` that blocked every member also reports the blocking error as a notice with the code `reconcile_blocked`; its exit status stays 1 and `result` is `failed`.

## Rejections

- A refusal over a stale revision or epoch of `claim`, `reconcile`, or `recover` keeps its label (`claim CAS or reused token`, `checkpoint CAS`, `stale coordinator authority`, `recover CAS`) as the start of the message and writes the value of the request, the current value, and the next step: read the checkpoint with `kander coordinator show <group>`, confirm that the decision still holds and that the group lease is still yours, and write the request again. A missing checkpoint is refused as `checkpoint missing` and points at `claim`. A different authority names the field that differs: the owner and the epoch with both values, the token only as different. No message writes a token or a claim proof.
- `reconcile` refuses the whole request on a stale card revision (retry with a fresh observation), a stale checkpoint revision or authority, a changed member set, an unknown member, or Git facts that changed during the command. Damaged originals, a failed Git verification, or a wrong dispatch observation block that member instead.
- `recover` refuses a missing field, a stale revision or epoch, a member that is not blocked, and a `backlog/` or `todo/` member. A repeated request fails on the revision, so one recovery wins; after a lost response read `recoveries` with `show`. A reset also refuses a member with a dispatch binding (end it with `dispatch fail|cancel|settle`), a `pending` member, and a member with a running executor or executor evidence. Each error names its evidence and next steps:

  - `pending`: wait while the starter runs. When the executor is alive, use `kander resume <task-id> --adopt --decision <reference>`. When the starter and the executor have ended, use `kander resume <task-id> --agent <agent> --decision <reference>`. A card with a dispatch binding first ends it with `kander dispatch fail|cancel`, or `kander dispatch settle` when the round completed.
  - `alive`, `alive-orphaned`, or `drifted` in `working/`: the error names the address. Use `kander resume --agent <agent>`, or stop the executor at that address and run `recover` again.
  - `alive`, `alive-orphaned`, or `drifted` in `review/`, and `unknown` with evidence at one address: the error names the address. Stop the executor at that address and run `recover` again. A `review/` member takes `resume --agent` only while its start attempt has not ended, and a takeover cannot exit an executor it cannot prove.
  - 2 or more matches: the error names the count. Stop the executors that match this session and run `recover` again.
- `remove-member` refuses a missing field or a blank `decision` or `reason`, a stale revision or epoch, a card that is not in the checkpoint or whose `TASK_GROUP` is another group, a started member (the error names the reason), the last remaining member (archive the card instead), and a card a remaining member lists in `PREREQUISITES` (the error lists those members). In a distributed project it also refuses a caller without a standing group lease (none, released, lost, or taken over) and a card with a service claim record. A repeated request fails on the revision; the same request with the same idempotency key returns the first answer. A refusal writes nothing.
- `release` without `--end-execution` is refused with exit 2 before any request. The claim of a bound coordinator execution that released its lease, lost it, or was taken over is refused with `lease_invalid` after the client read the group and before it sends a claim request. A claim of a lease holder that changed the request, or repeated it after a `remove-member` reset, is refused with `board_rejected`, as above.
- `--end-execution` belongs to `release` only, directly after the subcommand and before the path. It is a usage error anywhere else.
- A wrong argument count, a relative `recover` or `remove-member` path, or an unknown subcommand is a usage error.

## Distributed project

The service owns the checkpoint. Its public authority token is a session label and does not authorize a distributed write. `claim` receives a separate private lease bound to this coordinator execution. A live lease refuses a competing claim. An expired lease that nobody claimed again is lapsed: its own coordinator execution keeps renewing and writing. Once the lease lapsed or ended, an explicit claim with the observed checkpoint revision and epoch uses its basis as the handoff decision and obtains a new execution generation. An old process cannot silently reuse that generation.

The first group claim of a group without any group claim record writes the group's branch snapshot in the same transaction: the project policy's `git.branch_mode` and revision, or, while the project has no policy (revision 0), the `git.branch_mode` the client reports. Later group claim generations keep it and never read the current policy. The group branch, its members, and its wrap-up take the mainline branch from it, not from `kander config --json` (`snippets/git-integrate.md` "Integration Target"); a group first claimed before snapshots existed has none and keeps the local `git.branch_mode`. Only a claim that creates the snapshot checks the policy revision: when the policy changed since the client read it, the claim is refused with `policy_revision_stale` (HTTP 409) and nothing is written; the client reads the policy again and sends the claim once more, and a second refusal exits non-zero and names the server's revision and `kander policy show`.

The background worker renews the group lease independently of the UI and reports confirmed coordinator exit. Reconcile sends locally checked Git facts; the service repeats the producer pass and refuses to advance a member without the matching verdict.

Reconcile has two deadlines. The local Git verification (the Git commands and service reads between the facts request and the reconcile request) has 5 minutes. The reconcile request has 60 seconds, which start when the verification ends. A verification that runs out of time, or a Git step that runs out of its own time, is not a verdict: the command fails with `reconcile_verify_interrupted`, sends no reconcile request, and names the member or wrap-up fact and the deadline; check the network to origin and run the same request file again. A reconcile request without an answer within its deadline fails with `reconcile_result_unknown`: the service may have applied it. Running the same request file again is safe, because the service replays a request whose hash it recorded last; read the result first with `kander coordinator show <group>` when needed. A refusal with `checkpoint CAS` means another request changed the checkpoint; follow "Rejections". A command that ended before the request fails with `reconcile_not_sent`. Only a service the client cannot reach is `unreachable`. Recovery of a live member requires its private task proof and a fresh service-clock ticket around a confirmed stop on the owning host. An unknown, running or foreign executor refuses this route.

After a member lease is lapsed, lost or released, an explicit user decision may instead reset the blocked member under the current private coordinator lease. The service rechecks the exact task revision and old generation in the transaction. It records a new released audit generation, preserves the predecessor scene and an honest unknown observation, and supersedes old-generation requests. This recovery does not assign or start an executor. Continue with a fresh `resume --agent <agent> --decision <reference>` and satisfy the existing review-cycle requirements. Pending starts and dispatch bindings still block reset.

The client freezes recovery input, private proof and operation key before transport. A retry reconciles that exact request, including after a lost response or an expired coordinator lease. A completed receipt replays read-only; it cannot authorize a new recovery. `remove-member` is one group write under the same lease check as `reconcile`; the client sends it with a fresh idempotency key, and the service answers a retry of that key with the first result.

To release a distributed coordinator explicitly, use `kander coordinator release --end-execution <absolute-release.json>` with `group_id`, `reason`, and `decision`. This ends the private lease; the public checkpoint and its audit history remain readable.

A release is final for the coordinator execution that runs it. That execution can never claim the group again, and it can no longer dismiss the members of the group, because the worker then removes its group credential. Dismiss first when that is still needed, and release afterwards. Release only when the group is complete, or when the user decides to hand the group to another session; only a new orchestrator session can take the group over, with the observed checkpoint revision and epoch and a decision reference. Never release to retry a refused claim. Without `--end-execution` the command sends nothing, exits 2, and prints these consequences and the confirmed syntax on standard error. With it, the command prints the same JSON as before on standard output, and a notice with the execution ID, the group ID, and the generation on standard error.

While a coordinator execution holds its lease (active or lapsed), it needs no claim. A claim request that is byte for byte the last claim replays the stored checkpoint, unless `remove-member` reset the replay record since. A changed request, or a repeat after such a reset, is refused with `board_rejected`; the error names the generation held and points at `kander coordinator show <group-id>`, then `reconcile`. A coordinator execution bound to `KANDER_COORDINATOR_EXECUTION_ID` (or `KANDER_EXECUTION_ID`) that released its lease, lost it, or was taken over cannot claim again. The client reads the group, then refuses before it sends a claim request, with `lease_invalid` and a text for each case that names the next step: a new orchestrator session takes the group over.

## Additional Syntax

```text
kander coordinator release --end-execution <absolute-release.json>
kander coordinator integrate <absolute-integration.json>
kander coordinator integration-show <target-ref>
```

`integrate` is an explicit distributed Git operation. Its UTF-8 JSON input is at most 64 KiB and requires `operation_id`, `purpose`, `target_ref`, `commit`, and `decision`. The commit is a complete SHA. The optional `via_commit` is a complete SHA and only applies to `deliver`; it is needed only when the client reports that it cannot merge by itself. Use a stable operation ID when recovering an interrupted command; changing its source, target, actor, scope or `via_commit` requires a new ID.

A standalone delivery specifies `task_id` and purpose `deliver`. A group operation specifies `group_id`: `initialize` creates its `refs/heads/group/<group-id>` anchor, `assemble` advances that ref using the listed `contributors`, and `deliver` integrates the group's closed review target into the explicitly named final target. Group operations require the current private coordinator credential. Standalone delivery requires the task holder, and the card must be in `review/` with a published delivery of the closed review target `C` (`kander move <task-id> review --delivery-commit <C>`; `snippets/execute-single.md` "Distributed Mode"). Contributions require review generations that still stand (active or lapsed, not taken over) and their immutable delivery refs in the registered common remote. An assembly candidate can be a verified merge that contains the previous group tip and the named deliveries. In a group `deliver`, a member already in `done/` stays in the topology but contributes no delivery; its closed review target must still equal `commit`. This lets a member taken over after an earlier group receipt integrate again for its new generation once its siblings are done. A group `deliver` in which no member contributes (every member is excluded or in `done/`) is refused before the target lease is requested.

A `deliver` request always names the closed review target as `commit`. Anyone may push the target branch at any time, so the client does not require it to stand still. The request succeeds when the commit reaches the target in one of the ways below, tried in this order; the receipt records which one as `containment`:

1. With `via_commit`, only this rule applies: the commit must be on the common remote and descend from the review target. If it also descends from the current target, the target advances to it, or stays when it equals the target (`via`). If the current target already contains it, nothing is pushed: the receipt records the current target as `via_commit` with `already_contained` true, and your commit as `via_source`. If they have diverged, the client merges the current target and your commit as in rule 5 and records your commit as `via_source`.
2. The review target is an ancestor of the target (`contained`). Nothing is pushed.
3. The target is an ancestor of the review target (`fast_forward`). The review target is pushed.
4. The target holds a run of commits that repeats the review range commit by commit, in order, with equal patches (`replayed`; a rebased push is the usual case). Nothing is pushed.
5. Otherwise the client merges the current target and the review target (first parent the target, second parent the review target) with Git's default merge, without touching the work tree, index, `HEAD`, or any local branch, and pushes the merge commit (`via`, with `via_auto_merge` true and `via_commit` the merge). The merge needs Git 2.38 or later and a Git author identity in the repository configuration.

A conflict, an older Git, or a missing identity refuses the integration before any lease is claimed or anything is pushed. The conflict error lists the conflicting paths (up to ten, with the total); merge the target into your own branch, resolve the conflicts, re-verify, push the result to the common remote (a task branch under the name `KANDER-GIT-RULES.md` "Task Branches" gives), and integrate again with that commit as `via_commit`. kander never resolves a conflict and never builds or tests a merge it made. A `via` receipt records `via_commit`, and `commit..via_commit` is the content no review saw. For a receipt with `via_auto_merge` and without `via_source` that content is the commits the target already held plus one conflict-free merge, with nothing written by hand; with `via_source` it also includes `commit..via_source`, such as a conflict resolved by hand. The service accepts `via_auto_merge` only with `via`, `already_contained` false, and `deliver`, and `via_source` only with `via`, different from `via_commit`, and together with `via_auto_merge` or `already_contained`; a request with either field needs a service that has been upgraded first, because an older service refuses it. A `replayed` receipt records the review range and the range of the target that repeats it. `integration-show` prints these fields.

If the target moves while the command runs, the client gives up the attempt that has pushed nothing, releases its lease, and starts a new one from a fresh observation, within the usual limit of three attempts per command and 32 per operation ID. When the target never settles, the command fails with a message that says so; run it again with the same operation ID. Each contributing member's latest delivery must be an ancestor of the review target or of the target after the integration. The target branch may hold more than the review target; that never blocks the integration.

The client verifies remote identity, delivery availability and ancestry before claiming the exclusive target lease. It records push intent before an update and uses the observed target SHA as a compare-and-swap condition. Neither a cached Git object nor a successful local command alone proves publication. The service records completion only after a fresh remote observation verifies that the integrated commit is contained in the target.

The worker renews the target lease for the integration command's recorded process identity. Only a service answer that ends the attempt stops that command: the target was taken over or lost, or an integration revision conflict shows that the members its claim froze changed. A target released at the command's own generation ends normally: the worker removes the credential and stops nothing. An unreachable service stops nothing either. Its operation journal retains uncertain effects; recovery inspects the remote and the service receipt before another update. Published receipts remain readable after later target generations. `integration-show` prints the public target record without any private credential. Distributed wrap-up evidence copies its `id`, `request.execution_id`, `generation`, `request.commit`, `observed_target`, and `request.target_ref`; see `snippets/commands/dispatch.md` "Distributed wrap-up evidence".
