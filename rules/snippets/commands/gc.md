# kander gc

Source: new in this file

## Purpose and users

Reclaims the local client data that finished cards left on this host. That is their task worktrees and task branches, the review runtime copies of finished review runs, finished journal records, and the evidence refs left on the common remote. When the bound server runs on this host and its administration key is readable, it also runs `kander server gc` in the same plan and confirmation. Used by the user.

## Syntax

```text
kander gc [--dry-run] [--yes] [--older-than D] [--all-projects] [--client-only]
```

## Arguments

- `--dry-run`: print the plan and change nothing: no file below the client directory except lock files, no `git fetch` (Git runs with `GIT_NO_LAZY_FETCH=1`, so a partial clone fetches no missing object; with a Git older than 2.45, which cannot be kept from fetching lazily, a worktree of a partial clone is deferred without any probe; a repository counts as one when it names `extensions.partialClone` or enables a promisor remote in any form Git reads as true, a value Git would reject included), no ref, object, or worktree of the user's repositories, and nothing on the common remote.
- `--yes`: run without asking. Without a terminal, or with `--json`, `gc` needs `--yes` or `--dry-run`.
- `--older-than D`: the retention period: a Go duration or a whole number of days such as `7d`; at least `1h`. Default: `7d`. Also passed to the server gc.
- `--all-projects`: cover every client project directory of this host that belongs to a project of the bound server, instead of the bound project only. The directory's identity marker `project.json` and its own records (execution credentials, retained executions, request-call records, task branches of its worktree intents) name its project ID, or, for a directory without them, a project of the server gc plan has its namespace; `GET /api/v1/projects/{project}` confirms it, also without a server gc. A client command writes the marker when it first uses the directory and the gc never removes it, so a directory stays matchable after its last record went; the marker is only a lead and grants nothing the server's answer does not. A directory of another server or of a project the server does not have is listed as unmatched; one that names no project or whose project cannot be read is listed as unverified. Neither is changed.
- `--client-only`: never call the server gc.

## Examples

```text
# Show what a gc of the bound project would reclaim
kander gc --dry-run

# Reclaim every project of this host without asking, keeping two weeks
kander gc --yes --all-projects --older-than 14d
```

## Preconditions

- A checkout bound to a project (`server.url` and `project.name`) and a reachable server: every card state is read through the service, never from local files. `gc` only reads to open the project: it creates no client identity, starts no server or credential worker, and registers nothing. A host without a client identity still plans the evidence refs and the server part; local data of a project there is reported as not cleanable, because this host's claims and credentials cannot be told apart.
- The server gc part needs a loopback `server.url` and the administration key of the server's data directory, read the way `kander server stop` reads it.

## Effects

A card is terminal in `done`, `archived` or `trash`. The categories run in this order for each project; a candidate that fails a protecting condition is kept and reported with the reason. Before them, a run (never a dry run or a declined confirmation) writes the identity marker into a project directory created before markers existed; when that fails, or an existing marker names another project (it is never rewritten), nothing below the directory is removed and the error exits 1. A dry run reports such a marker the same way and plans nothing below its directory.

