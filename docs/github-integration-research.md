# GitHub Issue Integration Research

## Executive recommendation

Kander should ship its first GitHub issue integration on top of GitHub CLI (`gh`), behind a small provider interface that does not expose `gh` concepts to the board, TUI, or launch packages. This route meets all four product goals with the least new security-sensitive code:

- resolve a GitHub repository from the current Git worktree;
- list and filter issues;
- save an issue and optional comments inside a Kander directory card;
- start an agent with the imported card through Kander's existing launch path.

Kander must invoke `gh` with direct argument arrays, request JSON, impose deadlines and output limits, and never extract or persist the token used by `gh`. `gh repo view --json nameWithOwner,url` should be the primary repository resolver because it already understands the local repository context and GitHub CLI's explicit default-repository selection. `gh api` should be the data transport because it combines `gh` authentication and host support with the versioned GitHub REST schema.

A native login is a second-stage product investment. If Kander later needs a no-dependency installation, a branded login, fine-grained repository selection, background synchronization, or webhooks, it should register a GitHub App and use a user access token obtained through device flow. It should not build a traditional OAuth App for this feature: the OAuth `repo` scope grants broad read/write access to private repositories, while a GitHub App can request read-only `Issues` permission for selected repositories.^1 ^2

The two paths should share the same provider-neutral domain types and import transaction. That keeps the first release small without locking Kander to `gh`.

## Scope and interpretation

The requested outcomes are interpreted as follows:

1. **Associate the local project with GitHub.** Resolve a canonical tuple of host, owner, repository, and URL from the local Git worktree. Do not write a second permanent association when the Git remotes already determine it. Allow an explicit override for ambiguous multi-remote or fork layouts.
2. **Query issues.** List open, closed, or all issues with bounded pagination and common filters. Pull requests must not appear as issues.
3. **Save an issue locally.** Create a normal Kander directory card and store a lossless source snapshot as an attachment. Preserve provenance and fetch metadata.
4. **Send the issue to an agent.** Use the existing card lifecycle and `internal/launch.Start`. Do not create a separate agent invocation path.

Comments are useful context but are not part of the issue body endpoint. They should be opt-in for the first release to control prompt size, private-data exposure, and API calls.

## Current Kander constraints

Kander already has most of the local half of the integration:

- `internal/board.NewTask` creates `<task-id>/spec.md` as a directory card under board transaction and identity locks.
- `internal/board.UpdateDocument` supports safe, revision-checked attachment writes.
- `internal/launch.Start` validates a `todo` card, creates the agent task file, moves the card to `working`, and preserves rollback semantics.
- `startAgentPrompt` directs the agent to load Kander rules and inspect the card. An imported card can therefore reference a source attachment without copying remote text into command-line arguments.
- `internal/process` already uses direct process execution and has cross-platform handling for agent CLIs. The GitHub backend should follow the same no-shell boundary.
- Command names are centrally registered in `internal/cli`; implementations bind through the existing one-way registration pattern.

The integration should extend these contracts instead of writing card files directly. A new atomic board API is preferable to a sequence of `NewTask` and `UpdateDocument` calls because a failed attachment write must not leave a source-less imported card.

## Path A: use GitHub CLI

### Proposed operation

Repository resolution:

```text
gh repo view --json nameWithOwner,url,isPrivate
```

With no repository argument, `gh repo view` operates on the repository for the current directory.^3 When several GitHub remotes exist, `gh repo set-default` lets the user choose the remote used for issue and pull-request commands.^4 Kander should also accept `--repo [HOST/]OWNER/REPO`; the explicit value always wins.

Issue retrieval should use the REST API through `gh api`, not scrape human-readable `gh issue` output:

```text
gh api --hostname HOST \
  -H 'Accept: application/vnd.github+json' \
  -H 'X-GitHub-Api-Version: 2026-03-10' \
  --method GET 'repos/OWNER/REPO/issues?state=open&per_page=100'

gh api --hostname HOST \
  -H 'Accept: application/vnd.github+json' \
  -H 'X-GitHub-Api-Version: 2026-03-10' \
  --method GET 'repos/OWNER/REPO/issues/NUMBER'
```

`gh api` supports explicit hosts, pagination, response caching, headers, and JSON filtering.^5 The REST API must be pinned with `X-GitHub-Api-Version`; GitHub currently supports `2026-03-10` and promises at least 24 months of support for the preceding version after a new version ships.^6

