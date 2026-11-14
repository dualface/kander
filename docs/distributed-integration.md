# Distributed Git integration

A client integrates an explicitly named source into an explicitly named remote branch. The service owns an exclusive lease for that target. The service never executes Git. It validates the actor, execution generations, complete group membership, formal deliveries and closed review targets under the board transaction locks. The client verifies the registered common remote and actual ancestry.

## Operations and authority

`kander coordinator integrate <absolute-integration.json>` accepts UTF-8 JSON up to 64 KiB. Required fields are `operation_id`, `purpose`, `target_ref`, `commit`, and `decision`. Unknown fields and trailing JSON are rejected. `commit` is a complete SHA and `target_ref` is a complete `refs/heads/…` name.

| Purpose | Scope | Authority and effect |
| --- | --- | --- |
| `initialize` | `group_id` | Current private coordinator; create or adopt the group's exact anchor at `refs/heads/group/<group-id>` |
| `assemble` | `group_id`, `contributors` | Current private coordinator; fast-forward that group branch with the named current review deliveries |
| `deliver` | `task_id` or `group_id` | Current task holder or private coordinator; fast-forward an explicit final target with the current closed review target |

Group requests include the complete topology, even when only some members contribute to an assembly. Each contribution binds its current card revision, task generation and formal immutable delivery. An audited excluded member does not contribute to final delivery. A public coordinator session label grants no authority. The target lease does not grant ownership of any task.

The target identity is `i-<SHA256(ref)>`. Its private token, execution, generation and resource revision are independent of task claim generations and dispatch epochs. Renewals change neither card content nor checkpoint versions. An expired target reads as lost. Acquiring another generation requires a fresh inspected target record and an explicit decision. Every phase change and its precise operation receipt share one redo transaction.

## Source publication and target update

A newly composed merge may exist only in the coordinator's clone. After obtaining the target lease, the client publishes it at `refs/heads/kander/<project>/integrations/<execution>/<SHA>`. This immutable source ref has its own saved intent and an absent-ref compare-and-swap. It does not reuse a member's delivery ref. Recovery observes the exact ref before attempting publication again.

Before claiming, the client checks each contributor's immutable delivery ref in the common remote, including when its commit is already cached. It verifies source ancestry locally. Before changing the target it commits a `pushing` intent and renews its authority. The push uses the exact observed target SHA as a force-with-lease condition; the client requires fast-forward ancestry before this conditional update. A divergent target needs a newly reviewed integration commit. A source already contained in the target uses a read-only confirmation and cannot obtain push intent.

The client always reads the real remote after a push, including a nonzero exit. A command exit alone is not publication evidence. Confirmation requires fresh service-clock observation material and the verified source-to-target ancestry. The service atomically publishes the integrated result and releases the target lease.

## Recovery and completion

The private action journal freezes the operation ID, actor execution, options, exact request bodies and keys before effects. Repeating an ID with different material is rejected. A completed operation replays its historical service result even after task handoff or a later target generation; it grants no new write. A restarted command audits and releases its previous owned reservation before obtaining a fresh target execution. An uncertain release response replays the saved release operation. An expired reservation may be explicitly replaced, with the old intent retained in history.

The independent worker renews the target for the command's recorded PID, kernel start identity and boot identity. It never reports that command as a task agent or sends task requests to it. Lease loss stops the recorded command. The CLI cancels its owned Git process tree on a termination signal. Uncommitted task worktrees are preserved.

A final receipt is indexed by task and generation. `done` requires that receipt to match the current closed review target. Explicit no-Git plans with target `N/A` and no formal delivery retain their existing completion semantics. Portable wrap-up evidence names the integration ID, execution and generation; the service checks its immutable result, source, target and member generation. Clients fetch the source ref and verify that the actual remote still contains the published target. A missing source/delivery ref or a rewound target refuses completion despite cached objects. Historical records without the new optional fields retain their bytes.

The evidence is built from the public receipt: `id` is `integration_id`, `request.execution_id` is `integration_execution`, `generation` is `integration_generation`, `request.commit` is `source_commit`, `observed_target` is `target_commit`, and `request.target_ref` is `target_ref`. Before it reads the execution, the client reports each missing identity field by name with that mapping. After it reads the named execution receipt, it reports each differing value with the receipt value. Both checks run before any request or dispatch is created, so neither looks like an unresolved integration. The released rules carry the complete JSON example in `rules/snippets/commands/dispatch.md` "Distributed wrap-up evidence".

Completion is verified by the client. The service checks its published receipt and never runs Git, so it does not depend on its working directory or on a Git installation. The complete binary links the client's Git verifier for local boards (`board.SetWrapUpGitVerifier` from `internal/launch`); the service's move passes `board.MoveOptions.ServiceCompletion`, which keeps that process-wide verifier out of the service path. The client verifies in the recorded execution worktree, or, when cleanup already removed it, in the current directory when its `origin` and push URL are the registered project remote. Another project's clone and a checkout that cannot fetch the evidence refuse completion.

Delivery refs (`refs/heads/kander/<project>/<task>/g<generation>/<commit>`) and integration source refs (`refs/heads/kander/<project>/integrations/<execution>/<commit>`) are immutable evidence with their own lifecycle: later completion, review and coordinator verification fetch them. Task and group branch cleanup never deletes them.

`kander coordinator integration-show <target-ref>` prints the public target record. REST routes under `/projects/{project}/integrations/{id}` provide reads, observations, claim, renewal, phase actions and historical execution results. `/projects/{project}/tasks/{task}/integration` reads that task's current-generation final result. Public responses never include the private bearer except the direct claim grant.
