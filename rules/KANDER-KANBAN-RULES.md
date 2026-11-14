# Kander Kanban Command Protocol

## Applicability

- Read this file when actually using kanban commands: first the configuration per `KANDER-AGENTS.md`, then enabled modules as needed. This file does not by itself require intake guidance, the Git workflow, review, or fixed reports.
- This file governs the board managed by the `kander` command of the current scope (selected per `KANDER-LOADING-RULES.md` "Scope"). User instructions and target project rules take precedence; cards only store the task contract and execution records and never override user decisions, project rules, or security gates.
- Before operating on the board, read this file under the rules root, then the target card.

## Storage and Location

- `kanban/` is machine-local shared data, not committed to Git; its single instance lives in the main worktree root and serves only agents on the same host and file system. Task worktrees create no copies, mirrors, or symlinks; remote agents cannot see it. Symlinks, junctions, and other reparse points are always unsafe entries, and any failed safety check stops the operation; bypassing it with a file manager or plain path APIs is forbidden.
- Lookup order: `KANBAN_DIR` (tests, non-Git projects, explicit overrides only), then `kanban/` in the main worktree of the current Git repository, then upward search from the current directory. From any worktree of a Git project:

```sh
MAIN_WORKTREE="$(git worktree list --porcelain | sed -n '1s/^worktree //p')"
KANBAN_DIR="$MAIN_WORKTREE/kanban"
```

- Board operations create no branches, commits, pushes, or reviews; the code task a card represents still follows project rules. Committing `kanban/` or modifying the project `.gitignore` to propagate it is forbidden.

## Command Contract

Creating entries, querying, and moving between states use only `kander`; `mv`, `cp`, or a file manager is forbidden. Body editing and same-state form upgrades follow "Entries and Documents" and "Task Scale and Grouping".

Before running any `kander` command in this session, read `<rules root>/snippets/commands/README.md` once. Before the first use of a command, read its contract file. Each contract file states the command's purpose and users, syntax, arguments, preconditions, effects, output, and rejections; `kander <command> --help` prints its syntax lines and its path.

