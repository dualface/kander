# Kander Minimal Tool Protocol

This file constrains the tool and the language used with the user. It does not prescribe communication style, architecture, code verification, Git, or automatic review flows; those are the optional modules.

- Read the current scope's configuration first, as described in `KANDER-AGENTS.md`.
- Talk to the user, and write cards, records, and reports, in the `agent_language` from the configuration, or in the card's `LANGUAGE` when working on a task card.
- Read `KANDER-KANBAN-RULES.md` whenever kanban commands are used; no optional module needs to be enabled.
- Before running any `kander` command in this session, read `<rules root>/snippets/commands/README.md` once. Before the first use of a command, read its contract file.
- Tool boundary failures must be reported. Bypassing them with ordinary file operations, or by controlling the agent directly, is forbidden. The module switches never change arguments, data structures, path validation, or process isolation.

## Single Review

- `kander review` is a single-review tool that can be invoked explicitly; its contract, including run and batch identity, publication, and reviewer isolation, is `snippets/commands/review.md`. Invoking it does not enable the full review or Git flow.

## Installation and Task Files

- Automation must invoke the command root's `kander` through a process API argv array; never assemble shell command strings.
- The executing agent and the reviewer read the complete task from a UTF-8 temporary file named by a one-line instruction on the command line. The file asks the agent to delete it when done; a failed deletion does not affect the result.

## Permissions and Boundaries

- Review-private directories and files are accessible only to the current user. One exception applies on Windows: the review runtime also grants read-only access to a local sandbox group a built-in reviewer definition names (Codex). The configuration, the kanban board, Git exclude, and the review runtime all reject symlinks, junctions, and other reparse points. A failed safety check stops the operation.
- Bypassing the command to operate on these boundaries directly is forbidden.
