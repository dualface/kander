# GitHub issue result reconciliation

The Issues overlay uses `s` for investigation on an unbound issue and for result
reconciliation on a done anchor card. Bound non-done cards offer `g` only; all
bound cards retain `g` and disable `i`/`I`. The result confirmation explains that
missing result comments may be added but closing requires separate explicit
confirmation. Selection, binding and state are checked before launching. The
background work uses the existing `pendingWork` and terminal backend paths.

`kander issue result NUMBER --card TASK_ID [--repo HOST/OWNER/REPO]` starts the
same session from the CLI; `--agent` and `--launcher` override defaults. The session
uses the small execution tier, inherits the card language, and owns no card. Its
launch prompt selects the result protocol in the released issue rules. Evidence
files are unique per session under the private `result-sessions` cache; the agent
must refresh with `--action inspect`, not use startup evidence as a write grant.

## Controlled workflow

The `internal/issue.ResultProvider` contract is separate from `IssueProvider`, which
stays read-only for existing consumers. `ghcli` implements both. The result command
has four actions: `start` (default), `inspect`, `apply` and `decide`. The last two
consume `--file <UTF8 JSON>`. Exact proposal/decision JSON and agent duties are in
[`KANDER-ISSUE-RULES.md`](../rules/KANDER-ISSUE-RULES.md#completed-result-reconciliation).

Inspection returns current spec/report, completion-document delivery SHAs, card
digest, complete remote issue/comments, authenticated actor ID, remote revision,
open/close event version and previous recovery records. The agent assesses every
acceptance criterion, relevant verification and equivalent human or agent comments.
It quotes explicit completion evidence before asserting full issue resolution;
`done` and checked boxes alone do not establish that conclusion. Related work
explicitly referenced by the card must also be checked when relevant.

Apply checks a digest of the observation before accepting an assessment. The
result key hashes card identity, sorted completion-document SHAs, ordered criterion
outcomes and full-resolution status. Verification commands/statuses are retained
as assessment evidence but selecting a different subset never makes another key.
An additional completion-document fingerprint (SUMMARY/acceptance/report, excluding
implementation logs) blocks a second publication for unchanged evidence and SHAs
even when the agent changes its outcomes. An existing comment can cover a new
assessment without another publication. It does
not hash the comment's wording, session, timestamps or execution logs. No delivery
SHA is required for non-code work; criterion outcomes and full-resolution status still
identify its result. Semantic assessment belongs to the trusted agent, including
selecting relevant checks and avoiding invented conclusions. An existing equivalent
comment is recorded by numeric ID without posting. A published or equivalent result never posts again,
including after its remote comment is edited or deleted; deletion is not retry consent.
Only a definite `rejected` attempt may be retried after inspection.

Decide checks current card evidence and the exact observation shown when asking,
plus the selected result's full-resolution evidence. Yes/no decisions carry the
actual user reference and are retained with the result and remote state version.
An unchanged refusal suppresses asking again; a conflicting yes without explicit
reconsideration fails instead of silently returning success; explicit user reconsideration may
record another decision. A later close/reopen event invalidates earlier decisions.
A response that arrives after the target changes requires another inspection and
new consent, never a silently refreshed token. An already closed issue needs no write.

## Durability and failure boundaries

`board.EnsurePrivateDataDir` exposes feature-owned private storage without exposing
board implementation details. Result records live under
`kanban/.kander/issue-results/v1/<sha256 canonical source key>/state.json`, alongside
a per-issue cross-process lock. They are durable recovery evidence, never cache
entries. No pruning or user-facing reset command is provided. Back them up with
the board; deleting them destroys uncertain-write protection. Rollback must retain
them. Corrupt/foreign records fail closed instead of being rebuilt.

A context-bound exclusive lock spans observation, intent persistence, remote write
and receipt persistence. The board's card locks are not held across network calls.
Card state, source binding and current documents are rechecked after the remote read.
The file layer provides private modes/ACLs, no-follow access and atomic replacement.
An uncertain intent with exact body, a random marker and numeric author ID is saved
before POST. Recovery requires all three to match a complete fetched comment. A
marker alone, especially from another author, is insufficient. If no match is found,
further publication remains blocked: a lost response is not proof of a failed write.
A failed save after success leaves the earlier uncertain intent for recovery.
A definite gh HTTP rejection (400/401/403/404/410/413/422/429) is recorded as
`rejected` with its diagnostic retained. Comment retries require another complete
inspection; rejected close retries require fresh explicit user reconsideration.
Transport/deadline/5xx/response-mismatch failures stay uncertain. An open read alone
cannot prove that an earlier request will not complete later.
Close requests likewise persist the decision before PATCH; an uncertain open-state
outcome is never blindly retried. Observing closed records that fact, without
claiming which concurrent actor closed it.

The provider pins REST API version `2026-03-10`, explicitly names the host and path,
uses direct argv (no shell), and keeps gh-managed credentials out of local records.
Complete comments and events require a short final page: at most 10 pages of 100,
4 MiB per collection and the existing 1 MiB per-response process bound. Metadata
counts, unique IDs and bracketing metadata/event reads reject incomplete or changing
observations. Permission/authentication failures also block publication. Numeric
authenticated identity currently requires the `/user` endpoint; credentials that
cannot provide it cannot publish through this workflow.

GitHub's [comment creation endpoint](https://docs.github.com/en/rest/issues/comments#create-an-issue-comment)
has no documented idempotency key. The [issue update endpoint](https://docs.github.com/en/rest/issues/issues#update-an-issue)
has no atomic read/decision/write transaction for this workflow. Local coordination
allows one pending or accepted comment attempt per result per shared board;
explicit no-effect rejections can be retried after inspection, while uncertain
attempts remain blocked even if they might have failed before reaching GitHub.
Independent machines/boards can race and duplicate. Remote changes between the final
check and POST/PATCH remain an API race, including reopening at that boundary.
This is not global exactly-once delivery or an atomic conditional close. Agents must
report those uncertainties honestly and never bypass the controlled commands.

Tests use temporary boards/configuration, fake providers, copied gh/git test binaries,
fake terminals and a PTY confirmation. No test writes to live GitHub.

Publication bodies allow multiline Markdown and HTTPS links, with a 16000-byte UTF-8
limit. The publication validator rejects unsafe controls (except newline/tab),
recognizable credentials and local paths without the diagnostic sanitizer's 300-byte
truncation. Semantic summaries and deliberate privacy review remain agent duties.