```text
kander config [--json]
kander server start [--data DIR] [--listen ADDR] [--ttl D] [--renew D] [--foreground]
kander server stop
kander server restart
kander server status [--json]
kander server import [--data DIR] --project NAME [--remote URL] --from KANBAN_DIR
kander server maintain [--data DIR] --project NAME [--maintenance]
kander server projects [--data DIR] [--json]
kander doctor
kander version
kander install
kander uninstall [--yes]
kander terminal list
kander terminal test <name> [--keep] [--skip-focus]
kander init [--maintenance] [project-path]
kander list [--mobile|--json [--agent-status]] [backlog|todo|working|review|done|archived|trash]
kander show [--json] <task-id>
kander new [--large] [--language <agent language>] <feature|bug|chore|research> <slug> <title...>
kander move <task-id> <backlog|todo|working|review|done|archived|trash> [--owner <agent> [--decision <reference>]] [--result <result>] [--reason <reason> --decision <reference>] [--duplicate-of <task-id>] [--expect-revision <revision>] [--dispatch-id <id> --execution-epoch <epoch>] [--delivery-commit <full-SHA>] [--disposition <card-relative-path>]
kander pick [task-id]
kander update <task-id> --document <relative-path> --file <UTF8-input> --expect-revision <revision> [--contract-decision-file <UTF8-decision>] [--dispatch-id <id> --execution-epoch <epoch>]
kander start [--agent <configured-agent>] [--launcher <launcher>] [task-id]
kander orchestrate [--agent <configured-agent>] [--launcher <launcher>] (--message TEXT | --message-file FILE) <task-id|task-group-id>...
kander resume [--agent <configured-agent> [--decision <reference>]] [--timeout SECONDS] (--message TEXT | --message-file FILE) [--launcher <launcher>] [--dispatch-id <id>] [--kind fix|sync|wrap-up] [--base <full-SHA>] [--evidence-file <JSON>] <task-id>
kander resume --adopt --decision <reference> <task-id>
kander notify [--pane HERDR-PANE-ID] [--timeout SECONDS] (--message TEXT | --message-file FILE) [--dispatch-id <id>] [--kind fix|sync|wrap-up] [--base <full-SHA>] [--evidence-file <JSON>] <task-id>
kander dispatch prepare <absolute-UTF8-intent.json>
kander dispatch authorize-wrap-up <absolute-UTF8-request.json>
kander dispatch show <task-id> <dispatch-id>
kander dispatch fail|cancel <task-id> <dispatch-id> <dispatch-revision> <reason> [--decision <reference>]
kander dispatch settle <task-id> <dispatch-id> <dispatch-revision>
kander dismiss [--timeout SECONDS] <task-id>
kander check [--all] [task-id ...]
kander check delivery --base <ref> [--commit <ref>] [--json]
kander check overlap --source <ref> [--head <ref>] [--json]
kander guard-write <path>
kander subscribe [--refresh SECONDS] [--heartbeat SECONDS] (<task-group> <task-id>... [--watch <task-id|task-group-id>...] | --watch <task-id|task-group-id>...)
kander coordinator show <group-id>
kander coordinator claim <absolute-claim.json>
kander coordinator reconcile <absolute-observations.json>
kander coordinator recover <absolute-request.json>
kander review [agent] [--task <id>]... [--run-id <id>] [--batch-id <id>] [--previous-run-id <id>] [--requirements-file <JSON>] [--advance-file <JSON>] <CWD> <base-commit> <commit> <role> <task-goal|absolute-spec-path> [review-context] [reviewed-commit]
kander review plan <absolute-CWD> <absolute-plan.json>
kander review extend-plan <absolute-CWD> <absolute-extension.json>
kander review assign <absolute-CWD> <absolute-assignment.json>
kander review disposition <absolute-CWD> <absolute-author-record.json> <expected-card-revision>
kander review interpret <absolute-CWD> <absolute-interpretation.json>
kander review map-legacy <absolute-CWD> <absolute-map.json>
kander review aggregate <absolute-CWD> <batch-id>
kander review advance <absolute-CWD> <absolute-advance-request.json>
kander review close <absolute-CWD> <absolute-close-request.json>
kander review progress <absolute-CWD> <task-id>
kander review <plan|extend-plan|assign|disposition|interpret|advance|close> --schema
kander issue repo [--repo HOST/OWNER/REPO] [--json]
kander issue list [--repo HOST/OWNER/REPO] [--state open|closed|all] [--label NAME] [--search TEXT] [--limit N] [--json]
kander issue show NUMBER [--repo HOST/OWNER/REPO] [--comments] [--json]
kander issue import NUMBER [--repo HOST/OWNER/REPO] [--comments] [--type feature|bug|chore|research] [--large] [--language LANG] [--json]
kander issue triage NUMBER [--card TASK_ID] [--repo HOST/OWNER/REPO] [--agent NAME] [--launcher NAME]
kander issue result NUMBER --card TASK_ID [--repo HOST/OWNER/REPO] [--agent NAME] [--launcher NAME] [--action start|inspect|apply|decide] [--file JSON]
```

For distributed holder confirmation, use `kander review confirm <absolute-CWD> <publication-id> <task-id>` as described in the review command contract.


- Commands only do structural and mechanical validation; authorization, dependencies, and termination reasons are judged by the agent per this file.

## State Model

The directory is the single source of truth for state; the card body has no `status` field.

