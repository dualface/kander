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
| `PUT /api/v1/projects/{p}/policy` | Body `{"expected_revision": N, "values": {...}, "submitter": "..."}` with an `Idempotency-Key` and a `Kander-Client-Id`. `expected_revision` is required: a missing or `null` value is `invalid_request`, and only an explicit `0` initializes the policy |

The project answers of `GET /api/v1/projects?name=`, `POST /api/v1/projects`, and `GET /api/v1/projects/{p}` carry the policy in the field `policy` (revision 0 when unset), so a client gets it with its registration and sends no second request. A client reads an answer without `policy` as a server without project policies. The routes and the field are additive, as earlier additive routes were, and did not change protocol 3 on their own; the enforcement below raised the protocol to 4.

A write is a compare-and-swap on the revision. The request is checked before its idempotency receipt is written, so a refused request leaves no receipt and a corrected request may reuse its key; the rest runs under the project lock:

1. The values are validated strictly with the configuration validators (`config.ValidatePolicyValues`): all five values must be present, a key this version does not know anywhere in the document is refused and listed, and every value must pass the configuration rules (`git.branch_mode` and `issue.result_on_done` choices, the `review_stages` structure and modes, the rule switches and the `task_groups`-needs-`git` rule, the `agent_language` text rule). A flat `review_stages` object is first normalized to the two-scale form, which must then hold both scales with both roles. A rule module this version knows and the values leave out takes its default (on). A refused document is `invalid_request` (HTTP 400) and writes nothing.
2. `expected_revision` must equal the current revision; otherwise `policy_revision_conflict` (HTTP 409) and nothing is written.
3. The submitter is checked (only for a write that runs: a retry whose receipt exists replays the first result whatever the Attribution hook answers now). The audit line of this write attempt is appended and synced: a random attempt ID, epoch, revision, time, submitter, client ID, idempotency key, old values, and new values. Every field is written: an empty submitter as `""` and the old values of a first write as `null`.
4. `policy.json` is replaced atomically, and the new policy is the answer.

The submitter is the value of an Attribution hook when the host has one; otherwise the `submitter` the client reports (the Git `user.name` of its current directory, as for `kander new`, empty when there is none), checked like the `SUBMITTER` of a card. It is an audit hint, not an authentication. A set policy always carries `updated_by`, empty included; an unset one answers only `{"revision": 0}`.

**Crash recovery.** A crash after the audit append and before the replacement leaves an audit line for an attempt that never took effect. When the server opens the project it reads `policy.json` as the truth and appends a void line (`{"void": {"attempt", "epoch", "revision", "idempotency_key"}, "reason": "not_applied"}`) for every attempt that `policy.json` does not contain (another epoch, or a revision above the current one) and no void line cancels yet; it never rewrites an earlier line. A void cancels exactly one attempt, so a later attempt with the same key and revision is judged on its own. A last line without its newline, the torn tail of an interrupted append, is ended first. When the audit append or the replacement fails in the running server, the line may already be in the log (a failed sync, for example), so the server appends the void line of that attempt at once; if that append fails too, the project refuses policy writes with `not_ready` until a restart recovers it. Offline `kander server maintain` neither reads nor writes the policy files, so a damaged one never blocks a board maintenance.

**Idempotency.** A write uses the receipt protocol of every other write (operation `project-policy`). When a retry finds its receipt still pending, the write took effect exactly when the audit log holds an attempt with its idempotency key that no void line cancels. The retry then answers the policy that line recorded, the first result, even when later writes changed the policy since; it never answers `policy_revision_conflict` and never appends a second line. Without such a line the write runs again.

**Import.** `kander server import` into a new project leaves its policy unset; an import into an existing project keeps the policy it has.

## Client

### Cache

The client keeps the last policy it read per server and project in the private client directory (`KANDER_CLIENT_HOME`, else `client/` below the global share directory): `policies/<sha256(origin, project ID)>.json`. The origin is the canonical server URL (scheme and host in lower case, no trailing slash). The project ID comes from the project name (`config.ProjectIDForName`, the same derivation as `api.ProjectID`) for `config.Load`, and from the execution credential for the background worker (`remote.CredentialPolicy`). Files and the directory are private to the user, symlinks and reparse points are refused, and writes are atomic.