The repository-issues REST endpoint returns pull requests as well as issues. Kander must discard objects containing `pull_request`.^7 For a small result set, one page is enough. For `--limit` beyond one page, Kander should follow pagination via `gh api --paginate --slurp` or issue sequential page requests. The CLI already exposes higher-level issue JSON fields, but REST gives Kander one versioned schema for both backends.^8 ^9

Authentication remains owned by `gh`:

- `gh auth status --active --hostname HOST` provides an interactive diagnostic. Without `--json`, authentication failures produce a non-zero exit status; with `--json`, authentication problems can still exit zero, so Kander must inspect the JSON if it uses that mode.^10
- `gh auth login` normally stores tokens in the system credential store, but can fall back to a plain-text file when no credential store is available.^11 Kander should disclose that fact in `doctor` output, not silently claim keychain storage.
- `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, and `GITHUB_ENTERPRISE_TOKEN` can override stored credentials. `GH_HOST` and `GH_REPO` also affect resolution.^12 Kander should not clear these variables. Diagnostics should state which source class is active without printing any token.
- Kander must never call `gh auth token`, `gh auth status --show-token`, or save `gh` output that contains credentials.

### Strengths

- Small implementation and audit surface. GitHub login, SSO, multiple accounts, credential storage, and GitHub Enterprise host selection stay with `gh`.
- Proven local-repository behavior. In this checkout, GitHub CLI 2.46.0 resolved the HTTPS `origin` to `dualface/kander` and returned the repository's current issues.
- Good terminal fit. Users who already work with GitHub from a CLI often already have `gh` and an authenticated account.
- No Kander-managed long-lived secret and no OAuth application registration, callback service, refresh-token rotation, revocation UI, or account store.
- Private repositories work according to the credential already selected by `gh`.

### Costs and limitations

- `gh` becomes an optional runtime dependency. Missing, old, or broken installations need precise `doctor` diagnostics.
- Kander inherits the active `gh` account and environment precedence. A local Git remote does not prove that the active account has access.
- `gh auth login` uses a broad minimum scope set for its normal token flow: `repo`, `read:org`, and `gist`.^11 Kander itself requests no extra access, but its least-privilege story depends on the user's `gh` setup. Users may instead provide a fine-grained personal access token through `GH_TOKEN`.
- Organizations may forbid GitHub CLI's OAuth application or require SAML authorization. Errors must distinguish unauthenticated, unauthorized, SSO-required, missing repository, and rate-limited cases where possible.
- A library embedding Kander cannot use this backend without the executable and its process environment.

### Required hardening

Create a dedicated runner rather than scattering `exec.CommandContext` calls:

```go
type CommandRunner interface {
    Run(ctx context.Context, directory string, argv []string, stdoutLimit int64) ([]byte, []byte, error)
}
```

The production runner must:

- resolve one exact `gh` executable and invoke it without a shell;
- set `Cmd.Dir` to the Git worktree root;
- use fixed option names and validate host, owner, repository, state, labels, and issue number before constructing arguments;
- use context deadlines;
- cap stdout and stderr, reject truncated JSON, and require UTF-8;
- decode with typed structures while tolerating unknown fields;
- never pass issue text through arguments, environment variables, or shell fragments;
- redact token-shaped strings from returned diagnostics as defense in depth;
- return structured error categories while retaining a short safe cause.

## Path B: users authenticate Kander directly

### Correct GitHub integration type

For a distributable local CLI, “authenticate Kander” should mean a public GitHub App using user access tokens. GitHub generally prefers GitHub Apps over OAuth Apps because GitHub Apps provide fine-grained permissions, selected-repository access, and short-lived tokens.^13

Configure the GitHub App with:

- repository permission `Issues: read`;
- no write permission;
- no webhook subscription for the first release;
- device flow enabled;
- expiring user access tokens enabled;
- installation limited by the user to selected repositories.

The issue-list endpoint accepts GitHub App user tokens and installation tokens with `Issues: read`; public repositories can also be read without authentication.^7 A user access token is constrained by both the user's own access and the app's permissions, and it can access repositories only where the app is installed.^14

A native local CLI should not embed a GitHub App private key to mint installation tokens. GitHub warns that native clients cannot protect such a key and recommends user access tokens instead.^15

### Device-flow lifecycle

The login command would perform this sequence:

1. POST the public GitHub App client ID to `/login/device/code`.
2. Display the verification URL and one-time user code.
3. Poll `/login/oauth/access_token` no faster than the server-provided interval.
4. Save the user access token and refresh token in the operating system credential store.
5. Save only non-secret account metadata in Kander configuration.

GitHub documents device flow specifically for headless tools and CLI applications. The user code currently expires after 900 seconds, and clients must respect the returned polling interval.^16 ^17 Device flow does not require a client secret. For an expiring GitHub App login, the user token lasts eight hours and the refresh token lasts six months; refreshing rotates both tokens.^18

The flow still has installation friction. Authorizing a GitHub App and installing it are separate concepts. API access is the intersection of user access, app permission, and repositories included in the installation.^14 A user may need an organization owner to approve installation, and SAML-protected organizations may require an active SAML session before reauthorization.

### Secret storage obligations

This path makes Kander a credential manager. The implementation must support at least:

- macOS Keychain;
- Windows Credential Manager;
- Linux Secret Service when available;
- an explicit refusal or clearly labeled fallback when no secure credential store is available;
- login status, account switching, logout, revocation guidance, and refresh-token rotation;
- atomic replacement of refreshed credentials;
- prevention of tokens entering `config.json`, `.kander-config.json`, logs, crash output, board files, task files, environment dumps, or child-agent environments.

Kander's existing configuration policy intentionally does not tighten permissions. Tokens therefore must not be added to the current JSON configuration schema. A secure-store outage must fail closed for private access. A plaintext fallback should require an explicit user choice and a documented path and permission model.

### Why not a traditional OAuth App

Traditional OAuth Apps have poor permission granularity for this use case. No scope reads only private issues. The `repo` scope grants full read/write access to public and private repositories and several related organization resources; no scope grants only public information, while `public_repo` also grants write capabilities for public repositories.^1

An OAuth App is easier than a GitHub App only if broad user-level repository access is acceptable. That trade is not justified for an issue-import feature. If native authentication is built, the extra GitHub App installation step is preferable to excessive permission.

### Strengths

- No external executable.
- Branded, consistent setup and diagnostics across platforms.
- True least privilege through `Issues: read` and selected repositories.
- Stable typed HTTP client with conditional requests and explicit API versioning.
- Foundation for background sync and webhooks.

### Costs and limitations

- Largest security and maintenance burden: OAuth state machine, refresh rotation, credential stores, account and host management, revocation, SSO, enterprise variants, and redaction.
- GitHub App registration and long-term ownership become release infrastructure.
- Organization installation approval adds friction not present when reusing an already-approved `gh` setup.
- GitHub Enterprise Server requires per-host App registration or administrator-provided client metadata; one github.com App identity does not solve arbitrary enterprise hosts.
- A client ID is public and reusable. Device-flow phishing guidance and clear branding matter even though no client secret is embedded.^19

## Decision matrix

Scores use 1 for weak and 5 for strong. Weighted totals are directional product judgments, not sourced measurements.

| Criterion | Weight | `gh` backend | Native GitHub App |
|---|---:|---:|---:|
| Time to first reliable release | 25 | 5 | 2 |
| New credential risk | 20 | 5 | 2 |
| Least-privilege authorization | 15 | 2 | 5 |
| User setup without dependencies | 15 | 2 | 5 |
| GitHub Enterprise adaptability | 10 | 4 | 2 |
| Long-term API control | 10 | 3 | 5 |
| Background sync/webhooks | 5 | 1 | 5 |
| **Weighted total / 500** | **100** | **385** | **340** |

The `gh` route wins for the requested interactive import workflow. Native authentication wins only after dependency-free UX, least privilege, or continuous synchronization becomes more valuable than implementation and credential risk.

## Shared architecture

### Provider boundary

Keep remote access separate from card creation:

```go
type Repository struct {
    Host          string
    Owner         string
    Name          string
    URL           string
    Remote        string
    Private       bool
}