- `backlog/`: recorded but not yet committed to execution.
- `todo/`: confirmed by the user, contract complete, not yet claimed.
- `working/`: claimed and being implemented, verified, reviewed, or integrated; task group cards also return here during fix rounds and post-integration wrap-up.
- `review/`: used only by task group cards. Development, verification, and the task branch delivery record are complete, waiting for the orchestrator to ff that delivery onto the group branch and arrange applicable review and final integration. The state itself does not guarantee the delivery is on the group branch; the orchestrator verifies before releasing in-group dependencies. After moving in, the executing agent ends this round of response and keeps the interactive CLI session. Fixes, syncs, or wrap-up are dispatched back via `notify`. The executing agent first runs the ID/epoch-bound `kander move <task-id> working --dispatch-id <id> --execution-epoch <epoch>` from the dispatch prompt, then handles them. Members of a self-executed group (`snippets/self-executed-group.md` "Self-Executed Groups") are received, moved back, and completed by the same session with plain moves.
- `done/`: recent tasks that satisfied the completion gate.
- `archived/`: completed, cancelled, duplicate, or wontfix records off the active board. Archived is final: no move leaves it, and no command writes the card again (body, attachments, review evidence, dispositions, dispatch or session records). Read commands and `dismiss` still work, and an `archived/` card with `RESULT: completed` still satisfies dependencies.
- `trash/`: entries the user explicitly asked to delete but not yet permanently cleaned; not a task state.

```text
backlog <-> todo -> working -> done -> archived        (single-card flow)
                      |  ^
                      v  |  fix rounds and wrap-up move back to working with the ID/epoch-bound move
                    review -> working -> done         (task group flow; the orchestrator's wrap-up on behalf
                                                       takes the same path under its fenced grant)

todo -> backlog                                       withdraw commitment, back to scheduling
review -> working (--owner --decision)                user-authorized reclaim of an unbound card, see `snippets/commands/move.md` "Reclaiming"
review -> working (coordinator recover)               user-decided reset of a blocked group member, see `snippets/failure-recovery.md` "Blocked Group Members"
backlog, todo, working, review -> archived            only user-authorized termination
any state except archived and trash -> trash          only on explicit user request
```

- Entering `todo/` requires complete `GOAL`, `EXPECTED_OUTCOME`, `ACCEPTANCE_CRITERIA` (at least one top-level `- [ ]` item that can be judged), and `OUT_OF_SCOPE`. It also requires no `<FILL_IN>` placeholders in those four sections, and the `SELF_REVIEW:` line of "Post-Creation Self-Review" (large tasks and group members also the `CARD_REVIEW:` line). Entering `review/` requires `TASK_BRANCH`. The `done/` gate is in `snippets/execute-single.md` "Execution and Completion" with `snippets/review-evidence-gate.md` "Review Evidence Completion Gate"; the rest in `snippets/failure-recovery.md` "Termination and Cleanup".
- Older boards have no `review/`: when the other 6 directories exist, the first `kander` command that locates the board creates `review/` automatically. When other state directories are missing, stop normal board operations and use `init`. When `review` is occupied by a file or symlink, always fail.

## Entries and Documents

Before creating, completing, updating, or reviewing card content, read `snippets/card-content.md`. It is the single definition of card content; this section governs storage and controlled publication.

### Invariants

- Every new card is a directory `YYYYMMDD-short-slug-task/` containing a regular `spec.md`; `SIZE: small|large`, immediately after TYPE, determines task scale independently of directory form. Legacy `<task-id>.md` entries stay readable until explicit migration. `short-slug` contains only lowercase ASCII letters, digits, and hyphens; the entry name without extension is the task ID.
- Task IDs are unique across the board: never repeated across states, never both file and directory. A move moves the whole entry; the name never changes after creation, is never copied then deleted, and leaves no copies. Card directories use only relative links.
- The card path changes with state moves. Before editing, obtain `kander show --json <task-id>`, prepare the replacement in a separate UTF-8 input file, and publish with `kander update` using that revision; never write a cached path directly. A revision conflict requires reading and reconciling the current document before retrying. New cards are created only through `kander new`; missing cards are never recreated by update or rollback.
- Never put tokens, credentials, sensitive service addresses, or personal data that should not stay on this machine in a card.

### Controlled Documents and Recovery

