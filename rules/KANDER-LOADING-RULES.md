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
| `rules.task_intake`   | `KANDER-TASK-INTAKE-RULES.md`   | When enabled, on receiving a bug or feature request   |
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

## Reading Map by Role

The three largest rule files are protocols, not reading material. Load the sections your role needs at session start and follow cross references only when the referenced situation arises; section names below are the headings inside each file. Every applicable rule in the always-on protocols and enabled modules binds whether or not it was read. Disabled module rules do not bind unless the user explicitly asks for that workflow. The map limits what must be read up front, not what applies.

| Role | Read at session start | Read when the situation arises |
| --- | --- | --- |
| Executing agent, single card | `KANDER-KANBAN-RULES.md` "Command Contract", "Entries and Documents", "Claiming, Starting, and Coordination", "Execution and Completion"; the enabled `KANDER-GIT-RULES.md` and `KANDER-CODE-RULES.md` in full | `KANDER-KANBAN-RULES.md` "Failure Recovery", "Review Evidence Completion Gate"; the enabled `KANDER-REVIEW-RULES.md` from "Preconditions and Execution" through "Review Stages" before requesting review; `KANDER-REPORTING-RULES.md` when reporting |
| Executing agent, group member | As for a single card, plus `KANDER-TASK-GROUP-RULES.md` "Git Branches and Worktrees", "Group Integration Branch", "Executing Agent Wrap-Up" | `KANDER-KANBAN-RULES.md` "Durable Dispatch" when a `sync`, `fix` or `wrap-up` notice arrives; `KANDER-REVIEW-RULES.md` "Main Agent Verification Duty" for a fix round |
| Orchestrator | `KANDER-TASK-GROUP-RULES.md` in full; `KANDER-KANBAN-RULES.md` "Durable Dispatch", "Orchestrator Sessions", "Coordinator Checkpoints", "Review Evidence Completion Gate"; the enabled `KANDER-GIT-RULES.md` "Keeping a Task Branch Current" and "Integration and Cleanup" | The enabled `KANDER-REVIEW-RULES.md` "Group-Level Review for Task Groups" and "Controlled Plans, Author Dispositions, and Batch Closure" when the first batch is due; "Stage One Backend Failures" on a reviewer failure |
| Card creator (intake) | `KANDER-TASK-INTAKE-RULES.md`; `KANDER-KANBAN-RULES.md` "Task Scale and Grouping" including "Post-Creation Self-Review"; when `task_groups` and `git` are enabled, `KANDER-TASK-GROUP-RULES.md` "Task Splitting and Task Groups", "Dependencies Between Task Cards", "Running Task Cards in Parallel" | When `task_groups` and `git` are enabled, the rest of `KANDER-TASK-GROUP-RULES.md` only when the creator also orchestrates |
| Main agent running a review | `KANDER-REVIEW-RULES.md` from "Reviewer Selection" through "Review Stages", "Main Agent Verification Duty", "Conclusions and Failure Handling" | "Controlled Plans, Author Dispositions, and Batch Closure" when closing a batch; "Review Profiles" and the templates when writing the task context |

## Rule Precedence

- Explicit user instructions in the current session > the project-level `AGENTS.md` or `CLAUDE.md` closest to the target file > the user's own global rules > enabled Kander modules and the current scope's configuration > module defaults.
- When `AGENTS.md` and `CLAUDE.md` in the same directory conflict and no user instruction resolves it, stop only the affected operation and ask the user.