type IssueSummary struct {
    Number    int
    Title     string
    State     string
    URL       string
    Labels    []string
    UpdatedAt time.Time
}

type IssueSnapshot struct {
    Repository Repository
    Number     int
    NodeID     string
    Title      string
    Body       string
    State      string
    StateReason string
    URL        string
    Author     string
    Labels     []string
    Assignees  []string
    CreatedAt  time.Time
    UpdatedAt  time.Time
    Comments   []IssueComment
    FetchedAt  time.Time
}

type IssueProvider interface {
    ResolveRepository(ctx context.Context, directory, explicit string) (Repository, error)
    ListIssues(ctx context.Context, repository Repository, query IssueQuery) ([]IssueSummary, error)
    GetIssue(ctx context.Context, repository Repository, number int, withComments bool) (IssueSnapshot, error)
}
```

Suggested package direction:

```text
internal/issue        domain types, service, import mapping
internal/issue/ghcli  GitHub CLI provider
internal/issue/github native REST and GitHub App provider, added later
internal/board        atomic imported-card transaction
internal/launch       unchanged execution path
internal/cli          command registration only
```

Neither provider should import `board`, `launch`, or `tui`. The issue service may depend one way on the provider interface and board APIs. The TUI may call the issue service asynchronously, matching existing background-work rules.

### Repository resolution policy

Use one deterministic order:

1. explicit `--repo [HOST/]OWNER/REPO`;
2. saved project override, only if the product later adds one;
3. provider resolution from the current worktree;
4. fail with candidates and remediation; never guess among ambiguous remotes.

For `gh`, step 3 is `gh repo view`. If ambiguity remains, tell the user to run `gh repo set-default <remote>` or pass `--repo`. This correctly handles forks where `origin` may be the user's fork and `upstream` the project that owns the issues.

For a native provider, use Git configuration rather than shell output parsing where practical. Recognize HTTPS and SCP-like SSH URLs, preserve the host, strip only a terminal `.git`, validate owner/repository segments, then call the repository endpoint to canonicalize case and redirects. Multiple GitHub remotes are an ambiguity, not a reason to silently choose `origin`.

Do not infer GitHub from repository path text. Do not accept credentials embedded in a remote URL. Do not send a remote host to `api.github.com`; github.com and GitHub Enterprise API base URLs differ.

### Local persistence model

An imported issue should remain a normal card:

```text
kanban/backlog/20260909-gh-42-task/
  spec.md
  source/
    github-issue.json
    github-issue.md
