# Kander Workflow Rules Entry

This entry only locates and loads rules on demand; it does not take over the development flow by default. The installer copies the same Markdown originals to the rules root and generates no customized rule files.

## Scope

The directory containing this file is the "rules root". It determines the scope and the paths below:

| Logical name  | Global install                 | Project install                     |
| ------------- | ------------------------------ | ----------------------------------- |
| Rules root    | `~/.agents`                    | `<main worktree>/.kander/rules`     |
| Command root  | `~/.local/bin`                 | `<main worktree>/.kander/bin`       |
| Config file   | `~/.config/kander/config.json` | `<main worktree>/.kander/config.json` |
| Share dir     | `~/.local/share/kander`        | `<main worktree>/.kander/share`     |

- All settings live in the config file.
- A project install keeps its payload only in the main worktree's `.kander/`; task worktrees share it and create no copies, mirrors, or symlinks.
- Below and in every rule file, `kander` means the entry of the current scope. A global install may use the absolute path under the command root or a `kander` already on PATH; a project install must use the absolute path `<command root>/kander` (`<command root>\kander` on Windows) and must not substitute a global command from PATH.

## Read the Configuration First

- At the start of every new session run `kander config --json` for the current scope, read the normalized configuration, then read `KANDER-BASE-RULES.md` in the same directory. The minimal tool protocol is not controlled by the optional module switches.
- When reading or validating the configuration fails, stop the affected Kander operations and report; do not guess switch values. The user's own workflows and unrelated questions continue.
- Load rule files according to the switch table below and the needs of the task. Do not read, execute, or load a disabled module through cross references; the user may explicitly ask for an exception.

## Language

- These rules are written in English and are the only copy; they are not translated per install.
- `agent_language` in the configuration is the language for talking to the user. Use it for every reply to the user, for card titles and bodies, execution records, completion reports, review reports, and the messages passed to `kander notify` and `kander resume`. When the value is missing or empty, use the language the user writes in.
- Commit messages, code comments, and identifiers follow the project's own conventions, not `agent_language`.
- `language` in the configuration only selects the interface language of the `kander` command itself and does not affect how the agent talks to the user.

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
- `task_groups` depends on `git`.
- With `task_intake` off, cards can still be created manually and run one at a time.
- With `git` off, a single card follows the user's own working directory, branch, and delivery flow.
- With `review` off, no review is requested automatically, but `kander review` may still be invoked explicitly.
- With `reporting` off, filling in real card results and the necessary execution records is still required.

## Rule Precedence

- Explicit user instructions in the current session > the project-level `AGENTS.md` or `CLAUDE.md` closest to the target file > the user's own global rules > enabled Kander modules and the current scope's configuration > module defaults.
- When `AGENTS.md` and `CLAUDE.md` in the same directory conflict and no user instruction resolves it, stop only the affected operation and ask the user.
