# kander coordinator

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" (syntax) and `KANDER-KANBAN-RULES.md` "Coordinator Checkpoints" (semantics)

## Purpose and users

Durable group checkpoints for the orchestrator: `claim` fences one coordinator, `reconcile` records every observation, `show` reads the committed checkpoint. The JSON shapes and the recovery protocol are in `snippets/coordinator-checkpoints.md` "Coordinator Checkpoints" and `KANDER-TASK-GROUP-RULES.md` "Durable Coordinator Recovery".

## Syntax

```text
kander coordinator show <group-id>
kander coordinator claim <absolute-claim.json>
kander coordinator reconcile <absolute-observations.json>
```

## Arguments

- `show <group-id>`: print the committed checkpoint.
- `claim <absolute-claim.json>`: group_id, expected_revision, expected_epoch, owner, unique token, basis, and the complete member list.
- `reconcile <absolute-observations.json>`: group_id, expected_revision, the authority from the claim, a members object keyed by task ID (each with its card revision and, when bound, dispatch_id, epoch, base, and completed delivery_commit), and an optional absolute `cwd` for read-only Git verification.

## Preconditions

- A located board; the complete member set; a successful claim before any reconcile. A first unbound delivery needs the card in `review/`, its `TASK_BRANCH` recorded as the bare branch name, and the absolute `cwd` of a repository where that branch head equals the delivery commit.

## Effects

- Stores a checkpoint through the board transaction protocol. Never notifies, moves cards, integrates Git, or grants execution authority.

## Output

- One JSON object with the checkpoint, its revision, and the authority.

## Rejections

- Stale revision or epoch, a changed member set, an unknown member, damaged originals, or a Git verification failure. A wrong argument count or an unknown subcommand is a usage error.