```

`github-issue.json` is the lossless machine snapshot. It should include a schema version, canonical source key, fetched timestamp, repository identity, selected issue fields, and comments when requested. `github-issue.md` is a readable rendering with the source URL and fetched timestamp. Neither file contains an access token or response headers that may expose sensitive infrastructure.

The canonical source key should be:

```text
github://HOST/OWNER/REPO/issues/NUMBER
```

Import must scan or index this key across all card states and return the existing task by default. `--duplicate` may explicitly create another card. A later `refresh` command should append or atomically replace only provider-owned source attachments; it must never mutate a frozen task contract without the existing contract-decision mechanism.

The board needs one transaction that allocates the card identity and publishes `spec.md` plus source attachments together. It must apply the same no-symlink/reparse protections as other board writes and recover through the existing journal.

### Mapping into `spec.md`

Remote Markdown is untrusted data. Copying an issue body directly under `## GOAL` can introduce Kander headings or metadata that alter parsing. Do not splice it into the contract.

Generate a valid small-card contract that contains only Kander-authored text and references `source/github-issue.md`. Suggested mapping:

- title: issue title, normalized to one line;
- type: explicit `--type`, then conservative label mapping, then `feature` or another documented default;
- language: current `agent_language` or explicit `--language`;
- goal: resolve the named issue and read the source attachment;
- user decisions: record that GitHub issue content is imported context, not trusted agent instruction;
- expected outcome: satisfy the issue's reported behavior subject to repository rules;
- acceptance criteria: require a verified implementation, relevant tests, and traceability to the source issue;
- out of scope: updates made on GitHub after `fetched_at`, unless refreshed;
- discussion: canonical source key, source URL, fetched timestamp, label mapping decisions.

The raw body and comments stay lossless in the source attachment. Rendered Markdown must clearly delimit author, timestamps, body, and comments. Strip disallowed control characters while preserving valid UTF-8 and line breaks.

### Sending to an agent

Use one of two explicit flows:

```text
kander issue import 42
# inspect or edit generated backlog card
kander pick TASK_ID
kander start TASK_ID
```

```text
kander issue import 42 --start
```

`--start` is convenience, not a new execution mechanism. It should atomically import a ready contract, move it through the controlled `todo` transition, then call `launch.Start`. If launch fails, retain the imported backlog or todo card and report its ID; never delete the fetched source. The user can retry with ordinary Kander commands.

