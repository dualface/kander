# Card Content

Source: moved from `KANDER-KANBAN-RULES.md` "Entries and Documents" and "Post-Creation Self-Review", with content requirements from the review, intake, task group, and issue rules.

Read this file before creating, completing, updating, or reviewing a card. It defines the card template, contract content, saved plans, and creation checks in one place. Workflow rules reference these definitions and retain their applicability, authorization, and operational rules. Storage and controlled publication remain in `KANDER-KANBAN-RULES.md` "Entries and Documents". Apply conditional requirements only when their stated workflow applies.

### Small Task Template (`<task-id>/spec.md`)

```markdown
# <task title>

- TYPE: Feature | Bug | Chore | Research
- SIZE: small
- TASK_GROUP:
- LANGUAGE: <agent communication language, e.g. en, zh-CN, ja>
- CREATED_AT: YYYY-MM-DD HH:MM
- OWNER:
- SESSION:
- WINDOW:
- STARTED_AT:
- FINISHED_AT:
- TASK_BRANCH:
- RESULT:

## GOAL

<what to change and why>

## USER_DECISIONS

<directions and trade-offs the user has confirmed; write N/A if none>

## EXPECTED_OUTCOME

<observable, verifiable state after completion>

## ACCEPTANCE_CRITERIA

- [ ] <condition>

## THREAT_MODEL

<for security tasks: assets, trusted principals, and attacker capabilities; for non-security tasks write N/A>

## OUT_OF_SCOPE

<scope declarations per "Scope Content">

## DISCUSSION

<key conclusions; task group cards also record PREREQUISITES at the beginning>

## IMPLEMENTATION

<detailed task plan preserved at creation per "Required Plan for New Cards"; then branch, commits, verification commands, results, environment gaps, and blockers>

## SUMMARY

<actual results, deviations, unresolved issues, and acceptance conclusion; leave empty before completion>
```

### Large Task Documents

- `SIZE: large` selects the large-task contract. `spec.md` holds the metadata and the contract sections `GOAL`, `USER_DECISIONS`, `EXPECTED_OUTCOME`, `ACCEPTANCE_CRITERIA`, `THREAT_MODEL`, `OUT_OF_SCOPE`, `DISCUSSION`. Plan content and storage follow "Required Plan for New Cards". `report.md` is created on completion, never empty, with actual changes, final commits, verification, deviations, unresolved issues, risks, and the acceptance conclusion.

### Required Plan for New Cards

- Every new card must save a detailed plan before creation self-review, even when left in `backlog/`, regardless of module settings or execution choice. Cards predating this requirement need no backfill; a missing plan alone never blocks their continuation or completion.
- Preserve the confirmed technical approach, ordered steps, affected modules and paths, interfaces, data flows, constraints, dependencies, verification, and applicable release, integration, and rollback steps. Include confirmed choices, trade-offs, and stated reasons. Mark unresolved questions; never present proposals as user decisions.
- Small cards use `IMPLEMENTATION`; large cards use `plan.md`, linked from `spec.md` under `DISCUSSION`. Publish through `update`. Confirmed directions and trade-offs also belong in `USER_DECISIONS`; plans never authorize changing frozen contract fields.
- The card and attachments must let an agent outside the discussion execute without recovering the conversation or repeating settled design decisions. Summaries or conversation references alone are insufficient. Use structured prose, not transcripts; compressing execution history must preserve still-applicable plan details.
- For multiple cards, each saves its own plan and necessary shared decisions, contracts, dependencies, and integration steps. Together they preserve the full confirmed plan. Temporary handover notes never replace these records.

### Contract and Records

