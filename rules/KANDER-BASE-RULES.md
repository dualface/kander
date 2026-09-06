# Kander Minimal Tool Protocol

- Before using Kander, read the current scope's configuration as described in `KANDER-AGENTS.md`. This file only constrains the tool; it does not prescribe communication, architecture, code verification, Git, or automatic review flows.
- Talk to the user, and write cards, records, and reports, in the `agent_language` from the configuration, as described in `KANDER-AGENTS.md` "Language".
- When using kanban commands, read the structure, state, claiming, notification, and recovery protocol in `KANDER-KANBAN-RULES.md`; no optional module needs to be enabled.

**Single Review**

- `kander review` is a single-review tool that can be invoked explicitly. Its arguments are `[agent] <CWD> <base-commit> <commit> <role> <task-goal|absolute spec path> [review-context] [reviewed-commit]`.
- The target must be a clean Git worktree, and base must be an ancestor of commit.
- The command keeps the reviewer read-only and validates its output.
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