The agent receives Kander's normal task-file instruction. The card tells the agent to read the source attachment. This preserves the existing rules, language directive, liveness, session metadata, rollback, and review gates. Issue text never becomes a top-level instruction and never enters a shell argument.

## Proposed CLI surface

Prefer a provider-neutral noun even if GitHub is the first implementation:

```text
kander issue repo [--repo HOST/OWNER/REPO] [--json]
kander issue list [--repo ...] [--state open|closed|all] [--label NAME] [--limit N] [--json]
kander issue show NUMBER [--repo ...] [--comments] [--json]
kander issue import NUMBER [--repo ...] [--comments] [--type TYPE] [--large] [--language VALUE] [--start]
```

The top-level `issue` command fits future GitLab or other providers better than `github`. Configuration can choose `issue_provider: "gh"` later; the first release can auto-select `gh` and emit a clear missing-dependency error.

Do not add `login` for the `gh` backend. Report:

```text
GitHub CLI is not authenticated for HOST. Run: gh auth login --hostname HOST
```

If the native provider ships later, add provider-scoped commands such as `kander issue auth login`, `status`, and `logout` rather than overloading general Kander installation.

## Error and trust model

### Failure categories

Return actionable, stable categories:

- not a Git worktree;
- no GitHub remote;
- ambiguous remotes;
- `gh` missing or below the supported version;
- host not authenticated;
- repository inaccessible or App not installed;
- issue not found, noting that GitHub can return `404` for unauthorized private resources;
- SSO authorization required;
- rate limited, including reset or retry time when safely available;
- response too large, invalid JSON, invalid UTF-8, or schema mismatch;
- duplicate import;
- board transaction conflict;
- launch failed after successful import.

GitHub advises clients to follow redirects, use conditional requests, avoid concurrent requests, and honor `retry-after` and rate-limit reset headers.^20 Authenticated REST requests normally share a 5,000-request-per-hour user limit; unauthenticated public requests receive 60 per hour.^21 Interactive commands need no aggressive cache, but a future refresh loop should save ETags and use conditional requests.

### Prompt-injection boundary

Issue authors and commenters are external principals. Their text may contain instructions aimed at an agent. The generated card and launch prompt must state that source content is evidence and requirements data, subordinate to Kander rules, repository `AGENTS.md`, and the card contract.

Additional controls:

- comments are opt-in;
- impose configurable byte and comment-count limits;
- show truncation explicitly and preserve the source URL;
- never fetch arbitrary links or attachments embedded in issue Markdown during import;
- never execute code blocks from the issue;
- do not render terminal escape sequences;
- avoid terminal hyperlinks from unvalidated hosts;
- do not send private issue content to an agent provider without the user's explicit start action and existing agent configuration.

## Delivery plan

### Phase 0: contracts and tests

- Add provider-neutral issue types and error categories.
- Add an atomic board API for a new card plus attachments.
- Define source snapshot schema version 1 and canonical source identity.
- Add fake provider and fake command runner tests.

Exit condition: no GitHub process or network code is required to test mapping, duplicate handling, transactions, or launch handoff.

### Phase 1: `gh` read and import

- Add `gh` discovery and version diagnostics.
- Resolve repository with explicit override and local context.
- Implement list, show, import, optional comments, JSON output, and size limits.
- Add command registration, i18n strings, POSIX and Windows process tests.
- Add `doctor` checks without changing install requirements.

Exit condition: all four requested outcomes work for public and private github.com repositories when `gh` is authenticated.

### Phase 2: agent handoff and TUI

- Add `--start` through existing controlled move and launch APIs.
- Add asynchronous issue selection/import to the TUI only after CLI behavior stabilizes.
- Surface warnings through structured results, never background stdout/stderr.

Exit condition: import and launch failures are independently recoverable; the board always retains fetched source after successful import.

### Phase 3: native GitHub App, only with a product trigger

Start this phase only when at least one trigger exists:

- users reject the `gh` dependency at meaningful scale;
- organizations require selected-repository, read-only authorization;
- background synchronization or webhooks become a product requirement;
- an embedding use case cannot execute `gh`.

Implement the same `IssueProvider`, GitHub App device flow, secure credential stores, refresh rotation, selected-repository installation guidance, API versioning, ETags, logout, and revocation diagnostics. Keep `gh` as a fallback or migration path.