- Worktrees and task branches: a generation's worktree `worktrees/<task-id>/g<n>` is a candidate when its card is terminal and finished before the retention period (the directory's modification time when the card has no finish time). It is removed only when all of these hold: no other process holds its worktree lock (taken without waiting, held until it is done); the card read again after the lock is still terminal; this host has no standing claim and no unreleased execution credential for that generation; every retained executor of the generation reads `stopped` in the liveness probe, which searches a stale recorded pane's session by reverse lookup; a stale pane of a session without a recorded reference, and an executor without a terminal container, read unknown (alive, drifted or unknown keeps it: run `kander dismiss <task-id>` first); the command's current directory, compared after resolving symbolic links, is not inside the worktree; Git registers the worktree with its recorded task branch checked out and, for the bound project, in the repository of the current checkout; `git status --untracked-files=all` is empty; and the branch tip is the base commit of the generation, is contained in `origin/<integration target>`, or is the card's published delivery commit that an evidence ref on the common remote still names; the last two count only when the repository's `origin` is the project's common remote. The integration target is the card's `INTEGRATION_TARGET`, else the mainline branch. A run fetches the integration target first, into its tracking ref only (no tags, no configured refmap, no pruning); a dry run only lists it and reports a worktree it cannot judge without the missing objects as decided after the fetch, without counting it. Removal first records the removal in `gc-removals/<task-id>/g<n>.json`, runs `git worktree remove` (never forced; a Git error is reported and exits 1; with the directory gone the removal goes on only once Git no longer registers the worktree, after one `git worktree prune`, and otherwise the record, the branch and the intent stay for the next run; an error whose directory cannot be checked keeps the record), deletes the branch only when its name is `kander-task/<project-id>/<task-id>/g<n>` or the legacy `kander/<project-id>/<task-id>/g<n>` and it still has the recorded tip, deletes the worktree intent and then the record, removes `worktrees/<task-id>/` once no generation is left, and runs `git worktree prune` once per repository. The next run finishes a recorded removal that was interrupted.
- Review runtime: `review-runtime/<run-id>/` goes when the server reports the run abandoned, or finalized and published into every member card, or no longer knows it. A finalized run that still has to be published keeps its directory: the publication needs its writer secret. A run the server no longer knows keeps its directory while `pending-finalization.json` is there. Any other server failure keeps the directory and is an error. The stable lock `review-runtime/<run-id>.lock`, which a reviewer takes first and the gc never removes, and the run's `runtime.lock` are taken without waiting, and the server state and the pending record are read while they are held.
- Journal: a record of `launch-actions/` in phase `complete`, or of `request-calls/` in phase `complete` or `failed`, goes when its card is terminal and the record is older than the retention period. It goes under the lock its writer takes; the empty lock file stays. A record that cannot be read or decoded is kept with the reason. Worktree intents go only with their worktree. `review-operations/`, `review-material/`, `continuations/` and `continuation-intents/` carry no terminal mark and are never touched.
- Evidence refs: the refs below `refs/heads/kander/<project-id>/` that the service's evidence cleanup scope of a terminal card or a settled task group owns (scope `Ready`) are deleted through the same completion cleanup `kander move ... done` runs; no other ref is deleted. Integration source refs are asked about through the scopes of every terminal card and group. This category needs only a checkout, so it also runs for the bound project when this host has no client directory for it.
- Tracking refs: a local ref below `refs/remotes/origin/kander/<project-id>/` whose remote ref is gone is deleted with `git update-ref -d` and the value it was read with; no fetch runs.
- Server gc: the plan asks the server for its plan first, then plans the client part, and prints both with one confirmation. The run removes the client part first, then runs the server gc. A refused server gc (HTTP 403, or a gc already running) or a failed one keeps the server part and the client part still runs.
- Running `gc` again changes nothing it already removed. Never touched: cards, boards, `executions/`, `retained-executions/`, `worker.log`, other branches and refs, and an unmatched or unverified client project directory.

## Output

- The plan on standard output, by project and category, with counts and bytes (0 B for branches and refs), each kept candidate with its reason, the deferred worktrees, the unmatched and unverified directories, and the server part; the y/N prompt (default N) on standard error. A plan whose only work is a deferred worktree, an emptied task directory, or an interrupted removal still asks and runs.
- Exit 0 when every candidate was removed or there was none; 1 when the user cancelled, a candidate was kept, a directory was unverified, the server part was refused or failed, or the run met an error (an unreachable server, a failed read, removal or Git command, a timeout); 2 for a usage error, an invalid value, or a missing confirmation. The highest applicable status wins.
- With `--json` (and `--yes` or `--dry-run`): `dry_run`, `projects` (array of `name`, `id`, `worktrees`, `branches`, `review_runtime`, `journal`, `evidence_refs`, `tracking_refs`, optional `skipped` and `errors`), optional `unmatched` (client project directory names), optional `unverified` (`{id, reason}` per directory), `server` (optional `skipped`, `refused`, `error`, and `report`, the JSON result of `kander server gc`), and optional `errors`. Each category has `count`, `bytes` (always 0 for `branches`, `evidence_refs` and `tracking_refs`), optional `kept` and, for `worktrees`, optional `deferred`; each entry is `{id, reason}`.

## Rejections

- Any positional argument, an unknown flag, or an `--older-than` that is not a duration or is below `1h` is a usage error, exit 2.
- Without a terminal and without `--yes` or `--dry-run`, exit 2. `--json` without `--yes` or `--dry-run` is refused with `interactive_required`, exit 2, before anything is read or changed.
