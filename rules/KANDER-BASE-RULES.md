# Kander Minimal Tool Protocol

- Before using Kander, read the current scope's configuration as described in `KANDER-AGENTS.md`. This file only constrains the tool and the language used with the user; it does not prescribe communication style, architecture, code verification, Git, or automatic review flows.
- Talk to the user, and write cards, records, and reports, in the `agent_language` from the configuration, or in the card's `LANGUAGE` when working on a task card, as described in `KANDER-AGENTS.md` "Language".
- When using kanban commands, read the structure, state, claiming, notification, and recovery protocol in `KANDER-KANBAN-RULES.md`; no optional module needs to be enabled.

**Single Review**

- `kander review` is a single-review tool that can be invoked explicitly. Its arguments are `[agent] [--task <id>]... [--run-id <id>] [--batch-id <id>] [--previous-run-id <id>] [--requirements-file <JSON>] [--advance-file <JSON>] <CWD> <base-commit> <commit> <role> <task-goal|absolute spec path> [review-context] [reviewed-commit]`.
- The target must be a clean Git worktree, and base must be an ancestor of commit.
- Without `--task`, review does not locate a board. With tasks, flags precede CWD, repeated task IDs are deduplicated, and the board is located from the target CWD. Cards must be working/review directory cards with one language and compatible task group membership.
- Task-bound review requires an explicit batch ID. A new batch also requires a JSON requirements file naming all four roles as `required` or `N/A: <reason>`. Resolve these requirements from user/project rules and stage policies; the file records that decision, it does not grant approval.
- A missing run ID is generated and printed to stderr. Reuse that ID only to recover or finish publication; it never launches another reviewer. Changed inputs conflict. Use distinct run IDs for PM and QA on the same target. A retry after a process crash records interrupted evidence, never PASS.
- Raw output, logs, input snapshots, sidecar and manifest remain in each card's `reviews/<run_id>/`. The machine-owned REVIEWS section contains one JSON index line per run. Tool execution success is separate from semantic PASS. Do not edit or delete these artifacts as temporary reports.
- Publication is atomic per card. Partial publication exits nonzero, preserves successful cards and reports each result. After an interrupted board transaction, run `kander init` under its maintenance requirements; then retry the identical review invocation with the same run ID. An incompletely published run cannot establish completion.
- The command keeps built-in reviewers read-only through the isolation arguments declared on each agent's definition, and validates the output. Custom reviewers that declare a review template are accepted; their read-only posture is the definition author's responsibility. For every reviewer, Kander still isolates the review-private directories and afterwards checks the Git-visible state of the target worktree, which does not detect writes outside it or to ignored paths inside it.
- Invoking it does not enable the full review or Git flow and does not require the target branch to be `develop`.

- The switches do not change arguments, data structures, path validation, or process isolation. Tool boundary failures must be reported; bypassing them with ordinary file operations or by controlling the agent directly is forbidden.

**Installation and Task Files**

- Installation is done by the binary itself: the first run of an uninstalled `kander` enters the interactive wizard, or run `kander install` to rerun it.
- Windows does not modify `PATH` automatically.
- Automation involving special characters must invoke the command root's `kander` through a process API argv array; do not assemble PowerShell/cmd command strings.
- On every platform the executing agent and the reviewer read the complete task from a UTF-8 temporary file; the launch arguments contain only the CLI's required control options and a one-line instruction with the file path.
- The file asks the agent to try deleting it when done.
- A failed deletion or a leftover file does not affect the result.

**Permissions and Cleanup**

- Review-private directories and files are accessible only to the current user: POSIX `0600`/`0700`.
- Windows applies a protected DACL with inheritance disabled at creation time; publishing first and tightening later is forbidden.
- The Windows review root handle does not share WRITE/DELETE and is held until sensitive files are written, the reviewer has run, the process tree is collected, and cleanup finishes, which blocks renames and in-place reparse switches.
- Cleanup rejects reparse points level by level from the pinned handle, with a bounded budget; failure means the review fails.
- The configuration does not check, migrate, or tighten permissions: POSIX keeps the existing mode, new objects follow the umask.
- On Windows, new config files and directories inherit the parent ACL.
- On Windows, the configuration rejects reparse points component by component from the volume/UNC anchor and uses pinned handles for reading and atomic replacement.
- The kanban board and Git exclude likewise reject symlinks, junctions, and other reparse points.
- Git exclude keeps the existing ACL and appends deduplicated entries within the same pinned handle.
- Bypassing the command to operate on these boundaries directly is forbidden.