## Test strategy

### Unit tests

- remote and explicit repository normalization, including HTTPS, SSH, `.git`, ports, enterprise hosts, malformed URLs, and credential-bearing URLs;
- typed JSON decoding with missing, null, unknown, and newly added fields;
- pull-request exclusion;
- state, label, pagination, and limit behavior;
- hostile Markdown with Kander headings, metadata lines, ANSI escapes, bidi controls, and very large bodies;
- canonical source identity and duplicate detection;
- deterministic readable Markdown rendering;
- token redaction;
- timeout, cancellation, non-zero exit, truncated output, and malformed JSON;
- transaction failure at each staged file and recovery after restart.

### Integration tests

- fake `gh` executable capturing argv and working directory;
- environment-token precedence without recording values;
- separate stdout/stderr behavior on POSIX and Windows;
- import followed by controlled `todo` transition and `launch.Start` using existing launch fakes;
- private issue fixtures and comments without live credentials;
- optional nightly tests against a dedicated public repository, never required for ordinary `go test ./...`.

No test should call the user's real `gh`, alter active accounts, rewrite Git remotes, access the real board, or write the user's credential store.

## Final decision

Adopt this sequence:

1. **Ship `gh` first.** Use `gh repo view` for local association and `gh api` for typed, versioned REST responses.
2. **Build a provider boundary immediately.** Keep board import and agent handoff independent from authentication and transport.
3. **Persist source inside the card.** Save lossless JSON plus readable Markdown in one board transaction; use a canonical source key for idempotency.
4. **Launch through existing Kander flow.** `--start` is import, controlled move, then `launch.Start`; remote content remains untrusted attachment data.
5. **Do not build a traditional OAuth App.** If native authentication becomes necessary, use a read-only GitHub App with device flow and secure OS credential storage.

This plan delivers the requested capability quickly, avoids making Kander a credential manager in the first release, and preserves a clean route to a polished native integration later.

## Sources

1. GitHub. “[Scopes for OAuth apps](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/scopes-for-oauth-apps).”
2. GitHub. “[Choosing permissions for a GitHub App](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app).”
3. GitHub CLI. “[gh repo view](https://cli.github.com/manual/gh_repo_view).”
4. GitHub CLI. “[gh repo set-default](https://cli.github.com/manual/gh_repo_set-default).”
5. GitHub CLI. “[gh api](https://cli.github.com/manual/gh_api).”
6. GitHub. “[API Versions](https://docs.github.com/en/rest/about-the-rest-api/api-versions?apiVersion=2026-03-10).”
7. GitHub. “[REST API endpoints for issues](https://docs.github.com/en/rest/issues/issues?apiVersion=2026-03-10).”
8. GitHub CLI. “[gh issue list](https://cli.github.com/manual/gh_issue_list).”
9. GitHub CLI. “[gh issue view](https://cli.github.com/manual/gh_issue_view).”
10. GitHub CLI. “[gh auth status](https://cli.github.com/manual/gh_auth_status).”
11. GitHub CLI. “[gh auth login](https://cli.github.com/manual/gh_auth_login).”
12. GitHub CLI. “[gh environment](https://cli.github.com/manual/gh_help_environment).”
13. GitHub. “[Differences between GitHub Apps and OAuth apps](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/differences-between-github-apps-and-oauth-apps).”
14. GitHub. “[Authenticating with a GitHub App on behalf of a user](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-with-a-github-app-on-behalf-of-a-user).”
15. GitHub. “[Best practices for creating a GitHub App](https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/best-practices-for-creating-a-github-app).”
16. GitHub. “[Building a CLI with a GitHub App](https://docs.github.com/en/apps/creating-github-apps/writing-code-for-a-github-app/building-a-cli-with-a-github-app).”
17. GitHub. “[Authorizing OAuth apps: Device flow](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/authorizing-oauth-apps#device-flow).”
18. GitHub. “[Refreshing user access tokens](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/refreshing-user-access-tokens).”
19. GitHub. “[Best practices for creating an OAuth app](https://docs.github.com/en/apps/oauth-apps/building-oauth-apps/best-practices-for-creating-an-oauth-app).”
20. GitHub. “[Best practices for using the REST API](https://docs.github.com/en/rest/using-the-rest-api/best-practices-for-using-the-rest-api).”
21. GitHub. “[Rate limits for the REST API](https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api).”
