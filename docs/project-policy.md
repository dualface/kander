# Project policy

A v2 server keeps one *project policy* per project: the configuration values every client of the project must share. Before it existed each client read only its own scope `config.json` and the project overlay `.kander-config.json`, so one project could run with a different branch model, review stages, or rule modules on each host. Committing `.kander-config.json` to Git did not close that gap: an uncommitted local edit still applied, the main worktree read another copy after a branch switch, and the server still trusted whatever the client sent. The server is now the single source of these values, and every client uses them.

The command contract is `rules/snippets/commands/policy.md`; how `kander config` shows the policy is in `rules/snippets/commands/config.md`.

## Scope

The policy holds exactly five values, in the shapes of the configuration file:

| Key | Values |
| --- | --- |
| `git.branch_mode` | `main-develop` or `main-only` |
| `review_stages` | `large` and `small`, each with `PMQA` and `Security`: `auto`, `skip`, or `required` |
| `rules` | the module switches (`collaboration`, `code`, `git`, `review`, `task_intake`, `task_groups`, `reporting`) |
| `agent_language` | the language agents use with the user |
| `issue.result_on_done` | `auto`, `ask`, or `off` |

Everything else is *host configuration* and stays per host: execution and review agents, models and reasoning efforts, the launcher, the TUI, the server URL, and the project binding. Hosts differ in installed agents and environment, so the user decided not to share them.

Terms used below:

- **Policy revision**: a non-negative integer. 0 means the project has no policy (*unset*). Every successful write adds 1.
- **Policy epoch**: a random ID the server chooses when the first write of a project succeeds (revision 0 to 1). Later writes keep it. A project that is removed and imported again starts a new policy with a new epoch.
- **Policy version**: the pair (epoch, revision). Version B is *newer* than version A when the epochs differ, or when they are equal and B has the larger revision.

## Server

The policy lives in the control directory of the project, beside the board, so a board import or replacement never touches it:

```text
projects/<name>-<hash>/control/
  policy.json          the current policy: epoch, revision, values, updated_at, updated_by
  policy-audit.jsonl   one line per write and per void
```

Routes (both through the Authorize hook of an embedding host, like every project route):

| Route | Purpose |
| --- | --- |
| `GET /api/v1/projects/{p}/policy` | The policy. Unset: `{"revision": 0}` with no epoch and no values. Set: `epoch`, `revision`, `values`, `updated_at`, `updated_by` |
| `PUT /api/v1/projects/{p}/policy` | Body `{"expected_revision": N, "values": {...}, "submitter": "..."}` with an `Idempotency-Key` |

The project answers of `GET /api/v1/projects?name=`, `POST /api/v1/projects`, and `GET /api/v1/projects/{p}` carry the policy in the field `policy` (revision 0 when unset), so a client gets it with its registration and sends no second request. A client reads an answer without `policy` as a server without project policies. The routes and the field are additive, as earlier additive routes were: protocol 3 stays.

A write is a compare-and-swap on the revision, under the project lock:

1. The values are validated strictly with the configuration validators (`config.ValidatePolicyValues`): all five values must be present, a key this version does not know anywhere in the document is refused and listed, and every value must pass the configuration rules (`git.branch_mode` and `issue.result_on_done` choices, the `review_stages` structure and modes, the rule switches and the `task_groups`-needs-`git` rule, the `agent_language` text rule). A flat `review_stages` object is first normalized to the two-scale form, which must then hold both scales with both roles. A rule module this version knows and the values leave out takes its default (on). A refused document is `invalid_request` (HTTP 400) and writes nothing.
2. `expected_revision` must equal the current revision; otherwise `policy_revision_conflict` (HTTP 409) and nothing is written.
3. The audit line is appended and synced: epoch, revision, time, submitter, client ID, idempotency key, old values, new values.
4. `policy.json` is replaced atomically, and the new policy is the answer.

The submitter is the value of an Attribution hook when the host has one; otherwise the `submitter` the client reports (the Git `user.name` of its current directory, as for `kander new`), checked like the `SUBMITTER` of a card. It is an audit hint, not an authentication.

**Crash recovery.** A crash after the audit append and before the replacement leaves an audit line for a write that never took effect. When the server opens the project it reads `policy.json` as the truth and appends a void line (`{"void": {"epoch", "revision", "idempotency_key"}, "reason": "not_applied"}`) for every audit line that `policy.json` does not contain (another epoch, or a revision above the current one) and no void line cancels yet; it never rewrites an earlier line. A last line without its newline, the torn tail of an interrupted append, is ended first. When a write fails in the running server after its audit append, the server appends the void line at once; if that append fails too, the project refuses policy writes with `not_ready` until a restart recovers it. Offline `kander server maintain` reads the policy but writes no policy file.