- `LANGUAGE` is the language for everything written for the user about this card: title and body, records, reports, review reports, and the messages passed to `kander notify` and `kander resume`. `kander new` fills it from the configured `agent_language` or `--language <value>`; it is fixed at creation and overrides the configuration, and an old card without it falls back to the configuration per `KANDER-AGENTS.md` "Language".
- Manual claiming and `start` write `OWNER`, `STARTED_AT`, `SESSION`, and `WINDOW` per `KANDER-KANBAN-RULES.md` "Controlled Documents and Recovery"; manually claimed cards leave `SESSION` and `WINDOW` empty. Update `TASK_BRANCH` through the controlled body entrance (`N/A` without a branch). The command fills `FINISHED_AT` on entering `done/`, and the move options fill the result atomically on entering `done/`, `archived/`, or `trash/`.
- Once a card enters `todo/`, `GOAL`, `USER_DECISIONS`, `EXPECTED_OUTCOME`, `ACCEPTANCE_CRITERIA`, `OUT_OF_SCOPE`, `SIZE`, and task group relations are frozen; changing any needs an explicit user decision first. One exception: while the card is in `working/` or `review/`, the executing agent ticks acceptance criteria through ordinary `update`, without a decision file. A tick toggles only the line-start marker of a top-level item between `- [ ] ` and `- [x] `; any other change, including an uppercase `[X]` or an indented item, is a contract change. `THREAT_MODEL` is part of the review task context but not frozen; refine it through ordinary `update` and note the change in `DISCUSSION`.
- `OUT_OF_SCOPE` follows "Scope Content"; unconfirmed extended goals are not written into `ACCEPTANCE_CRITERIA`.
- During implementation, append only key decisions, verification, environment gaps, commits, blockers, and next steps, never the session transcript: at most one dated entry per round, earlier rounds compressed to one summary line each once superseded. Review reports, finding lists, and dispositions are referenced by `reviews/<run_id>/` and `dispatches/`, never pasted. The unresolved items list in `SUMMARY` (or `report.md`) holds one line per item. A finding names role, tier, status, and the card-relative path of its disposition record. A role not completed, a missing report section, or a verification gap names the run's sidecar or error log with tier and status `N/A`. With no run (a preflight failure, or a gap outside review) the line names the actual command log or the `IMPLEMENTATION` verification record and says "no run produced", never an invented path. A card body above roughly 30 KB signals duplicated history. Stable architecture, APIs, and long-term rules still go into repository documentation or project rules.

### Scope Content

- Every card states its boundary truthfully. Declare inclusion or exclusion, with a reason, for each category: existing issues; hardening; shared contracts and documentation; adjacent features. These baseline requirements apply with every module setting.
- When `rules.review=true`, or the user explicitly requests the full review flow, complete the following detail before `pick`. The same detail applies to a non-kanban review task context. Expand the four categories to: problems predating the change; concurrency interleaving, cross-platform, and security hardening; syncing shared contracts, public APIs, and architecture documentation; adjacent features and later phases. State categories not required by `ACCEPTANCE_CRITERIA` as excluded and work the card owns as included. One generic exclusion is insufficient. With review disabled and no explicit full-review request, this additional detail is not required.

The following guidance applies only under that review condition.

Common `OUT_OF_SCOPE` items:

- Existing problems: `<specific item> existed before the change (see <commit>); fixing it would expand this task's scope.`
- User-decided directions: `This is the direction the user explicitly requested; do not oppose the direction itself on the grounds of <opposing claim>; the user is aware of the trade-off.`
- Hardening beyond the contract: `Concurrency interleaving, cross-platform or security hardening is not listed in ACCEPTANCE_CRITERIA; this card only guarantees <ACCEPTANCE_CRITERIA>, the rest is handled by <follow-up card>.`
- Theoretical defects with disproportionate cost, excludable only when all three hold (triggering needs an additional precondition already compromised; the consequence is minor or safer; fix complexity clearly exceeds the benefit): `<specific item> requires <premise> to trigger, has consequence <consequence>, and fixing it needs <cost>; not in this round's scope.`
- Environment verification gaps: `<command> could not be executed because of <reason>; covered by <substitute verification>; do not judge a problem on that basis.`
- Dropped backward compatibility: `The project has dropped all backward compatibility; do not raise compatibility, migration or fallback issues.`

