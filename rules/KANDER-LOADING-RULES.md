# Kander Rule Loading

This file says where the Kander scope lives, which optional rule files exist, and when each one is read. `KANDER-AGENTS.md` sends every session here after the configuration and `KANDER-BASE-RULES.md`. This file is read in every session and is not controlled by the module switches.

## Scope

The rules root is the directory containing `KANDER-AGENTS.md`. It decides the scope and the paths below:

| Logical name    | Global install                             | Project install                        |
| --------------- | ------------------------------------------ | -------------------------------------- |
| Rules root      | `~/.agents/kander`                         | `<main worktree>/.kander/rules`        |
| Command root    | `kander` on PATH (optional `~/.local/bin`) | `<main worktree>/.kander/bin`          |
| Config file     | `~/.config/kander/config.json`             | `<main worktree>/.kander/config.json`  |
| Share dir       | `~/.local/share/kander`                    | `<main worktree>/.kander/share`        |
| Project overlay | `<main worktree>/.kander-config.json`      | same                                   |

- Effective configuration = project overlay > scope `config.json` (or `KANDER_CONFIG`) > defaults. The overlay is merged at read time and is never copied into the scope file.
- The overlay may set `agents` executable paths and argv templates; trust it like the repository's own code.
- A project install keeps its payload only in the main worktree's `.kander/`; task worktrees share it and never copy it.
- In every rule file, `kander` means the entry of the current scope: a global install uses `kander` on PATH, a project install uses the absolute path `<command root>/kander` and never a global command from PATH.

## Language Details

- `agent_language` in the configuration is the language for the user: every reply, card title and body, execution record, completion report, review report, and the messages passed to `kander notify` and `kander resume`. When the value is missing or empty, use the language the user writes in.
- A task card's `LANGUAGE` field overrides the configuration for everything about that card. `kander new` records the configured value at creation; cards without the field use the configuration.
- Commit messages, code comments, and identifiers follow the project's own conventions.
- `language` in the configuration only selects the interface language of the `kander` command itself.

## Optional Modules

| Config key            | Rule file                       | When to read                                          |
| --------------------- | ------------------------------- | ----------------------------------------------------- |
| `rules.code`          | `KANDER-CODE-RULES.md`          | When enabled, before changing code or verifying       |
| `rules.collaboration` | `KANDER-COLLABORATION-RULES.md` | When enabled, when starting a collaborative task      |
| `rules.git`           | `KANDER-GIT-RULES.md`           | When enabled, before branch, commit, or integration   |
| `rules.task_intake`   | `KANDER-TASK-INTAKE-RULES.md`   | When enabled, whenever a request is expected to change code, before any code write; also for a new bug or feature request |
| `rules.task_groups`   | `KANDER-TASK-GROUP-RULES.md`    | When enabled, when planning or running a task group   |
| `rules.review`        | `KANDER-REVIEW-RULES.md`        | When enabled, to decide review triggers and execution |
| `rules.reporting`     | `KANDER-REPORTING-RULES.md`     | When enabled, when reporting at the end of a task     |

- Read `KANDER-KANBAN-RULES.md` whenever kanban commands are used.
- Read `KANDER-ISSUE-RULES.md` whenever a GitHub issue is imported, investigated, or taken over, including a session started by `kander issue triage` or the issues overlay. Its untrusted-data and binding clauses are security requirements and ignore the switches.
- `task_groups` depends on `git`.
- `review` reads the "Delivery Self-Check" of `KANDER-CODE-RULES.md` only when `code` is also on; with `code` off, `KANDER-REVIEW-RULES.md` "Preconditions and Execution" states what the author checks before review instead.
- With `task_intake` off, no plan options are presented and cards are created manually; a task group is still possible when `task_groups` is on.
- With `git` off, a single card follows the user's own working directory, branch, and delivery flow.
- With `review` off, no review is requested automatically, but `kander review` may still be invoked explicitly.
- With `reporting` off, real card results and the necessary execution records are still required.

## Phase Snippets

Phase rules live in `snippets/` below the rules root. Kander names the file in the prompt that starts the phase; read it then, by its absolute path. Every applicable rule binds whether or not it was read. Command contracts are `snippets/commands/<command>.md`, indexed by its `README.md`.

| Snippet | Read when |
| --- | --- |
| `snippets/execute-single.md` | Executing a card (named in the start, resume, and takeover task files) |
| `snippets/orchestrate-sessions.md` | Orchestrating (named in the orchestrate task file); a blocked executing agent |
| `snippets/failure-recovery.md` | Terminating a card; an executing agent stopped or lost its session |
| `snippets/coordinator-checkpoints.md` | Writing or recovering a task group checkpoint |
| `snippets/review-evidence-gate.md` | Before `move done`; handling a review plan, batch, or closure |
| `snippets/git-sync.md` | Before and after a work item, before review, before delivery (Git module) |
| `snippets/git-integrate.md` | Integrating a task or group branch into its target (Git module) |
| `snippets/report-orchestration.md` | Issuing the orchestration summary (reporting module) |
| `snippets/execute-group-member.md` | (pending) |
| `snippets/orchestrate-start.md` | (pending) |
| `snippets/orchestrate-review.md` | (pending) |
| `snippets/orchestrate-integrate.md` | (pending) |
| `snippets/self-executed-group.md` | (pending) |
| `snippets/fix-round.md` | (pending) |
| `snippets/wrap-up.md` | (pending) |

## Rule Wording

In a sentence that directs the agent, `must` marks an obligation, `never` marks a prohibition, and `may` marks a permission. A plain imperative sentence is an obligation. A sentence that describes the behaviour of a tool, the system, or a card uses these words in their ordinary sense, and `can` states a capability.

## Rule Precedence

- Explicit user instructions in the current session > the project-level `AGENTS.md` or `CLAUDE.md` closest to the target file > the user's own global rules > enabled Kander modules and the current scope's configuration > module defaults.
- A prompt Kander renders carries instance facts (task ID, dispatch ID, epoch, paths, language, size) and the exact command lines to run; those are authoritative for that run. The protocol itself comes from the rule files and the snippets, never from a prompt.
- When `AGENTS.md` and `CLAUDE.md` in the same directory conflict and no user instruction resolves it, stop only the affected operation and ask the user.