The *policy refresh function* (`remote.RefreshPolicy`, built on `config.BeginPolicyRefresh`) runs the request that answers the policy (the registration of `remote.Open`, `GET .../policy`, or the policy write) while it holds the lock of that project's cache file, and stores the answer before it releases the lock, when its policy version is newer than the cached one. The refreshes of one cache key therefore run one at a time: a request starts only after every earlier refresh of the key stored its answer, so its answer comes from a later state of the server and is the newest the key has seen. That orders answers of different epochs and unset answers, which the version alone cannot: an older answer of a concurrent process never replaces a newer cache, whichever process finishes first, while a real unset transition and a new epoch after a re-import apply. An unset answer replaces a cached policy with an unset record, so the values of a project that was removed and imported again are not kept. The wait for the lock is short: at most 2 seconds, and at most a quarter of the time the command has left, so a command with a short deadline (the TUI start, `kander check`) keeps most of its time for the request. When another process holds the lock longer, waiting for a slow answer, the request still runs without it and its answer applies to the command, but nothing is stored, and the failed cache write is only a remark. An explicit cancellation before the lock ends the refresh without its request. `remote.Open` sends no separate policy request: the registration itself runs under the lock. The cache only serves offline reads such as `kander config`; it is never a basis the server trusts. A cache that cannot be read or does not validate counts as no cache and the next refresh replaces it. Later features reuse this function and never write the cache another way.

### Merge order

For the five policy values the order is: cached policy (revision above 0) > project overlay > scope `config.json` > defaults. Every other key keeps the old order (overlay > scope > defaults).

- `config.Load` applies the cache last, without network access. A configuration without a project binding has no policy.
- `remote.Open` refreshes the cache from the registration answer and applies the answer to the configuration the command passed in, so the rest of the command uses the server's values. The answer also stays in the process for the running command (`config.RememberServerPolicy`, cleared when `command.Execute` starts the next command): every later `config.Load` of that command uses it, also when the cache could not be written. `kander issue import` without `--language` takes the agent language of the opened session. An unset policy, or a server without project policies, keeps the local values with the `project_policy_unset` remark, made once per command even when it opens the project twice; a server without project policies counts as unset for every later read of the command, whatever the cache holds. A failed cache write is only a remark.
- A key of the policy this version does not know (a newer rule module, for example) is ignored with a `project_policy_ignored` remark; the policy still applies.
- No write of a configuration file takes the policy into account: Options saves, `Update`, doctor repair, and the sparse writes of `kander init` start from the content of the file itself, as they already did to keep overlay values out of `config.json`, and `Save` refuses a configuration that holds more than the scope file (a `Load` result with an overlay or a policy, a configuration a server policy was applied to, and the display of `kander config`) with `config.save_layered_config`; change the scope file through `LoadScope` or `Update`. A policy value therefore never lands in `config.json` or `.kander-config.json`.

### Remarks

