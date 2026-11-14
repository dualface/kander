# kander coordinator

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" (syntax) and `KANDER-KANBAN-RULES.md` "Coordinator Checkpoints" (semantics)

## Purpose and users

Durable group checkpoints for the orchestrator: `claim` fences one coordinator, `reconcile` records every observation, `show` reads the committed checkpoint. `recover` is the audited last resort for a blocked member; it runs only on an explicit user decision. The JSON shapes and the recovery protocol are in `snippets/coordinator-checkpoints.md` "Coordinator Checkpoints" and `snippets/orchestrate-start.md` "Durable Coordinator Recovery".

## Syntax

```text
kander coordinator show <group-id>
kander coordinator claim <absolute-claim.json>
kander coordinator reconcile <absolute-observations.json>
kander coordinator recover <absolute-request.json>
```

## Arguments

- `show <group-id>`: print the committed checkpoint.
- `claim <absolute-claim.json>`: group_id, expected_revision, expected_epoch, owner, unique token, basis, and the complete member list.
- `reconcile <absolute-observations.json>`: group_id, expected_revision, the authority from the claim, a members object keyed by task ID (each with its card revision and, when bound, dispatch_id, epoch, base, and completed delivery_commit), and an optional absolute `cwd` for read-only Git verification.
- `recover <absolute-request.json>`: group_id, member, expected_revision, expected_epoch, decision (the user decision reference), and reason. All six are required. The member's column selects the mode.

## Preconditions

- A located board; the complete member set; a successful claim before any reconcile. A first unbound delivery needs the card in `review/`, its `TASK_BRANCH` recorded as the bare branch name, and the absolute `cwd` of a repository where that branch head equals the delivery commit.

- `recover` needs the member blocked in the checkpoint, and the current checkpoint revision and coordinator epoch. Run it only on a user decision: the orchestrator never recovers on its own, and Kander records the decision reference without verifying it.

## Effects

- `show`, `claim`, and `reconcile` store or read a checkpoint through the board transaction protocol. They never notify, move cards, integrate Git, or grant execution authority.
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

## Output

- `show`, `claim`, and `recover`: one JSON object with the checkpoint, its revision, and the authority.
- `reconcile`: the same object plus `result`. Exit 0 with `complete`: no member is blocked. Exit 3 with `partial`: the checkpoint is committed and some members are blocked. Exit 1 with `failed`: the checkpoint is committed and every member is blocked. Exit 1 with no JSON: the request was refused and the checkpoint is unchanged.

## Rejections

- `reconcile` refuses the whole request on a stale card revision (retry with a fresh observation), a stale checkpoint revision or authority, a changed member set, an unknown member, or Git facts that changed during the command. Damaged originals, a failed Git verification, or a wrong dispatch observation block that member instead.
- `recover` refuses a missing field, a stale revision or epoch, a member that is not blocked, and a `backlog/` or `todo/` member. A repeated request fails on the revision, so one recovery wins; after a lost response read `recoveries` with `show`. A reset also refuses a member with a dispatch binding (end it with `dispatch fail|cancel|settle`), a `pending` member, and a member with a running executor or executor evidence. Each error names its evidence and next steps:

  - `pending`: wait while the starter runs. When the executor is alive, use `kander resume <task-id> --adopt --decision <reference>`. When the starter and the executor have ended, use `kander resume <task-id> --agent <agent> --decision <reference>`. A card with a dispatch binding first ends it with `kander dispatch fail|cancel`, or `kander dispatch settle` when the round completed.
  - `alive`, `alive-orphaned`, or `drifted` in `working/`: the error names the address. Use `kander resume --agent <agent>`, or stop the executor at that address and run `recover` again.
  - `alive`, `alive-orphaned`, or `drifted` in `review/`, and `unknown` with evidence at one address: the error names the address. Stop the executor at that address and run `recover` again. A `review/` member takes `resume --agent` only while its start attempt has not ended, and a takeover cannot exit an executor it cannot prove.
  - 2 or more matches: the error names the count. Stop the executors that match this session and run `recover` again.
- A wrong argument count, a relative `recover` path, or an unknown subcommand is a usage error.

## Distributed project

The service owns the checkpoint. Its public authority token is a session label and does not authorize a distributed write. `claim` receives a separate private lease bound to this coordinator execution. A live lease refuses a competing claim. After lease loss, an explicit claim with the observed checkpoint revision and epoch uses its basis as the handoff decision and obtains a new execution generation. An old process cannot silently reuse that generation.

The background worker renews the group lease independently of the UI and reports confirmed coordinator exit. Reconcile sends locally checked Git facts; the service repeats the producer pass and refuses to advance a member without the matching verdict. Recovery of a live member requires its private task proof and a fresh service-clock ticket around a confirmed stop on the owning host. An unknown, running or foreign executor refuses this route.

After a member lease is lost or released, an explicit user decision may instead reset the blocked member under the current private coordinator lease. The service rechecks the exact task revision and old generation in the transaction. It records a new released audit generation, preserves the predecessor scene and an honest unknown observation, and supersedes old-generation requests. This recovery does not assign or start an executor. Continue with a fresh `resume --agent <agent> --decision <reference>` and satisfy the existing review-cycle requirements. Pending starts and dispatch bindings still block reset.

The client freezes recovery input, private proof and operation key before transport. A retry reconciles that exact request, including after a lost response or an expired coordinator lease. A completed receipt replays read-only; it cannot authorize a new recovery.

To release a distributed coordinator explicitly, use `kander coordinator release <absolute-release.json>` with `group_id`, `reason`, and `decision`. This ends the private lease; the public checkpoint and its audit history remain readable.

## Additional Syntax

```text
kander coordinator release <absolute-release.json>
kander coordinator integrate <absolute-integration.json>
kander coordinator integration-show <target-ref>
```

`integrate` is an explicit distributed Git operation. Its UTF-8 JSON input is at most 64 KiB and requires `operation_id`, `purpose`, `target_ref`, `commit`, and `decision`. The commit is a complete SHA. Use a stable operation ID when recovering an interrupted command; changing its source, target, actor or scope requires a new ID.

A standalone delivery specifies `task_id` and purpose `deliver`. A group operation specifies `group_id`: `initialize` creates its `refs/heads/group/<group-id>` anchor, `assemble` advances that ref using the listed `contributors`, and `deliver` integrates the group's closed review target into the explicitly named final target. Group operations require the current private coordinator credential. Standalone delivery requires the task holder. Contributions require live review generations and their immutable delivery refs in the registered common remote.

The client verifies remote identity, delivery availability and ancestry before claiming the exclusive target lease. It records push intent before an update and uses the observed target SHA as a compare-and-swap condition. A divergent target requires a new reviewed integration commit. Neither a cached Git object nor a successful local command alone proves publication. The service records completion only after a fresh remote observation verifies that the source is contained in the target.

The worker renews the target lease for the integration command's recorded process identity. A lost lease stops that command. Its operation journal retains uncertain effects; recovery inspects the remote and the service receipt before another update. Published receipts remain readable after later target generations. `integration-show` prints the public target record without any private credential. Distributed wrap-up evidence copies its `id`, `request.execution_id`, `generation`, `request.commit`, `observed_target`, and `request.target_ref`; see `snippets/commands/dispatch.md` "Distributed wrap-up evidence".
