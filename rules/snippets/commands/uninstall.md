# kander uninstall

Source: new in this file

## Purpose and users

Removes the global Kander install of this host and stops its local server and background processes; the data directories stay. It stops the local server and the worker, removes what Kander wrote for the agents (the Kander load commands earlier releases wrote into the agents' global rules files, the agent rules extensions, and the managed `kander-leader` skill), deletes the rule files and state files of the rules root, and deletes the global `kander` binary last. A new install continues with the kept directories. Used by the user.

## Syntax

```text
kander uninstall [--yes]
```

## Arguments

- `--yes`: skip the confirmation. Required when standard input or standard error is not a terminal.

## Examples

```text
# Uninstall Kander from this host without the confirmation
kander uninstall --yes
```

## Preconditions

- An installed Kander.

## Effects

- Lists everything first: the processes it stops (the local server and the worker, each with its PID), every rules file it edits or deletes, every extension, skill file, and binary it deletes, the number of rule files it deletes from the rules root, every path it keeps or skips with the reason, the three kept data directories, and the unreleased leases of this host with their impact. On a terminal it then asks y/N (default N); any answer other than `y` changes nothing.
- Leases in use or running agents never refuse the uninstall. Each lease is listed with its project ID, task ID, generation, kind, and agent PID. While Kander is uninstalled these leases are not renewed and the `kander` commands their agents run fail; after a new install, once a worker runs again, the leases nobody took over renew again.
- Stops the loopback server at the configured `server.url` and the one the default server data directory recorded, the same way as `kander server stop`, when its administration key is readable; one whose key cannot be read is skipped and reported. A remote server is never touched.
- Stops the worker that holds `worker.lock` of the client home by writing the stop request `worker.stop` next to the lock, then waits up to 30 seconds for the lock to be free. It signals no process. The worker exits through its normal shutdown and deletes the request; a lock still held after the wait is a failed stop that names the lock file.
- A failed stop is reported and the other steps still run; the exit status is then 1.
- Drops every Kander load command from each embedded agent's existing global rules file and `AGENTS.override.md` sibling, whatever entry the command names: the same cleanup every other command runs once (`snippets/commands/install.md`). Other bytes stay, including comment text on the same line as a load command. A rules file left with only blank lines, or a `.mdc` rule left with only the `alwaysApply` frontmatter Kander wrote, is deleted; a rules file that is a symlink to the rules entry is removed as a link; `AGENTS.override.md`, other symlinked rules files, and files whose directory is reached through a symlink are only edited.
- Deletes each agent rules extension whose digest is a released version, and the unmodified managed leader skill files; a locally modified copy, a pre-existing skill, and a user link stay and are reported.
- Deletes the rule files and state files Kander installed below the rules root, including the global cleanup marker `kander-global-cleanup.json`, then the directories left empty. Any other file stays and is reported.
- Deletes the global entry last: the `kander` the installer copied into the global bin directory, after `<path> version` printed a line starting with `kander `. A Homebrew install is skipped with `brew uninstall kander`; a Microsoft Store install is skipped with the hint to uninstall it in Windows Settings. On Windows a running entry is renamed to `<name>.old` in its directory and a detached helper deletes it after this command exits; when the helper cannot start, the leftover path is printed. A failed skill removal keeps the binary, so a later run can finish the bundle.
- Never follows a symlink, junction, or other reparse point to delete a file.
- Deletes nothing in the configuration directory, the share directory with the client home, and the server data directory, and writes nothing there except `worker.stop`. Never changes `config.json`. Runs no Git command: the project references committed in repositories, the task branches, and the evidence refs stay (run `kander gc` first to reclaim the local ones).

## Output

- The plan on standard output, the y/N prompt on standard error, then one line per change, and last the list of what stays.
- Exit 0 on success, 1 when cancelled or when a step failed, 2 for a usage error or a missing `--yes` without a terminal.
- With `--json` and `--yes`: `mode` (always `global`), `rules_dir`, `items` (array of `action` (`edit`, `remove`, `stop`, `keep`, `skip`), `path` for a file, `process` and `pid` for a process, and `reason`), `leases` (array of `project_id`, `task_id`, `kind`, `generation`, and `agent_pid`), and `failures` (array of messages, empty on success; exit status 1 otherwise). `--json` without `--yes` is refused with `interactive_required` and exit status 2 before anything changes.

## Rejections

- Any argument other than `--yes` is a usage error.