| Code | Meaning |
| --- | --- |
| `project_policy_unset` | The project has no policy; the local values apply. Run `kander policy init` |
| `project_policy_cache_missing` | `kander config` found no cache yet; the next command that opens the project fills it |
| `project_policy_cache_invalid` | The cache (or the server's values) cannot be used; the local values apply |
| `project_policy_ignored` | A policy key this version does not know is ignored |
| `project_policy_override` | A value of `config.json` or `.kander-config.json` differs from the policy, which applies; listed by `kander config` and `kander doctor` (also as a notice with `--json`), never written back |
| `project_policy_cache_failed` | The cache could not be written; the command continues |

`kander config --json` adds the top-level object `project_policy`: `epoch`, `revision`, and `source` (`server_cache` when the cached policy applies, `local` otherwise).

## Initialization and changes

A project starts unset. While it is, every client works with its local values and gets the `project_policy_unset` remark. `kander policy init` uploads the values this host has before any policy (scope, overlay, defaults) as revision 1, after showing them and asking when it runs in a terminal; `--yes` skips the question and is required otherwise. `kander init` offers the same question when it binds a project that has no policy, and only gives the remark when nobody can answer. `kander policy set <key> <value>` changes one value with the revision it read and keeps every other key, those it does not know included. Any client bound to the project may change the policy; limiting that is the job of the embedding host's Authorize hook.

## Read paths for embedding hosts

The policy is readable through two routes: `GET /api/v1/projects/{p}/policy` and the project answers, among them the registration `POST /api/v1/projects`, which is not a per-project route. A host that restricts reading the policy must apply the same restriction to the registration and project lookup routes. The policy holds no credential.

## Enforcement

The server enforces the policy on new review batches and on first claims. It validates what the client sends and computes nothing for it.

### Review batches

A *new batch* is one that `kander review --requirements-file`, `kander review plan`, or `kander review extend-plan` creates; a batch that already exists is not new. Only for a new batch, under the project lock and before any receipt is written:

1. The request carries `policy_revision`, which the client fills in from its policy. When the policy revision is above 0 and differs, the server answers `policy_revision_stale` (HTTP 409) and writes nothing: no batch, run, staging file, or idempotency receipt.
2. When the revision is 0 or the policy's `rules.review` is false, no role is checked. With the review module off no review is requested automatically; `kander review` is then only an explicit call.
3. Otherwise the scale comes from the members' `SIZE`: `large` when any member is large; a card without `SIZE` counts as small in the file form and large in the directory form. For `PMQA` and `Security`, when `review_stages.<scale>.<role>` is `required` and the requirement is `N/A: <reason>`, the request must name for that role the ID of a decision record (`kander decision record`, human or leader kind) that exists on one of the members; otherwise `policy_requirement_unmet` (HTTP 400), with a message that lists each failing role and why (no decision, or an ID on no member). A `skip` role written `required`, and an `auto` role with any valid value, pass without a decision. Extra roles in the decisions pass and are not recorded.
4. On success the batch record stores `policy_revision` (omitted when 0) and `requirement_decisions` (only the decisions the check used; omitted when none).

The decisions come from `--requirement-decision <ROLE>=<decision-id>` (repeatable, only with `--requirements-file`) or from `requirement_decisions` of a batch of `review plan` and of the `batch` of `review extend-plan`. Existing batches, historical four- and six-role batches included, are never checked again: reads, retries, incremental runs, and closing behave as before, and those batches have neither field. The policy revision is not part of the run identity (the `ReviewPrepare` identity digest), the plan and extend-plan idempotency receipts, or the joint publication material, so a retry of a run ID after a policy change returns the first result and never `idempotency_mismatch`. `kander review aggregate` shows the two fields in `batch`, and `kander review progress` lists them per planned batch in `batch_policies`.

### Branch snapshot

A *branch snapshot* is the branch model (`git.branch_mode` and the policy revision) the server fixes at a first claim:

- **Card.** A claim (`kander start`, `kander move <task-id> working --owner`) of a card that is no task group member and has no claim record yet writes the card's snapshot in the same transaction: with a policy, its `git.branch_mode` and revision; with revision 0, the branch mode the client reports (its local effective `git.branch_mode`) and revision 0. An empty or invalid reported mode is `invalid_request` (HTTP 400). Takeovers, restarts, resumes, and later generations keep the snapshot and never read the current policy. A group member has no snapshot of its own.
- **Group.** The first group claim (`kander coordinator claim` on a group without any group claim record) writes the group's snapshot the same way; later group claim generations keep it.
- **Older cards and groups.** A card or group claimed before this change has a claim record but no snapshot and never gets one; it keeps taking the mainline branch from the local `git.branch_mode`.

Claim and group claim requests carry `policy_revision` and `branch_mode`. Only a claim that creates a snapshot checks the revision: a policy revision above 0 that differs is `policy_revision_stale` and writes nothing, receipt included. The check runs after the receipt lookup and before the pending receipt is written, under the project lock. The revision and the branch mode are not part of the claim's idempotency digest, so a resend after a crash with the same key and a newer revision goes through the existing crash recovery, never `idempotency_mismatch`.

`kander show --json` prints `claim.branch_snapshot` (`branch_mode`, `policy_revision`, `scope`: `task` for the card's own, `group` for its task group's), and `kander coordinator show` prints the group's as a top-level `branch_snapshot` next to the checkpoint fields (scope `group`); the claim grant of `kander start` and the group view and grant of the API carry it as `claim.branch_snapshot`. `kander start` (the start branch) and the dispatch evidence checks (the wrap-up `target_ref`, the integration preflight, the coordinator Git facts) take the mainline branch (`main` for `main-only`, `develop` otherwise) from the snapshot of the card or its group and do not read the local configuration; without a snapshot they behave as before. A `todo` group member without its own claim reads its group's snapshot from the group lease. A change of `git.branch_mode` therefore reaches only cards and groups first claimed afterwards.

### Stale retry

On `policy_revision_stale` the client reads the policy again with the policy refresh function, which updates the cache and the command's effective configuration, and sends the same request once more with the new revision and the same idempotency key. A second stale refusal exits non-zero with a message that names the server's current revision and `kander policy show`. The client never rewrites requirements: when the new policy makes them invalid the server answers `policy_requirement_unmet`, and the agent handles that per the rules. For `kander start`, whose launch journal records the claim request, the journal's policy revision and branch mode are updated atomically before the claim is sent again with the same claim key; on a first start the start branch and base are resolved again when the refreshed policy changes them.

### Protocol 4

Claims, group claims, and batch-creating review requests carry the policy revision, so the protocol moved from 3 to 4 (`api.ProtocolVersion` and `api.MinProtocolVersion`). A protocol 3 client would send no revision and bypass the checks, and the server decodes requests strictly, so neither side accepts the other: see [Distributed server](distributed-server.md#protocol) for the rollout order and why a server cannot go back to protocol 3.

## Later work

Long-running processes (TUI, worker, `kander subscribe`) do not yet refresh the policy on change; that is a separate task card of the same group.
