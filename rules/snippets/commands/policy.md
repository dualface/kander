# kander policy

Source: new in this file

## Purpose and users

Shows, initializes, and changes the project policy: the five configuration values the server keeps per project for every client. They are `git.branch_mode`, `review_stages` (`large` and `small`, each with `PMQA` and `Security`), `rules` (the module switches), `agent_language`, and `issue.result_on_done`. Every other key stays host configuration. Used by the user; any client bound to the project may change the policy.

## Syntax

```text
kander policy show
kander policy init [--yes]
kander policy set <key> <value>
```

## Arguments

- `show`: no arguments.
- `init --yes`: write without the confirmation question.
- `set <key> <value>`: `<key>` is one leaf path of the policy: `git.branch_mode`, `review_stages.<large|small>.<PMQA|Security>`, `rules.<module>` (a module this version knows), `agent_language`, or `issue.result_on_done`. `<value>` is `true` or `false` for a `rules.<module>` key and the text of the value for the others; the server checks it with the configuration rules.

## Examples

```text
# Show the project policy
kander policy show

# Upload the values in force on this host as the first project policy
kander policy init

# Make the Security review required for large cards on every client
kander policy set review_stages.large.Security required
```

## Preconditions

- A checkout bound to a project (`kander init`) and a reachable server that keeps project policies. A server without them (its project answer has no `policy` field) is refused with `policy_unsupported`: upgrade the server.

## Effects

- `show` reads the policy. Every form refreshes the local policy cache with what the server answered.
- `init` works only while the project has no policy (revision 0). It uploads the policy values this host has before any policy applies (the scope `config.json`, the project `.kander-config.json`, and the defaults) as revision 1. A call from a terminal (stdin and stderr are terminals, no `--json`, no `KANDER_EXECUTION_ID` or `KANDER_COORDINATOR_EXECUTION_ID`) shows the values and asks `[y/N]` unless `--yes` is given; any answer but `y` writes nothing and exits 1.
- `set` reads the current policy, changes the one value, and writes the whole policy back with the revision it read (compare-and-swap). Keys of the policy that this version does not know are written back unchanged. A refused write is never retried.
- A write sends the Git `user.name` of the current directory as the submitter; an embedding Attribution hook replaces it. The server appends one audit line per write: epoch, revision, time, submitter, client ID, idempotency key, old values, and new values.
- Once the policy has a revision above 0, every command that opens the project uses its values instead of the same keys of `config.json` and `.kander-config.json`; see `snippets/commands/config.md` for how `kander config` shows this.

## Output

- `show`: the revision and epoch, the last submitter and time, and one `<path> = <value>` line per value; a project without a policy prints that it has none. With `--json`: the policy object of `GET /api/v1/projects/{project}/policy`: `revision`, and when it is above 0 also `epoch`, `values`, `updated_at`, and `updated_by`.
- `init` and `set`: the new revision and epoch. With `--json`: the policy object the write produced.
- A key of the policy this version does not know is a `project_policy_ignored` remark. A failed cache write is a `project_policy_cache_failed` remark and does not fail the command.

## Rejections

- An unknown subcommand, a wrong number of arguments, an unknown option, a `set` key outside the list above, or a `rules.<module>` value other than `true` or `false` is a usage error, exit 2.
- `init` without `--yes` when the call is not from a terminal (including every `--json` call) is a usage error, exit 2, before anything is read or written.
- `init` when the policy already has a revision above 0 fails with `policy_already_set`; use `set`. `set` while the project has no policy fails with `project_policy_unset`; use `init`.
- A write whose revision is no longer the current one fails with `policy_revision_conflict` (HTTP 409) and changes nothing; read the policy again and repeat the change.
- Values the server refuses (a missing value, a key it does not know, a value outside the configuration rules) fail with `invalid_request` (HTTP 400) and change nothing.