- Use `update --document spec.md` for every card body. Both scales accept `plan.md`, `report.md`, and ordinary attachments, including relative subdirectories. Legacy file cards are read-only: mutation commands fail before side effects and direct the caller to offline `kander server maintain --project NAME`; never bypass this with an older binary or direct editing.
- Paths must be canonical, relative, and free of traversal, symlinks, and reparse points. Never edit managed `reviews/`, `dispatches/`, manifests, indexes, or control records through update; dedicated producers own them.
- Full-body replacements preserve `LANGUAGE`, `OWNER`, `SESSION`, `WINDOW`, creation/start/completion timestamps, `RESULT`, execution metadata, and managed indexes. Keep `TASK_BRANCH` and ordinary implementation and summary records current through update.
- Manual claiming uses `kander move <task-id> working --owner <agent>`, writing `OWNER` and `STARTED_AT` with the transition; `start` and `resume` own session and window metadata; dispatch-back moves to working keep the existing owner.
- Completion uses `kander move <task-id> done --result completed`, with validation, `RESULT`, and `FINISHED_AT` in one transaction, after the summary or report was written through update. Termination uses `move <task-id> archived --result cancelled|duplicate|wontfix --reason <reason> --decision <user-decision-reference>` (`--duplicate-of <replacement-id>` for duplicate); archiving a completed card uses `--result completed` with reason and decision reference; trash uses `move <task-id> trash --result trashed --reason <reason> --decision <user-decision-reference>`. These references record the authorization basis; the tool does not verify user intent. `--expect-revision` is available on move when the transition must match a previously read snapshot.
- After a card enters todo, its contract stays frozen even if withdrawn to backlog. A user-authorized contract change uses update with `--contract-decision-file <UTF8-decision>` and the expected revision; Kander records the decision and old/new frozen fields in the protected `CONTRACT_DECISIONS` section. This never authorizes changing managed metadata or language.
- A process interrupted during publication leaves a recoverable transaction. Readers report the unfinished operation without repairing it; ask the service operator to stop the server and run `kander server maintain --project NAME` to finish prepared writes and moves. A recovery conflict preserves the evidence and both copies; never guess a primary copy or delete one. Transactions protect commands and agents that follow this protocol, not arbitrary local processes editing files directly.

## Archived Review Evidence

- Task-bound `kander review` uses repeated `--task` flags before positional arguments; its run/batch and retry protocol is in the minimal tool protocol, and using the command does not require the optional review workflow.
- Each directory card retains immutable `reviews/<run_id>/` originals, sidecar, and manifest. REVIEWS is a machine-owned section with one JSON line per run (run/batch/role/execution status/base/commit/predecessor and a relative report path). `update` cannot modify it or its attachments; reports move with the card; never reconstruct an old state path or replace originals with summaries.
- Archiving cleans up automatically. After a move into `archived/` commits, Kander deletes the review runs, batches, plans, closures, dispositions, start and dispatch records, task-group checkpoints, and card `reviews/` copies whose cards are all archived. This happens once every member of each card's task group is archived or in trash (a task-group checkpoint also needs every member it lists to be archived). Anything still shared with another card stays until that card is archived too. The card body and its `REVIEWS` index stay, a per-card receipt lets `check` tell removed evidence from damage, and `move` reports what was removed. A cleanup that fails never undoes the archive; the next archive resumes it.
- `check` reports missing or conflicting intents, incomplete publication, index/manifest/hash mismatches, language or member conflicts, and broken predecessor chains; it never infers PASS from prose. A failed run with complete evidence stays a recorded failure. An interrupted publication requires explicit service recovery, then a retry with the same run ID, which never reruns the reviewer.

## Task Scale and Grouping

