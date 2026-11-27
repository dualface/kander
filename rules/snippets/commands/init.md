# kander init

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Initializing the Board")

## Purpose and users

Binds a checkout to a service project and writes the project reference to the Kander rules into its project rules files. Used by the user when configuring a client. Normal clients never open local board storage.

## Syntax

```text
kander init [--server <URL>] [--project <name>] [project-path]
```

## Arguments

- `project-path`: checkout whose project binding to initialize; default is the current checkout.
- `--server <URL>`, `--project <name>`: the Kander server URL and the service project name to bind. They are saved as `server.url` and `project.name` in the checkout's `.kander-config.json`, and every later command in that checkout uses them, `kander server import` included. See "Binding fields" below.

## Examples

```text
# Bind the current checkout to the configured project
kander init

# Bind a checkout to a project of a server
kander init --server http://127.0.0.1:7878 --project demo /tmp/checkout
```

## Preconditions

- A readable scope configuration and reachable compatible service.
- A project name from configuration or `--project`. The effective service URL comes from `--server`, configuration, or the default `http://127.0.0.1:7878`.

## Effects

- Registers or reads the service project, binds or checks its Git remote against the exact text `git remote get-url origin` prints for the checkout, and sparsely saves explicitly requested binding fields.
- Never creates or recovers local `kanban/`, changes Git excludes, migrates cards, or prunes evidence. `KANBAN_DIR` does not select a client board.
- After the binding succeeded, in a Git checkout, writes the project reference into the project rules file of every built-in agent below the main worktree (`CLAUDE.md` for Claude, a one-line `@~/.agents/kander/KANDER-AGENTS.md` import; `AGENTS.md` for the others, a `## Kander Rules Entry` block that names `~/.agents/kander/KANDER-AGENTS.md` and tells a reader to skip it when that file does not exist). A missing file is created; a shared file is written once; a file that already references the entry is not changed; a reference to the retired `.kander/rules/KANDER-AGENTS.md` of a project install is replaced. The `install_rules` switches decide the files: a file that an agent with its switch off reads is not written (`snippets/commands/install.md`).
- A project rules file that resolves outside the main worktree, directly or through a symlink, is not written and is reported; a symlink inside the main worktree (`CLAUDE.md -> AGENTS.md`) is written in its target. The binding stands either way. Outside a Git checkout only the binding is written, with the reason.
- Runs no `git add`, `git commit`, or `git push`. The Kander rules apply in task worktrees and on other hosts only after the user commits and pushes the files.

- When the bound project has no project policy (revision 0), a call from a terminal (stdin and stderr are terminals, no `--json`, no `KANDER_EXECUTION_ID` or `KANDER_COORDINATOR_EXECUTION_ID`) runs the confirmation of `kander policy init`: it shows the values that would become the policy and writes them only on `y`. Any other call writes no policy and gives the `project_policy_unset` remark. A failed initialization is a `project_policy_init_failed` remark; the binding stands. `init` has no `--yes`; use `kander policy init --yes` (`snippets/commands/policy.md`).

## Output

- The service project binding, then the project's board path on the server host when the server reports one, then one line per project rules file written (or already referencing the entry) with the reminder to commit and push them, then the result of the policy step. With `--json`: `project_name`, `project_id`, `server_url`, `board_path` (omitted when the server reports none), and `rules_files` (the absolute paths of the project rules files written, omitted when none); the reminder is the `project_rules_commit` notice, a file not written is `project_rules_not_written`, `project_rules_disabled`, or `project_rules_not_git`, and the policy remark is in `notices`.

## Rejections

- More than one path, an unknown option, invalid configuration, incompatible protocol, unreachable service, or origin mismatch refuses binding.
- A project name must start with an ASCII letter and contain only ASCII letters, digits, `_`, or `-` (at most 128 characters); any other name is refused, never rewritten.
- A concurrent project overlay change refuses the save.

## Offline service maintenance

Pause every executor and external writer first, then run `kander server stop`: a running Agent starts the server again on its next command. Run `kander server maintain [--data DIR] --project NAME`. If active cards require form migration, add `--maintenance` only after all writers are paused. The command takes the exclusive server data-directory lock and never starts an HTTP listener or stops agents. It converts imported legacy file cards using the existing board redo journal. A repeat reports zero changes. Original import snapshots and evidence bytes stay intact. Keep writers paused after failure and retry the same maintenance command; never choose or delete a partial copy manually.

## Binding fields

The optional URL and name are validated with the normal configuration schema. The client checks protocol compatibility, registers the project with the checkout's `origin` URL, compared as exact text, and saves only the requested binding fields in the project's sparse `.kander-config.json`. A concurrent overlay edit refuses the save. Repeating the binding returns the same project. Different checkout paths do not affect project identity. A name already bound to another origin is refused.

Without these flags, `init` uses the existing binding at the selected project path. A missing project name fails with `kander init --project <name>`. An unreachable loopback service is started automatically, without a question, when the project board directory already exists in the default data directory, for every caller including Agents, `--json` and callers without a terminal; the command then continues and reports the start as a notice. Without that directory, an interactive caller is offered startup with the storage paths and default `[Y/n]`, and an Agent or noninteractive caller fails with `kander server start` and the data directory. When a process holds the data directory lock and the service stays unreachable, nothing is started and the command fails with an unreachable error that names the lock. Remote unavailable services fail and are never started. No path falls back to local storage.