**Idempotency.** A write uses the receipt protocol of every other write (operation `project-policy`). When a retry finds its receipt still pending, the write took effect exactly when the audit log holds a line with its idempotency key that no void line cancels. The retry then answers the policy that line recorded, the first result, even when later writes changed the policy since; it never answers `policy_revision_conflict` and never appends a second line. Without such a line the write runs again.

**Import.** `kander server import` into a new project leaves its policy unset; an import into an existing project keeps the policy it has.

## Client

### Cache

The client keeps the last policy it read per server and project in the private client directory (`KANDER_CLIENT_HOME`, else `client/` below the global share directory): `policies/<sha256(origin, project ID)>.json`. The origin is the canonical server URL (scheme and host in lower case, no trailing slash). The project ID comes from the project name (`config.ProjectIDForName`, the same derivation as `api.ProjectID`) for `config.Load`, and from the execution credential for the background worker (`remote.CredentialPolicy`). Files and the directory are private to the user, symlinks and reparse points are refused, and writes are atomic.

The *policy refresh function* (`remote.RefreshPolicy`, built on `config.StorePolicy`) writes a policy a server answered only when its policy version is newer than the cached one, under the lock of the cache file, so an older answer of a concurrent process never replaces a newer cache. An unset answer replaces a cached policy with an unset record, so the values of a project that was removed and imported again are not kept. A cache that cannot be read or does not validate counts as no cache and the next refresh replaces it. Later features reuse this function and never write the cache another way.

### Merge order

For the five policy values the order is: cached policy (revision above 0) > project overlay > scope `config.json` > defaults. Every other key keeps the old order (overlay > scope > defaults).

- `config.Load` applies the cache last, without network access. A configuration without a project binding has no policy.
- `remote.Open` refreshes the cache from the registration answer and applies the answer to the configuration the command passed in, so the rest of the command uses the server's values. An unset policy, or a server without project policies, keeps the local values with the `project_policy_unset` remark; a failed cache write is only a remark.
- A key of the policy this version does not know (a newer rule module, for example) is ignored with a `project_policy_ignored` remark; the policy still applies.
- No write of a configuration file takes the policy into account: Options saves, `Save`, `Update`, doctor repair, and the sparse writes of `kander init` start from the content of the file itself, as they already did to keep overlay values out of `config.json`. A policy value therefore never lands in `config.json` or `.kander-config.json`.

### Remarks

| Code | Meaning |
| --- | --- |
| `project_policy_unset` | The project has no policy; the local values apply. Run `kander policy init` |
| `project_policy_cache_missing` | `kander config` found no cache yet; the next command that opens the project fills it |
| `project_policy_cache_invalid` | The cache (or the server's values) cannot be used; the local values apply |
| `project_policy_ignored` | A policy key this version does not know is ignored |
| `project_policy_override` | A value of `config.json` or `.kander-config.json` differs from the policy, which applies; listed by `kander config` and `kander doctor`, never written back |
| `project_policy_cache_failed` | The cache could not be written; the command continues |

`kander config --json` adds the top-level object `project_policy`: `epoch`, `revision`, and `source` (`server_cache` when the cached policy applies, `local` otherwise).

## Initialization and changes

A project starts unset. While it is, every client works with its local values and gets the `project_policy_unset` remark. `kander policy init` uploads the values this host has before any policy (scope, overlay, defaults) as revision 1, after showing them and asking when it runs in a terminal; `--yes` skips the question and is required otherwise. `kander init` offers the same question when it binds a project that has no policy, and only gives the remark when nobody can answer. `kander policy set <key> <value>` changes one value with the revision it read and keeps every other key, those it does not know included. Any client bound to the project may change the policy; limiting that is the job of the embedding host's Authorize hook.

## Read paths for embedding hosts

The policy is readable through two routes: `GET /api/v1/projects/{p}/policy` and the project answers, among them the registration `POST /api/v1/projects`, which is not a per-project route. A host that restricts reading the policy must apply the same restriction to the registration and project lookup routes. The policy holds no credential.

## Later work

The server does not yet enforce the policy on review batches or claims, and long-running processes (TUI, worker, `kander subscribe`) do not yet refresh it on change; both are separate task cards of the same group.