- New cards always use directory form. `new` writes `SIZE: small` with IMPLEMENTATION/SUMMARY; `new --large` writes `SIZE: large` and requires a non-empty report.md for completion (a small card still needs its SUMMARY even with a report.md). Both require SELF_REVIEW before todo; large tasks and all group members also CARD_REVIEW. After todo, changing SIZE uses the contract-decision update flow. Never infer scale from a directory or report.md.
- `SIZE` measures difficulty and risk, not file count; it selects the execution agent tier per `snippets/commands/start.md` "Start Parameters and Metadata", the review stage scale per `KANDER-REVIEW-RULES.md` "Review Stages", and the large-task documents. Choose `large` when any of the following holds, otherwise `small`; when unclear, choose `large`:
  - Scope: several modules or phases, a release or rollback plan, or verification beyond a single targeted test run (so `plan.md` is needed to stay reviewable).
  - Contract: adds or alters a schema, protocol, DTO, message, or interface shared across a module, process, platform client, or repository boundary, including one producer with a single consumer, whether or not this card touches every side.
  - State: adds or alters concurrency, ordering, generation or epoch semantics, retry or reconnect behavior, or a state machine.
  - Security: touches authentication, authorization, credentials, signing, cryptography, or another trust boundary.
  - Uncertainty: a design choice is still open at creation, the card is a contract card of a task group, or it revises the design of an earlier delivery whose assumptions failed. A follow-up card that only applies deferred or non-blocking findings does not meet this by that fact alone.
  A card whose whole change and verification fit one `IMPLEMENTATION` entry and meet none of these is `small`. Group membership grants no exemption: assess every member independently; how a request is split is governed by `KANDER-TASK-GROUP-RULES.md` "Task Splitting and Task Groups", not by `SIZE`.
- Cards keep optional task group fields and dependency records. With task_groups off, every request becomes a single card, whatever the number of goals; when on, plan and execute per `KANDER-TASK-GROUP-RULES.md`, with git also on. Intake guidance belongs to `KANDER-TASK-INTAKE-RULES.md` and is read only when `rules.task_intake=true`; a user operating the board directly does not need it.
- `kander new` creates the template in backlog; the caller fills in the full contract and the detailed plan per `snippets/card-content.md` "Required Plan for New Cards" from confirmed content, in the card's `LANGUAGE`, and never writes suggestions as user decisions.

### Explicit Migration and Maintenance

- Offline `kander server maintain --project NAME` migrates legacy `<task-id>.md` files to `<task-id>/spec.md`, adding `SIZE` and rewriting only the relative links needed to keep their targets; all other bytes are preserved, and repeated maintenance reports zero migrations. `list`, `show`, `check`, and `subscribe` read legacy files without migrating them (missing SIZE means small for a file, large for a directory); read commands never migrate.
- Before migration, pause all executing agents, external editors, notifications, and archive writers, including older binaries, and keep that window open through recovery and migration. When working or review cards exist and migration is needed, maintenance refuses by default and lists the affected IDs; only after all writers are paused, `kander server maintain --project NAME --maintenance` acknowledges the preconditions, also for recovery of an interrupted migration. No agent is terminated automatically.
- An interrupted migration leaves a managed pending transaction that only explicit offline service maintenance replays; no reader repairs the board. Never guess a primary copy, delete unknown artifacts, or hide an unfinished operation by renaming it. `guard-write` recognizes stale state paths and the old `.md` spelling but makes no external write atomic; use update.

### Post-Creation Self-Review

Follow `snippets/card-content.md` "Post-Creation Self-Review" before advancing a newly completed card to `todo/`.

## Phase Rules

The rules an agent needs only in one phase live in `snippets/` and are read when that phase arrives; Kander names the file in the prompt that starts the phase, and `KANDER-LOADING-RULES.md` "Phase Snippets" lists them.

- Claiming and executing a card: `snippets/execute-single.md`.
- An orchestrator session started by `kander orchestrate`, and handling blocked executing agents: `snippets/orchestrate-sessions.md`.
- Termination, cleanup, and failure recovery: `snippets/failure-recovery.md`.
- Coordinator checkpoints: `snippets/coordinator-checkpoints.md`.
- The review evidence completion gate: `snippets/review-evidence-gate.md`.
