# Multi Repository Workspace Research

## Recommendation and decision status

Support a shared board with each task bound to one repository before attempting coordinated delivery across repositories. These are two different capabilities. The first addresses the principal workflow gap in [issue #17](https://github.com/dualface/kander/issues/17); the second requires new dependency, verification, completion, and recovery protocols.

The user selected one card per repository for work that spans repositories. That establishes the preferred task organization, but does not authorize implementation. The current deliverable is this research report. The narrower first release below remains a recommendation, and the complete design is retained to identify future constraints rather than make every component a first-release requirement.

| Capability | Shared board with independent repository delivery | Coordinated delivery across repositories |
| --- | --- | --- |
| Board | One board for all registered repositories | The same board |
| Card | One repository per card | One repository per card |
| Task group | Members belong to one repository | Members may span repositories |
| Review | Existing repository commit-range review | Repository reviews plus joint verification and review |
| Integration | Each task or same-repository group completes independently | A coordinator records delivery of the complete repository set |
| Recovery | Existing task recovery with an explicit repository context | Additional recovery for dependency changes and partial integration |

The first release must not advertise cross-repository task groups or a combined completion guarantee. It must reject unsupported cross-repository dependencies before execution, rather than apply same-repository ancestry checks to unrelated histories.

## Scenario and verified current behavior

The reported layout has a non-Git workspace root, a shared `kanban/`, and independent repositories below `febase/`:

```text
workspace-root/
  kanban/
  febase/
    repo-a/.git/
    repo-b/.git/
```

Investigation date: 2026-10-16. The source inspection and isolated reproductions used commit `d23e2eb3d4c532ff8180d8f846713375e259d68c`. Source links below identify the relevant implementations; observations describe that inspected revision.

| Area | Current behavior | Consequence |
| --- | --- | --- |
| Board location and initialization | `BoardRootAt` accepts `KANBAN_DIR`, checks the Git main worktree, then walks upward. Initialization falls back to the current directory without Git. | A non-Git workspace can already host a board. See [board location](../internal/board/locate.go). |
| Review | `validateContextMode` validates the explicit absolute CWD and its commit range. Task-bound review locates the board from that CWD. | A child repository can be a review target; the workspace root cannot serve as its Git target. See [review validation](../internal/review/validate.go) and [review entry](../internal/review/review.go). |
| Delivery and overlap checks | Both are board-free analyzers using the process working directory. | They can run separately in each repository, but do not aggregate unrelated histories. See [Git runner](../internal/check/git.go). |
| Launch and recovery | Launch, session discovery, and repair derive execution context from the board parent. | A shared board does not select the correct task repository. See [start](../internal/launch/start.go), [agent launch](../internal/launch/agent.go), and [session repair](../internal/launch/session_repair.go). |
| Project overlay | Inside Git, overlay lookup checks the main worktree root; otherwise it walks upward. | Entering a child repository can stop applying the workspace overlay. See [overlay lookup](../internal/config/overlay.go). |
| Project installation | Project install resolution requires a Git repository. | A project-scoped install at the reported non-Git root is rejected. See [install paths](../internal/config/paths.go). |
| Review and coordinator records | A review plan has one CWD. Coordinator reconciliation accepts one CWD for Git validation. | These contracts do not directly represent a cross-repository execution. See [review plans](../internal/board/review_plan.go) and [coordinator reconciliation](../internal/launch/coordinator.go). |
| Completion | `move done` checks the card's review plan. | Joint completion would require an additional group gate, not just better prompts. See [card moves](../internal/board/snapshot.go). |

The issue therefore identifies a valid context-management gap, but the existing board, review, and check tools are not universally restricted to the board's parent repository. Manually supplying the correct Git context can use existing lower-level capabilities; it does not establish a supported launch-to-completion workflow.

## Verification evidence and limits

A temporary binary built from the inspected revision was exercised against a temporary non-Git parent containing two independent Git repositories. Each repository had one empty commit. No real board or remote repository was changed.

| Probe | Observed result |
| --- | --- |
| `kander check delivery --base HEAD --json` at the non-Git parent | Exit 3, `status:error`, `not-repository` |
| `kander check overlap --source HEAD --json` at the non-Git parent | Exit 3, `status:error`, `not-repository` |
| Both commands inside each child repository | Exit 0, `status:pass` for the empty comparison |
| Delivery check in repository A using repository B's commit as the base | Exit 3, `invalid-ref` |
| `kander config --json` at the parent with an `agent_language` overlay | The workspace value was returned |
| The same config query inside either child repository | The scope value was returned instead |

The following existing tests also passed at the inspected revision:

```text
go test ./internal/check ./internal/board ./internal/review -run 'Test(DeliveryHappyPathAndJSON|Delivery.*|Overlap.*|PlannedInitRootMatchesInitBoard|PathOutsideGitRejected|NonGitExplicitNAPlanCanCompleteReviewGate)$' -count=1

go test ./internal/config -run 'Test(InstallPathsRejectsProjectEntryWithoutGit|ProjectInstallPathsRejectsNonGit|OverlayPathGitAndNonGitLookup|ProjectInstallPathsFromLinkedWorktreeUsesMain|GitDirEnvDoesNotDivertProjectPaths)$' -count=1 -v
```

The first command reported success for all three selected packages; the second executed five named tests. The non-Git N/A review test demonstrates an explicitly exempted workflow, not permission to disable review when a repository is missing.

These checks establish the current boundaries. They do not verify a complete multi-repository task lifecycle, nonempty cross-repository feature delivery, real reviewer isolation across several repositories, or Windows behavior for the proposed feature.

## Proposed foundation

### Workspace declaration and repository identity

Use an explicit workspace declaration with stable repository IDs. A possible `.kander-workspace.json` is:

```json
{
  "schema_version": 1,
  "workspace_id": "<stable-workspace-id>",
  "repositories": {
    "repo-a": { "path": "febase/repo-a" },
    "repo-b": { "path": "febase/repo-b" }
  }
}
```

This filename and schema are proposals, not supported configuration. Initially, repository paths should stay within the declared workspace. External repository mappings and submodule orchestration can be evaluated separately. Repository registration must be explicit; discovery may suggest candidates but must not choose one by recursively searching for `.git`.

Cards should store `REPO: repo-a`, with the workspace identity supplied by their board binding. The ID separates task identity from a directory spelling and avoids treating arbitrary CWD as an authorization to switch repositories. Freeze the binding with the card contract. A directory relocation requires controlled mapping reconciliation; it must not rewrite historical review originals.

Resolve the main worktree and Git common directory when binding a local repository. Record sufficient local identity to detect replacement or conflicting mappings. A remote URL alone is insufficient: two clones can share it while having different refs and working state. Linked worktrees of the same local repository must resolve to the same repository identity. Normal `.git` files are valid and must not be rejected merely because they are not directories.

### Context resolution and path boundaries

Resolve the following separately: workspace root, board root, repository root, task worktree, task branch, and session launch directory. `KANBAN_DIR` continues to override board storage; its parent must not implicitly become the repository root.

An explicit workspace argument or propagated launch context takes priority over automatic discovery. Conflicting explicit board and workspace bindings must fail clearly. Discovery from a linked worktree should inspect its owning main worktree and the applicable workspace declaration. Without a workspace declaration, retain the existing single-repository lookup behavior.

At card creation, the current registered repository or a unique candidate may supply the binding. Before execution, a missing or ambiguous binding must be resolved. Never select a different repository at resume time from the caller's current directory.

Validate paths using the existing no-follow and reparse-point boundaries. Confirm that the resolved Git root matches the registration, rather than accepting a directory inside an unintended enclosing repository. Repository absence or identity drift blocks the affected task while leaving unrelated repositories usable. Git subprocess environments must not allow inherited repository-selection variables to redirect an explicitly resolved target.

### Configuration and installation

The proposed configuration order is scope configuration, workspace overlay, then repository overlay. Existing card language and execution/reviewer pins retain their frozen-value and conflict semantics. Configuration reads need an explicit context, including in concurrent TUI workers; changing process CWD is not a safe selection mechanism.

Workspace presentation and chat use workspace configuration. Task execution and repository review use the task's effective configuration. Group planning must validate all members' effective policies and freeze the applicable requirements. A child overlay cannot silently turn a required group stage into an exemption.

Project installation should also accept a declared non-Git workspace and place its payload under the workspace `.kander/`. Rules lookup, generated task prompts, command paths, and the Pi rules extension must resolve that same installation from child repositories and linked worktrees. This is a prerequisite for claiming complete project-scope support.

### Execution and session recovery

Validate the workspace, repository binding, configuration, and readiness before claiming a card or opening a terminal. A new session may start at the repository main worktree, while the existing executor workflow creates and records its dedicated task worktree.

Keep session launch CWD distinct from the directory where code is edited. Start, resume, notify recovery, takeover, session discovery, and session repair must share this distinction. A controlled record should bind the task worktree and branch to the card's execution cycle; a later claimant cannot reuse stale evidence solely because a path exists.

Launch failures retain existing rollback rules. Repository movement or disappearance must produce a recoverable diagnostic, not a fallback to the workspace root. Propagate an explicit board/workspace context to launched processes so nested commands do not attach to a child repository's different board.

## Recommended first release

The first release should cover explicit workspace registration, per-card repository binding, consistent configuration and installation, launch/recovery routing, repository-bound review and checks, and visible repository identity in the TUI.

Each standalone card and each same-repository task group should complete its existing review and integration lifecycle independently. Preserve `main` and `develop` policy; configurable branch strategies are a separate decision. Cross-repository groups and dependency edges must be rejected at contract/start validation until their semantics are implemented.

Suggested interfaces include `workspace show/check`, `new --repo-id`, and `config --task --json`. Task-aware delivery and overlap checks could resolve the recorded worktree, while existing board-free invocations keep their current-directory behavior. An explicit review CWD remains useful but must agree with the card's repository binding.

The TUI should show repository identity, offer repository filtering, and include the resolved repository and worktree in start previews. GitHub source identity remains distinct from local execution identity: `issue --repo` already names a remote GitHub repository, so a new local selector must use a different name. A bound issue still has one anchor card, with sibling cards referencing its source key.

Ordinary browsing and card drafting may continue at a non-Git root without a task binding. A Git-enabled task cannot start there until a target is determined. Existing explicitly configured non-Git workflows remain possible, but a missing repository never silently disables Git checks or review.

## Complete cross repository orchestration design

### Group partitions and repository evidence

Retain one task group and one coordinator, but partition its Git state by repository. Each partition has its own group branch, group worktree, review plan, integration target, and delivery records. Identical branch names in different repositories are valid because identity includes the repository ID.

Repository review plans can retain their single-CWD commit-range model. Add a group execution plan that references those plans, member bindings, dependencies, and the joint acceptance contract. Each card still belongs to one repository review plan. Coordinator reconciliation must resolve Git context per member or partition instead of applying one caller-provided CWD to every record.

The task-splitting rules must explicitly cover the selected one-card-per-repository approach. The current rule that one goal stays one card cannot be left contradictory when a shared goal requires changes in several repositories; the shared outcome belongs in the group contract, with each member defining its repository contribution.

### Dependency consumption

| Dependency | Required evidence |
| --- | --- |
| Within one group and repository | The predecessor's accepted delivery is contained in that repository's group branch |
| Within one group across repositories | The source delivery is accepted, and the consumer records its exact source commit or artifact |
| Outside the group | The predecessor is complete, its delivery is verified in its own repository's integration target, and the required artifact is available |

Never ask whether a commit from repository A is an ancestor of a branch in unrelated repository B. A consumption record instead identifies the source repository, task, commit, artifact version or digest, and validation method. A source commit alone does not prove that a required package was published or a service was deployed.

When a predecessor changes, retain old evidence but mark affected consumers and joint verification as stale. The coordinator schedules the necessary synchronization or verification before completion. Dependency graphs remain acyclic; mutually dependent implementations need a shared interface contract followed by a compatible execution plan.

Conflict detection uses repository identity plus path. Shared resources outside Git, such as test ports, databases, package registries, and generated output directories, need explicit ownership or isolation. Arbitrary source changes at the non-Git workspace root cannot become an unversioned delivery channel.

### Joint verification and review

Bind joint results to a manifest containing every participating repository's exact commit, the integration-contract digest, and consumed artifact identities. Record test entry points, execution directories, commands, outcomes, and bounded logs. Moving branch names are not sufficient evidence of what was tested.

Add an integration scope to the existing review gate rather than inventing a synthetic workspace commit. This scope inspects the fixed repository combination and applies the configured PMQA/Security policies. Reviewer isolation and post-run validation must cover every exposed repository; existing single-root assumptions need explicit adaptation.

Findings identify repository and responsible card. A finding involving several repositories may produce several fix dispatches referencing the same immutable original. The dispatch contract must support a joint finding reference while still binding each executor to its own repository and base commit.

A changed target commit or integration contract invalidates the old joint conclusion. Unchanged repository reviews may remain valid, but a new joint result must cover the final combination. This prevents two individually passing reviews from being treated as proof of interface compatibility.

### Integration and partial failure

Complete the applicable repository reviews and joint verification before integration. Integrate repository partitions in the agreed dependency order and record each actual transition: pending, locally integrated, push outcome unknown, remotely confirmed, and cleaned up.

Git atomic push covers one remote transport, not a transaction spanning repositories. A group can therefore reach a state where repository A is integrated and repository B is blocked. See the [Git push atomicity contract](https://git-scm.com/docs/git-push#Documentation/git-push.txt---atomic).

Keep the group incomplete in that state. Preserve remaining worktrees and originals, reconcile actual refs after uncertain results, and resume only unfinished operations. Do not repeat successful merges or automatically revert published commits. If upstream changes require a new candidate combination, update the evidence and repeat affected checks before continuing.

Projects that cannot tolerate temporary version skew must provide a compatibility transition or staged publication contract. The coordinator cannot manufacture distributed atomicity from independent Git repositories.

Only after every required partition and joint gate succeeds should the board issue the group completion evidence. A member's `move done` must validate both its repository review gate and the current group evidence. Terminal cleanup remains separate from code integration and must not roll back a valid delivery result.

### Recovery and concurrency

Extend coordinator checkpoints with repository-qualified observations and references to the group plan, consumed versions, joint results, and integration receipts. Retain existing revision checks, execution epochs, and immutable dispatch IDs. Process presence, a timeout, or a column transition must not substitute for a business receipt.

Use short board transactions to publish verified observations under expected revisions. Avoid holding board locks across network operations. Serialize mutations to the same repository/ref, acquire multiple required locks in a deterministic order, and revalidate refs before publication. Recovery must tolerate a crash between a successful Git operation and its recorded receipt.

## Storage and compatibility

Store workspace declarations separately from machine-local execution facts. Board-owned repository bindings, group plans, joint evidence, and integration receipts belong in controlled durable storage, not an evictable cache. Keep original review reports and hashes unchanged.

Version new protocol records and declare the minimum writer version. Existing single-repository boards and cards retain their previous interpretation until explicitly enrolled. Active executions require a controlled migration or completion before changing their repository bindings. Deployment must upgrade all writers; an older binary cannot be assumed to reject a marker it does not understand.

Relocation changes local path mappings only after repository and commit evidence is checked. Historical absolute CWD values remain originals, with an explicit verified relocation binding where needed. Same-origin clones are not interchangeable recovery targets.

Archive cleanup must account for references from unfinished groups and consumers. Preserve compact durable completion evidence when detailed review artifacts are pruned. A completed predecessor must not become unverifiable merely because its card was archived.

## Package responsibilities

| Area | Proposed responsibility |
| --- | --- |
| New `internal/workspace` | Workspace declaration, discovery, repository identity, and path resolution; no dependency on board, config, launch, or review |
| `internal/config`, `internal/install` | Explicit configuration contexts, overlays, non-Git workspace installation, and rule lookup |
| `internal/board` | Frozen bindings, durable group evidence, completion gates, transactions, and reference-aware cleanup; Git verification remains outside storage |
| `internal/launch` and session consumers | Resolved launch/recovery context, repository-specific coordinator verification, and dispatch handling |
| `internal/review`, `internal/check` | Repository binding validation, joint review scope, and task-aware check routing |
| `internal/tui`, `internal/issue`, `internal/i18n` | Repository selection, identity-aware asynchronous results, source/execution distinction, and diagnostics |
| `rules/` and command contracts | Explicit workspace semantics, same/cross-repository dependencies, review scopes, integration recovery, and reporting |

The foundation has multiple stable consumers in config, board location, launch, and review. Keep it below those packages. In particular, a resolver imported by config cannot depend on the current `internal/probe` package, which itself imports config; any shared probe extraction must preserve that dependency direction.

## Delivery sequence and acceptance

| Stage | Deliverable | Required acceptance |
| --- | --- | --- |
| Foundation and independent delivery | Shared board, repository binding, configuration/install support, launch/recovery, checks/review, minimal CLI/TUI/Issue selection | Two independent repositories complete tasks through the normal lifecycle without context drift; a cross-repository group is rejected |
| Cross-repository scheduling | Group partitions, dependency consumption, coordinator recovery | Exact source versions are recorded; changed dependencies invalidate affected consumers; crashes do not duplicate claims |
| Joint verification and delivery | Integration review, joint test manifests, partial integration recovery, group completion gate | A joint failure blocks completion; a successful A/failed B integration resumes without re-integrating A |
| Compatibility and product completion | Migration, full status presentation, reference-aware cleanup, platform coverage | Historical evidence remains readable; archive does not break live references; all supported entry points use consistent context |

Acceptance must also cover incorrect repository selection, misleading inherited Git environment variables, duplicate mappings, missing repositories, linked worktrees, conflicting child boards, different overlays, session takeover, upstream ref movement, ambiguous push results, and cleanup failure. Windows path, handle, reparse-point, and session behavior require platform-specific tests.

The current scope estimate is qualitative: independent delivery is a bounded but cross-cutting feature; coordinated delivery is a substantial protocol extension. No implementation spike or full lifecycle prototype has established a time estimate. Before implementation, settle the workspace declaration/API names, first-release scope, configuration precedence, and migration policy in the task contract. Joint consumption and test entry points remain project-specific inputs.

## References

- [Issue #17](https://github.com/dualface/kander/issues/17): reported layout and requested behavior.
- [Git worktree documentation](https://git-scm.com/docs/git-worktree): worktrees share one repository; linked and main worktrees are distinct working directories.
- [Git push documentation](https://git-scm.com/docs/git-push): remote ref updates and the limits of atomic push.
- [Task group rules](../rules/KANDER-TASK-GROUP-RULES.md), [review evidence gate](../rules/snippets/review-evidence-gate.md), and [group integration](../rules/snippets/orchestrate-integrate.md): current workflow contracts requiring extension.
- [Coordinator recovery](coordinator-recovery.md), [durable dispatch](durable-dispatch.md), [review disposition](review-disposition.md), and [installation boundaries](install-and-permissions.md): current protocol and storage boundaries.
