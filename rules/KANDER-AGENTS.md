# Kander Workflow Rules Entry

This file is the Kander rules entry. It is short on purpose: it names what every session must do first and where the rest of the rules live. The directory containing this file is the "rules root"; every rule file it names sits in the same directory.

## Every Session

1. Run `kander config --json` for the current scope and read the normalized configuration. `kander` means the entry of the current scope: a global install uses `kander` on PATH, a project install uses `<main worktree>/.kander/bin/kander` and never a global command from PATH. If reading or validating the configuration fails, stop the affected Kander operations and report; do not guess switch values. Unrelated work continues.
2. Read `KANDER-BASE-RULES.md`. It is the tool protocol and is not controlled by the module switches.
3. Read `KANDER-LOADING-RULES.md`. It holds the scope paths, the module switch table, the reading map by role, and the rule precedence. Load the other rule files only when it says to or the task needs them, and never load a disabled module through cross references unless the user explicitly asks.

## Language

`agent_language` in the configuration is the language for the user: every reply, card, record, report, and the messages passed to `kander notify` and `kander resume`; when it is missing or empty, use the language the user writes in. A task card's `LANGUAGE` field overrides it for everything about that card; a card without the field uses the configuration. Commit messages, code comments, and identifiers follow the project's own conventions.

## Always Binding

- `KANDER-KANBAN-RULES.md` binds whenever kanban commands are used, with every module setting.
- `KANDER-ISSUE-RULES.md` binds whenever a GitHub issue is imported, investigated, or taken over, including a session started by `kander issue triage` or the issues overlay. Its untrusted-data and binding clauses are security requirements and ignore the switches.
