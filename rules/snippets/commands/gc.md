# kander gc

Source: new in this file

## Purpose and users

Reclaims the local client data that finished cards left on this host. That is their task worktrees and task branches, the review runtime copies of finished review runs, finished journal records, and the evidence refs left on the common remote. When the bound server runs on this host and its administration key is readable, it also runs `kander server gc` in the same plan and confirmation. Used by the user.

## Syntax

```text
kander gc [--dry-run] [--yes] [--older-than D] [--all-projects] [--client-only]
```

## Arguments

- `--dry-run`: print the plan and change nothing: no file below the client directory except lock files, no `git fetch`, no ref, object, or worktree of the user's repositories, and nothing on the common remote.
- `--yes`: run without asking. Without a terminal, or with `--json`, `gc` needs `--yes` or `--dry-run`.
- `--older-than D`: the retention period: a Go duration or a whole number of days such as `7d`; at least `1h`. Default: `7d`. Also passed to the server gc.
- `--all-projects`: cover every client project directory of this host that matches a project of the bound server, instead of the bound project only. The project names come from the server gc plan, so without a server gc (`--client-only`, a server that is not on a loopback address, or an unreadable key) only the bound project matches. Unmatched directories are listed and never changed.
- `--client-only`: never call the server gc.

## Examples

```text
# Show what a gc of the bound project would reclaim
kander gc --dry-run

# Reclaim every project of this host without asking, keeping two weeks
kander gc --yes --all-projects --older-than 14d
```

## Preconditions

- A checkout bound to a project (`server.url` and `project.name`) and a reachable server: every card state is read through the service, never from local files.
- The server gc part needs a loopback `server.url` and the administration key of the server's data directory, read the way `kander server stop` reads it.

## Effects

A card is terminal in `done`, `archived` or `trash`. The categories run in this order for each project; a candidate that fails a protecting condition is kept and reported with the reason.

- Worktrees and task branches: a generation's worktree `worktrees/<task-id>/g<n>` is a candidate when its card is terminal and finished before the retention period (the directory's modification time when the card has no finish time). It is removed only when all of these hold: no other process holds its worktree lock (taken without waiting, held until it is done); the card read again after the lock is still terminal; this host has no standing claim and no unreleased execution credential for that generation; every retained executor of the generation reads `stopped` in the liveness probe (alive or unknown keeps it: run `kander dismiss <task-id>` first); Git registers the worktree with its recorded task branch checked out and, for the bound project, in the repository of the current checkout; `git status --untracked-files=all` is empty; and the branch tip is the base commit of the generation, is contained in `origin/<integration target>`, or is the card's published delivery commit that an evidence ref on the common remote still names. The integration target is the card's `INTEGRATION_TARGET`, else the mainline branch. A run fetches the integration target first; a dry run only lists it and reports a worktree whose target commit is not present locally as decided after the fetch, without counting it. Removal runs `git worktree remove` (never forced), deletes the branch only when its name is `kander-task/<project-id>/<task-id>/g<n>` or the legacy `kander/<project-id>/<task-id>/g<n>`, deletes the worktree intent, removes `worktrees/<task-id>/` once no generation is left, and runs `git worktree prune` once per repository.
- Review runtime: `review-runtime/<run-id>/` goes when the server reports the run finalized or abandoned, or no longer knows it. A run the server no longer knows keeps its directory while `pending-finalization.json` is there. Any other server failure keeps the directory and is an error. The run's `runtime.lock` is taken without waiting.
- Journal: a record of `launch-actions/` in phase `complete`, or of `request-calls/` in phase `complete` or `failed`, goes when its card is terminal and the record is older than the retention period. It goes under the lock its writer takes; the empty lock file stays. Worktree intents go only with their worktree. `review-operations/`, `review-material/`, `continuations/` and `continuation-intents/` carry no terminal mark and are never touched.
- Evidence refs: the refs below `refs/heads/kander/<project-id>/` that the service's evidence cleanup scope of a terminal card or a settled task group owns (scope `Ready`) are deleted through the same completion cleanup `kander move ... done` runs; no other ref is deleted. A run then prunes the matching remote-tracking refs with `git fetch --prune origin 'refs/heads/kander/<project-id>/*:refs/remotes/origin/kander/<project-id>/*'`.
- Server gc: the plan asks the server for its plan first, then plans the client part, and prints both with one confirmation. The run removes the client part first, then runs the server gc. A refused server gc (HTTP 403, or a gc already running) or a failed one keeps the server part and the client part still runs.
- Running `gc` again changes nothing it already removed. Never touched: cards, boards, `executions/`, `retained-executions/`, `worker.log`, other branches and refs, and the client directory of a project that matches no project of the bound server.

## Output

- The plan on standard output, by project and category, with counts and bytes, each kept candidate with its reason, the deferred worktrees, the unmatched directories, and the server part; the y/N prompt (default N) on standard error.
- Exit 0 when every candidate was removed or there was none; 1 when the user cancelled, a candidate was kept, the server part was refused or failed, or the run met an error (an unreachable server, a failed read, removal or Git command, a timeout); 2 for a usage error, an invalid value, or a missing confirmation. The highest applicable status wins.
- With `--json` (and `--yes` or `--dry-run`): `dry_run`, `projects` (array of `name`, `id`, `worktrees`, `branches`, `review_runtime`, `journal`, `evidence_refs`, optional `skipped` and `errors`), optional `unmatched` (client project directory names), `server` (optional `skipped`, `refused`, `error`, and `report`, the JSON result of `kander server gc`), and optional `errors`. Each category has `count`, `bytes`, optional `kept` and, for `worktrees`, optional `deferred`; each entry is `{id, reason}`.

## Rejections

- Any positional argument, an unknown flag, or an `--older-than` that is not a duration or is below `1h` is a usage error, exit 2.
- Without a terminal and without `--yes` or `--dry-run`, exit 2. `--json` without `--yes` or `--dry-run` is refused with `interactive_required`, exit 2, before anything is read or changed.