Exclusions exclude only scope, never severity: "do not report high" or "report only doc issues" is forbidden. Exclusion reasons rest on facts (the introducing commit, the triggering premise, the user instruction); "not important" or "later" is forbidden.

### Conditional Workflow Fields

These formats apply only when the corresponding workflow requires the field. Reading this file enables no optional module and grants no execution or integration authorization.

- Intake option 3: record a separate line in `DISCUSSION`: `EXECUTION_MODE: self (intake option 3; the session that receives the start instruction claims and executes the card)`. The intake rules decide when to write it and how later instructions override it.
- Task groups (`rules.task_groups=true` and `rules.git=true`): put the group ID in `- TASK_GROUP:`; non-members leave it empty. Members share one `LANGUAGE`. For a permitted group of 4 or 5 members, record its grouping reason in the first member's `DISCUSSION` in dependency order. Grouping decisions remain in the task group rules.
- Dependencies recorded by the enabled task group or intake workflow: put `PREREQUISITES` in a standalone code block at the start of `DISCUSSION`. Separate IDs with ASCII commas; card and group IDs may be mixed. Group members without dependencies write `PREREQUISITES: N/A` in that position. Dependency resolution remains in the task group rules.

```text
PREREQUISITES: 20260905-contract-task,20260905-foundation-group
```

- Issue siblings, only when the issue workflow permits splitting: at the start of `DISCUSSION`, next to `PREREQUISITES`, write `SOURCE_ISSUE: <canonical source key> (anchor: <anchor-task-id>)` as a standalone line. The key is `github://HOST/OWNER/NAME/issues/NUMBER`. A `- ` list prefix and a surrounding code block are accepted; other text on the line voids it. Binding, import, and work-set decisions remain in the issue rules.

### Post-Creation Self-Review

- After filling in the full contract, the creator self-reviews before `todo/`, fixes problems, and re-checks until it passes. This step is part of card creation and independent of the `task_intake` and `review` switches. Check against the user's goal, the confirmed plan, and project rules:
  - Goal and outcome are consistent, no confirmed requirement is missing, and no suggestion or assumption is written as a user decision.
  - The card satisfies "Required Plan for New Cards"; apply its existing-card exemption when relevant.
  - The boundary is clear, scope and exclusions do not conflict, and work necessary to reach the goal is not excluded.
  - Constraints are accurate, actionable, and consistent with user decisions, project rules, and the actual interfaces and environment.
  - `ACCEPTANCE_CRITERIA` cover the goal and outcome, are decidable, and neither miss key conditions nor add out-of-scope requirements.
  - The contract is written for the executing agent and the reviewers, per the writing principles of ASD-STE100 (Simplified Technical English). Each acceptance criterion states one checkable result. A condition comes before the action it limits. One term names one thing across the whole card; a term the reader cannot know is defined at first use.
  - `SIZE` is justified against `KANDER-KANBAN-RULES.md` "Task Scale and Grouping": name the criterion that makes the card `large`, or state that none applies. A `small` card whose planned changes meet a `large` criterion is resized before `todo/`; listing such items under `OUT_OF_SCOPE` does not avoid resizing.
- Record the conclusion and fixes in `DISCUSSION` as a separate line `SELF_REVIEW: <conclusion>` (ASCII colon, may be a list item); the `todo/` gate checks that the line exists. Fix what existing decisions settle; list ambiguities that need new user decisions explicitly and wait, never fill them in on your own.
- Large cards and task group members additionally need an independent card review. The reviewer is an agent that does not share the creation session context (a new session or a subagent); it reads only the card, its linked plan and attachments, and the user's original requirement and judges the points above. After fixing, the creator records `CARD_REVIEW: <conclusion>` with the reviewer in `DISCUSSION`, which the gate checks likewise. The tool checks only that the line exists; independence and quality are the creator's truthful responsibility, and writing the line without an actual review is forbidden.
