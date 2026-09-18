# Kander × QuickTUI: A Production Retrospective on Nearly 1,000 Real-World AI Coding Tasks

**Analysis date:** 2026-09-18  
**Primary time range:** 2026-08-01 to 2026-09-18  
**Source repository:** `dualface/quicktui-mono`  
**Task/work-record repository:** `dualface/quicktui-mono-kanban`  
**Kander repository:** `dualface/kander`

---

## 0. Executive Summary

This dataset materially changed my assessment of Kander: **it is no longer best described as an early AI-agent orchestration tool with a relatively complete design. It is a production workflow runtime that has evolved through sustained, high-frequency use on real software development.**

From 2026-08-01 through 2026-09-18, a span of 49 calendar days, the user reports that Kander handled nearly **1,000 tasks**. That implies a rough card-level throughput of about **20 tasks per calendar day**. This number should not be interpreted directly as productivity—the tasks vary enormously in scope, and it would be misleading to compare this with a human engineer's tickets/day. What it does establish is that Kander has undergone continuous, high-frequency, cross-platform, cross-module production use rather than demo-scale dogfooding.

After reviewing the data, I would rank Kander's most valuable assets as follows:

1. **A workflow protocol evolved from real failures, not the Kanban UI.**
2. **A quality loop of independent review → finding → verification → fix dispatch → incremental re-review → closure.**
3. **Provenance across tasks, branches, worktrees, review bases, reviewer/model/effort, batches/runs, and dispatches.**
4. **Conflict awareness, task groups, dependency ordering, and controlled parallelism rather than maximizing the number of agents.**
5. **An empirical dataset built from nearly 1,000 real tasks.** This may ultimately be the hardest asset to replicate.

The strongest evidence is not simply that “many tasks were completed,” but that the history contains repeated instances of this loop:

> Implementation completed → all tests green → independent review still finds real semantic issues → findings become explicit fix rounds → incremental re-review → tests are added/fixed → final PASS.

`20260912-android-e2e-liveness-auth-task` is especially representative. The initial author-side verification reached **2,889 tests passed, with lint/build passing**, yet PM/QA review still identified issues involving connection generations, the heartbeat state machine, late callbacks, lease lifecycle, and false-green tests. Multiple fix rounds followed, eventually reaching **2,935 tests passed** and a PM/QA re-review PASS.

This demonstrates that Kander's review loop is not a ceremonial approval step. At least on some high-complexity tasks, it caught defects that ordinary test gates did not cover.

At the same time, the data reveals Kander's most important next-stage gap:

> **Kander already has rich provenance, but it has not yet turned that provenance into a stable, structured analytics layer suitable for longitudinal analysis.**

Accordingly, my highest-priority recommendations are not to add more UI or more agents, but to:

- establish an append-only execution/event schema;
- record finding disposition in a structured form;
- add incident provenance to rules;
- use historical data for complexity-normalized model routing;
- add a true OS-level hard sandbox for reviewers;
- turn real tasks into a long-running benchmark/case-study system.

In one sentence:

> **Kander's strongest moat today is not that it can run multiple coding agents at once, but that it has gradually encoded the failures, rework, reviews, and recovery lessons from hundreds—approaching a thousand—real AI software-engineering tasks into an executable engineering protocol.**

---

# 1. Scope and Methodology

## 1.1 Data Sources

This report directly examined the following GitHub repositories.

### QuickTUI source

`dualface/quicktui-mono`

The repository contains several clearly distinct product and engineering boundaries, including:

- `agentbridge2/`
- `client/`
- `cloud/`
- server / relay / account-related modules
- iOS / Android / Qt/QML / Web
- repository-level `AGENTS.md`
- agent configuration such as `.claude/` and `.codex/`

This means the task set is not a synthetic benchmark built from a small, single-language, single-module project. It is the development history of a real cross-platform product.

### Kander board / work records

`dualface/quicktui-mono-kanban`

The repository root includes:

```text
.kander/
archived/
done/
review/
todo/
trash/
working/
```

The main artifacts visible inside task directories include:

```text
spec.md
plan.md
report.md
reviews/
```

Later tasks also include:

```text
review plan
batch
run
manifest.json
assignment.json
finding evidence
closure/disposition artifacts
.kander/groups/... dispatch manifests
```

### Kander itself

`dualface/kander`

The analysis cross-referenced, in particular:

- `rules/KANDER-REVIEW-RULES.md`
- `rules/KANDER-TASK-INTAKE-RULES.md`
- `rules/KANDER-TASK-GROUP-RULES.md`
- and the board / dispatch / terminal / review / issue / TUI semantics and implementation boundaries examined in the earlier project review.

---

## 1.2 Method Used in This Analysis

This was not a line-by-line classification of every task. Instead, the analysis combined:

1. the overall board directory structure;
2. the historical tree under `done/`;
3. longitudinal samples from early, middle, and recent tasks;
4. `spec/plan/report/reviews` artifacts for larger tasks;
5. `.kander` durable-dispatch data;
6. corresponding source commits;
7. current Kander rule semantics;
8. real-time state changes observed on the live board.

The goal was to determine:

- how the workflow evolved;
- which rules clearly came from real incidents;
- whether review produces tangible value;
- where Kander is strong or friction-heavy in real engineering work;
- what the current data can and cannot prove;
- how the nearly 1,000 historical tasks could become a durable data asset.

---

# 2. Data Reliability and Important Limitations

This section matters because ignoring these limitations would make it easy to produce conclusions that look impressive but are wrong.

## 2.1 “Nearly 1,000 tasks” is not the same as productivity

Nearly 1,000 tasks across 49 calendar days works out to roughly 20 cards/day.

However, several confounding factors apply:

- a small UI adjustment and a large cross-platform protocol task can each count as one card;
- some cards contain only a spec, while others go through multiple review/fix rounds;
- one card may correspond to multiple commits;
- one commit may be only the final consolidation commit for a task;
- task groups can split one goal into several independently accepted cards;
- some cards are fixes, verification, migration, or review follow-ups rather than new features.

Therefore, **card count is a throughput metric, not a measure of value or difficulty.**

## 2.2 The board is a live system, not a static data warehouse

During the analysis, the contents of `review/` changed: different review cards were visible at different reads.

That is itself useful evidence—the system is actively being used—but it means:

> You cannot infer historical completion rates from the point-in-time proportions of `todo/working/review/done/trash`.

Proper historical state analysis should be based on append-only transition events, not current directory snapshots.

## 2.3 `trash` does not mean failure, and neither does `archived`

Based on naming and context, some trashed or archived work may correspond to:

- designs superseded by newer ones;
- retirement / pruning;
- work merged into another task;
- product decisions to cancel;
- changes in technical direction.

If failure rate is to be measured, the system needs a structured `final_reason`; board columns alone are insufficient.

## 2.4 Incomplete early artifacts do not imply failed execution

In early August, some tasks contain only `spec.md`, while others have `spec/plan/report`.

This more likely reflects:

- an evolving Kander schema / discipline;
- lighter workflows for smaller tasks;
- changes in historical artifact-retention policy.

A missing report should not automatically be counted as a failure or an unverified task.

## 2.5 The current data is suitable for workflow research, but not yet for a model leaderboard

Reviewer metadata is already visible, for example:

```json
{
  "role": "PM",
  "reviewer": "codex",
  "model": "gpt-6-astra",
  "effort": "high"
}
```

Recent reports also show patterns such as:

```text
reviewer codex (gpt-5.6-sol, high)
PMQA (grok)
```

But simply counting “which model found more bugs” would be seriously misleading because:

- different models receive tasks of different difficulty;
- roles differ;
- review bases and change sizes differ;
- more findings do not necessarily mean a better reviewer—the implementer may simply have produced worse code;
- some reviewers focus on security while others focus on PMQA;
- effort levels differ;
- some findings may later be rejected.

Model comparison therefore needs task-complexity normalization and finding disposition, not raw finding counts.

---

# 3. The Real QuickTUI Workload

## 3.1 A highly heterogeneous task set

Based on task names and reports, the workload includes at least the following categories.

### Desktop / Qt / QML

- UI layout
- resource/QRC
- model management
- command palette
- file preview
- terminal UX

### iOS

- terminal wire
- native WebSocket
- relay identity/tunnel
- D-pad
- shortcut bar
- editor/search bar
- simulator/build validation

### Android

- Relay E2E
- liveness/heartbeat
- authentication
- fragmentation
- reconnect convergence
- session lifecycle
- native terminal bridge

### Server / Relay / Cloud

- account login/session
- QTRID owner binding
- relay auth
- PostgreSQL / Redis
- CLI
- terminal transport
- capability contract

### Herdr integration

- workspace/tab/pane/session
- focus switching
- startup screen
- real E2E
- plugin
- compatibility matrix

### Build / codegen / docs / contract

- protocol code generation
- locale generation
- cross-platform compile
- release/build constraints
- runbooks
- contract documents

The research value of the dataset is therefore that it covers:

> **UI, state machines, concurrency, networking, authentication, persistence, cross-platform behavior, protocols, code generation, testing, deployment, and verification.**

It is much closer to real product development than a synthetic agent benchmark centered on fixing a few Python bugs from GitHub issues.

---

# 4. Workflow Evolution: From “Task Records” to an “Execution Protocol”

This is the most important long-term trend in the dataset.

## 4.1 Early August: already more than a raw agent, but artifacts were still human-report oriented

For example:

`done/20260801-agent-model-effort-settings-task/`

already contains:

```text
spec.md
plan.md
report.md
```

And by August 2:

`done/20260802-server-account-login-relay-bind-task/report.md`

already records:

- actual changes;
- final commit;
- develop integration;
- multi-platform tests;
- review and corrections;
- accepted risks;
- validations not run, with reasons.

This shows that even at an early stage, Kander's core idea was not:

> “Let the agent write code and stop when it says it is done.”

Instead, it was:

> “A task should produce an engineering delivery record that is explainable, verifiable, and closable.”

At this point, however, much of the review evidence still lived as natural-language content inside reports.

---

## 4.2 Mid-August: multi-role review + real E2E had already become engineering gates

`20260815-herdr-e2e-hardening-task` is an excellent mid-period example.

The task covered:

- Linux/macOS Herdr real E2E;
- workspace/tab/pane/layout;
- attach/output/input/resize/scroll;
- restart/network reconnect;
- SID/tombstone;
- Direct/Relay;
- PostgreSQL/Redis;
- iOS/Android compatibility;
- plugin behavior;
- resource cleanup.

The report records verification across real environments including:

- official Linux Herdr;
- a macOS remote runner;
- a Docker real relay;
- PostgreSQL / Redis;
- iOS Simulator;
- Android JVM/full unit tests;
- Windows cross-linking;
- an independent plugin repository.

The review stage still used the historical four-role model:

```text
PM    (Grok)
QA    (Codex)
CSA   (Grok)
Hacker(Grok)
```

In the first round, PM caught a representative engineering problem:

> The E2E harness inherited host `HERDR_* / XDG_*` environment state and could accidentally connect to a real user session.

After the fix, E2E was not only rerun, but rerun specifically under deliberately polluted environment variables.

This is an important signal:

> Review was not merely inspecting business logic; it was pushing the test environment from “possibly passing by accident” toward “repeatable, isolated, and trustworthy.”

---

## 4.3 September: review had become a machine-traceable protocol

By September, task artifacts were noticeably more formalized.

For example, the review manifest for `20260912-android-e2e-liveness-auth-task` records:

```text
schema
findings_schema
task_group
run_id
batch_id
task_ids
role
reviewer
model
effort
cwd
base
commit
report_language
input hashes
output hashes
```

This is no longer just “writing a log for a review.”

It establishes:

> **An unambiguous review fact: who reviewed which base→commit, in which role, with which model/effort level, against which input-context hashes, and what result they produced.**

For AI-agent systems, this provenance matters because model output is inherently not a deterministic build artifact.

If someone later asks:

> “Who issued this PASS, and against exactly which version of the code?”

Kander already has the information needed to answer.

---

# 5. The Strongest Evidence: Independent Review Still Finds Real Problems After All Tests Are Green

## 5.1 Android E2E liveness/auth case

Task:

`done/20260912-android-e2e-liveness-auth-task/`

The first author-side verification already reached:

- Android tests: **2,889 passed**
- lint: pass
- compile/build target: pass

In a conventional CI workflow, this task could easily have been considered complete.

But independent PM/QA review continued and found multiple classes of issues.

### A. Heartbeat lifecycle-state separation error

After background suspension, a late pong could still restart the timer, causing background ping / death detection to resume.

Conceptually, the problem was:

```text
authentication eligibility state
!=
current heartbeat runtime state
```

### B. Heartbeat write failure was swallowed

The ping state had already moved to outstanding, but when the actual send failed, the error did not correctly flow into `failReader`.

This kind of bug is difficult to catch with happy-path unit tests.

### C. Pong logic for a lazy authenticated tunnel was incorrectly gated by an active flag

As a result, if no later application write occurred, a server heartbeat ping might not receive a pong promptly.

### D. Connection generation / stale callback issue

A delayed exception from an old reader could mutate shared heartbeat state before the generation check, affecting the new connection.

This is a classic asynchronous epoch/generation race.

### E. Epoch / outstanding-state inconsistency on repeated resume

If resume occurred again before the previous ping completed:

- epoch advanced;
- outstanding state was not cleared;
- the new ping was blocked;
- the old deadline exited because the epoch no longer matched.

The end state could look “ready” while liveness checking was effectively dead.

### F. Managed-auth lease leak

Repeated authentication of the same scope could increment the reference count, while the fast-return path failed to release it correspondingly.

### G. Test-validity problems

The reviewer explicitly noted that some tests merely:

- tested a routing helper;
- tested a budget helper;
- performed source-string `contains/indexOf` checks;

without actually verifying the lifecycle side effects required by the task's acceptance criteria.

This deserves special attention because it is a typical failure mode in AI coding:

> **The model writes tests that are formally green and make the acceptance package look complete, while failing to exercise the actual behavioral semantics.**

### H. Mechanical dead code

The reviewer also found newly added but unused test seams / legacy functions and comments that no longer matched the implementation.

---

## 5.2 Findings did not stop at the report—they entered durable fix dispatch

`.kander/groups/00000000-dispatch-group/20260912-android-liveness-fix1.json`

explicitly stores:

```text
dispatch_id
task_id
kind = fix
review batch
group branch
current target commit
PM findings
QA findings
specific code evidence
minimal fix suggestions
```

In other words, review findings were not merely “suggestions in a chat transcript.”

They were converted into:

> **Recoverable, auditable next-round work bound to a task and a specific code version.**

This is one of the clearest differences between Kander and many systems that amount to “Agent A writes code, Agent B leaves a review comment.”

---

## 5.3 Final convergence

The task went through multiple rounds of fixing, synchronization, and re-review and ultimately reached:

- **2,935 tests passed**
- lint pass
- build/compile pass
- final PM/QA PASS

So the review process changed not only the code; **it expanded the verification surface itself.**

This case supports a strong conclusion:

> For real AI coding workflows, “author model + automated tests” is not sufficient to replace independent review. One of review's most important functions is discovering where the tests themselves fail to encode the true contract.

---

# 6. Another Key Case: A Small D-pad Feature Exposed a Pre-existing Codegen Defect

Task:

`done/20260917-dpad-3x3-reduce-keys-task/report.md`

On the surface, this was an ordinary UI change:

> Reduce the D-pad from 4×5 to 3×3.

In practice, it touched:

- protocol contract;
- Swift codegen;
- Kotlin codegen;
- TypeScript generated artifacts;
- iOS dead-code cleanup;
- Android dead-code cleanup;
- i18n source + generated output;
- Android release build;
- remote iOS build.

During the task, the workflow discovered that:

> An empty dictionary was being generated as an invalid literal on the Swift side.

This defect was not part of the original UI feature, but was exposed and fixed because the workflow exercised a real cross-platform build.

The PMQA review used:

```text
reviewer: codex
model: gpt-5.6-sol
effort: high
```

It found a medium-severity mechanical/documentation issue, which was fixed and closed.

Security was explicitly recorded as N/A because the task did not involve a trust boundary, credentials, or a remote-execution surface.

This case illustrates another strength of Kander's later workflow:

> **Not every task is forced through every review role; role applicability has itself become part of the protocol.**

That helps control the cost of the quality process.

---

# 7. The Review System: Kander's Strongest Product Capability Today

## 7.1 It is not simply “have a second model look at the code”

The key semantics in the current `KANDER-REVIEW-RULES.md` include:

- fixed review base;
- explicit role;
- configurable reviewer/model;
- reviewer isolation;
- full first review;
- incremental re-review for fix rounds;
- finding severity;
- independent verification of findings by the main agent;
- chain/closure constraints on reviewer PASS;
- batch semantics for task-group review;
- hashed input/output artifacts;
- immutable historical review artifacts.

This makes review much closer to:

> **An auditable quality-gate protocol**

than to:

> **A single prompt call.**

## 7.2 Independent verification of reviewer findings is critical

AI reviewers can hallucinate too.

The correct flow is therefore not:

```text
reviewer: there is a bug
→ fix immediately
```

It should be:

```text
reviewer raises a finding
→ main/executing agent independently verifies the evidence
→ confirmed / rejected / unverifiable / user decision
→ fix only if confirmed
→ reviewer re-reviews only the changed delta
```

Kander's current rules are already moving in this direction.

This is a design principle worth preserving.

## 7.3 Converging from four roles to PMQA + Security is a good move

Historical data shows:

```text
PM
QA
CSA
Hacker
```

The current rules have converged to:

```text
PMQA
Security
```

while retaining compatibility with historical artifacts.

I think this direction is sound.

Too many review roles create:

- duplicate findings;
- reviewer cost;
- stage waiting time;
- finding-reconciliation overhead;
- repeated review of the same context across multiple models.

Combining product fit, correctness, testing, architecture, and code quality into PMQA while keeping the security boundary separate is a reasonable minimal layering.

---

# 8. Model Orchestration: Kander Already Has the Beginnings of Role-Based Routing

Historical records show at least:

- Grok used for PM/CSA/Hacker;
- Codex used for QA;
- Codex + `gpt-5.6-sol high` used for PMQA;
- Grok used for recent PMQA;
- review manifests containing model labels such as `gpt-6-astra high`.

The important point is not which of these particular models is “best.” It is that:

> **Kander already treats models as role executors instead of binding the entire workflow to one provider.**

That is the foundation for genuine model-aware routing.

But it would be premature to build a “leaderboard.”

The right question is:

> Under comparable complexity / domain / task-type conditions, which model produces the highest accepted outcome for a given role, at what cost?

For example, future analysis could separate:

```text
implementation:
  UI/QML
  Kotlin concurrency
  Go backend
  protocol/codegen

review:
  correctness
  concurrency/state-machine
  cross-platform integration
  security
  spec completeness
```

A model may be:

- fast at implementation;
- weak at review;

while another model may exhibit the opposite pattern.

This closely matches the user's existing observation that a strong reviewer is needed to complement an implementation model.

---

# 9. Controlled Parallelism Matters More Than “More Concurrent Agents”

`KANDER-TASK-INTAKE-RULES.md` and `KANDER-TASK-GROUP-RULES.md` currently emphasize:

- overlap checks between expected edit paths and `working/review` tasks;
- group-branch overlap;
- hotspot files;
- prerequisites;
- shared contracts;
- path-level isolation;
- not splitting the same resource across multiple cards merely to gain parallelism;
- slicing transport/contract changes by deliverable rather than mechanically by layer.

This is a mature direction.

The largest hidden cost in multi-agent systems is usually not CPU/GPU or API spend. It is:

```text
parallel implementation
→ architecture divergence
→ semantic conflict
→ merge conflict
→ duplicate tests
→ shared contract drift
→ exploding review context
```

The metric to maximize is therefore not:

> concurrent agents

but:

> **safe, useful parallelism**.

This should remain a core Kander product principle.

---

# 10. Durable Dispatch / Recovery: A Hidden but Important Capability

`KANDER-TASK-GROUP-RULES.md` contains an especially correct definition:

> “once” refers to a logical dispatch ID; it does not promise exactly-once external side effects.

This effectively treats AI coding orchestration as a distributed execution system.

In the real world, the following can happen:

```text
Kander crashes after writing intent
The agent received the prompt, but the receipt was not persisted
A terminal session was created, but the main process lost contact
The agent completed the work, but the board transition failed
A notification retries
The same fix round is dispatched more than once
```

If the system relies on “we probably sent the prompt,” state quickly becomes impossible to explain.

Kander's combination of dispatch ID / receipt / epoch / task state / worktree / branch is designed to handle exactly this class of problem.

Users may rarely notice this capability directly, but it is likely one of the reasons the system can sustain close to a thousand tasks over time.

---
# 11. Current WIP Shape: Healthy at a Glance, but Easy to Over-Interpret

At one snapshot during the analysis:

- `todo`: very few;
- `working`: a small number;
- `review`: a small number;
- `done`: a large historical set;
- `trash/archived`: relatively few.

The contents of review also changed while the analysis was underway.

From an operational perspective, this at least suggests that:

> Kander does not currently keep hundreds of tasks sitting indefinitely in “in progress.” It appears to favor low WIP and fast flow.

However, because there is no complete state-transition event log, this report does not use that snapshot to compute:

- historical WIP;
- average cycle time;
- completion rate;
- abandonment rate.

These should be among the first metrics added by the next analytics layer.

---

# 12. Major Failure / Risk Patterns Visible in the Task Records

Below is the defect taxonomy I would recommend for historical tasks.

## 12.1 Concurrency / lifecycle / generation

This is currently the most important class worth separating explicitly.

Typical problems include:

- callbacks from an old reader affecting a new generation;
- epoch/state not being fully reset after reconnect;
- cancellation interleaving with a new connection;
- heartbeat suspend/resume;
- lease/refcount lifecycle errors.

Characteristics of this class:

- unit tests often miss it;
- static diff review can be valuable;
- property/state-machine testing can be very valuable;
- reviewers with strong state-machine reasoning can provide high leverage.

Recommended taxonomy:

```text
concurrency.lifecycle
concurrency.generation
concurrency.cancellation
concurrency.refcount
state_machine.liveness
```

## 12.2 False-green tests / insufficient behavioral tests

Examples include:

- testing only a helper function;
- checking source-code strings only;
- never triggering the actual side effect;
- validating release within the same scope, which cannot prove that cross-scope exclusion was actually released.

Recommended categories:

```text
test.false-green
test.behavior-gap
test.missing-negative-path
test.environment-leak
```

This may become one of the most interesting data categories for AI coding research.

## 12.3 Environment contamination

Herdr E2E exposed cases involving:

- inherited host `HERDR_*` variables;
- inherited XDG pointers;
- potential accidental connection to a real user session.

Recommended categories:

```text
env.isolation
env.shared-state
env.non-hermetic-test
```

## 12.4 Cross-platform contract/codegen drift

The D-pad task exposed a Swift empty-dictionary codegen defect.

This class of problem commonly appears in pipelines such as:

```text
source contract
→ Swift generated
→ Kotlin generated
→ TypeScript generated
```

Recommended categories:

```text
contract.codegen
contract.platform-parity
contract.generated-drift
```

## 12.5 Documentation / dead-code mechanical defects

Kander currently treats the following as at least medium-severity mechanical findings:

- dead code;
- redundant tests;
- documentation/comments inconsistent with the implementation.

There is a reasonable argument for this policy. Individually, these defects may be low impact, but in a high-frequency AI code-generation environment they can accumulate entropy quickly if they are never cleaned up.

That said, the policy should eventually be validated empirically:

> Is the gate cost of mechanical findings actually worth it?

If they fire constantly and almost never affect future maintenance, a quick workflow may need a lighter standard. If they frequently correlate with misleading behavior or incorrect wiring, the rule is worth preserving.

## 12.6 Product / design supersession

Items in trash/archived may correspond to:

- older designs superseded by new ones;
- retirement;
- merging into another task;
- a change in product direction.

These must not be counted as implementation failures.

Recommended structured reasons:

```text
completed
superseded
cancelled_product_decision
duplicate
merged_into_other_task
failed_technical
blocked_external
invalid_spec
abandoned_cost
```

---

# 13. Existing Rules That Appear to Be Validated by Real Work

## 13.1 Review-base freeze

**Recommendation: keep it.**

Without a fixed base/commit, there is no reliable way to know what the reviewer actually reviewed.

Historical manifests show that this mechanism is practical in real use.

## 13.2 Incremental re-review

**Recommendation: strongly keep it.**

If every fix triggered a complete re-review of a large task, the workflow would incur major costs in:

- latency;
- token usage;
- reviewer drift;
- duplicate findings.

Incremental review also more closely resembles real code-review practice.

## 13.3 Independent verification of findings

**This is a core principle and should not be weakened.**

A reviewer is not an oracle.

Future metrics should explicitly track:

```text
confirmed
rejected
unverifiable
accepted-risk
mechanical-confirmed
```

Only then can reviewer precision be evaluated meaningfully.

## 13.4 Task-overlap / hotspot serialization

**Strongly supported by the structure of the real project.**

QuickTUI has many natural concurrency hotspots:

- generated files;
- protocol schemas;
- shared locales;
- project files;
- terminal transport contracts.

These resources are inherently risky to modify in parallel.

## 13.5 Task-group dependency graph

**Important for cross-platform and cross-contract work.**

By September, the history already shows large cards combined with task groups and shared-batch review.

## 13.6 Durable dispatch ID

**Highly necessary.**

It becomes especially important once review findings automatically trigger fix rounds, because it provides the basis for recovery semantics.

## 13.7 Explicit Security N/A

**Better than silently skipping security review.**

It preserves the answer to:

> Why did this task not run Security review?

rather than leaving only the absence of a security artifact.

---

# 14. Where the System May Be Becoming Too Heavy

Kander's primary risk is no longer simply “not enough capability.” It is:

> **Protocol complexity may begin to exceed its marginal benefit.**

The current conceptual surface already includes:

```text
card state
task group
group branch
anchor
review plan
batch
run
round
review base
reviewed commit
finding
finding disposition
closure
waiver
recovery
dispatch id
epoch
receipt
original artifact
summary/report
```

Every concept is individually defensible.

Together, however, they create two risks.

## 14.1 Agent cognitive load

The longer the rules become, the more likely it is that:

> The agent is perfectly capable of writing the code, but forgets a procedural requirement.

The system then adds another rule to prevent agents from forgetting rules, creating a feedback loop.

## 14.2 Workflow tax

If a five-line bug fix receives the same ceremony as a large cross-platform task, the system will feel sluggish.

I therefore recommend formally introducing workflow risk profiles such as:

```text
quick
standard
strict
critical
```

The goal should not be to expose a large settings panel to the user. Kander should infer or recommend a level from signals such as:

- change size;
- subsystem touched;
- auth/security exposure;
- external contract exposure;
- concurrency involvement;
- generated/shared hotspots;
- historical defect rate.

---

# 15. The Largest Product Gap Today: Analytics Is Not a First-Class Citizen

The data is already rich, but most of it is scattered across:

```text
spec.md
plan.md
report.md
review artifacts
.kander manifests
Git commits
board directory state
```

This is excellent for human auditability, but awkward for long-term statistics.

## 15.1 Add an append-only event stream

I recommend keeping the board directory as a human-readable projection while also writing something like:

```text
.kander/events/events-YYYYMM.jsonl
```

or a local SQLite database.

Each event could look like:

```json
{
  "schema": 1,
  "event_id": "...",
  "at": "2026-09-18T01:23:45+08:00",
  "task_id": "...",
  "event": "review.finding.disposition",
  "actor": {
    "role": "implementer",
    "agent": "...",
    "model": "...",
    "effort": "high"
  },
  "review": {
    "batch_id": "...",
    "run_id": "...",
    "finding_id": "PMQA-001"
  },
  "data": {
    "disposition": "confirmed"
  }
}
```

## 15.2 Recommended event set

At minimum:

```text
task.created
task.planned
task.picked
task.started
task.state_changed
task.completed
task.archived
task.cancelled

dispatch.created
dispatch.accepted
dispatch.reconciled
dispatch.failed
session.started
session.lost
session.recovered

implementation.commit
verification.started
verification.completed

review.plan_created
review.run_started
review.run_completed
review.finding_created
review.finding.disposition
review.fix_dispatched
review.batch_closed

integration.started
integration.completed
integration.conflict

rule.triggered
rule.waived
rule.blocked
```

With this in place, almost every important future analysis can be performed without reparsing Markdown.

---

# 16. Strong Recommendation: Structure Finding Disposition

This is the most important missing dataset for evaluating reviewer quality.

Each finding should include at least:

```text
finding_id
role
reviewer/model/effort
severity
category
observed | inferred
confidence
file/path
introduced_by_current_task?
disposition
verification_method
fix_commit
re_review_run
```

Disposition values might include:

```text
confirmed
rejected
unverifiable
accepted_risk
superseded
mechanical_confirmed
```

Only with this data can Kander answer questions such as:

### Reviewer precision

```text
confirmed findings
------------------
all verifiable findings
```

### False-positive rate

```text
rejected findings
-----------------
all verified findings
```

### High-severity precision

These are much more meaningful than “how many findings did the reviewer produce?”

---

# 17. Add Incident Provenance to Every Important Rule

One of Kander's most distinctive characteristics is that its rules visibly appear to have evolved from real incidents.

Today, however, most of the causal relationship between incident and rule exists only in maintainer memory.

I recommend attaching machine-readable provenance to every important rule, for example:

```yaml
rule_id: review.incremental.same-base
introduced: 2026-09-xx
reason:
  - task: 20260912-...
    incident: "full re-review caused ..."
enforcement: protocol
risk: reviewer_drift
last_triggered: 2026-09-18
```

For every rule, Kander could eventually answer:

1. What incident originally motivated it?
2. How often did it trigger in the last 30 days?
3. How many problems did it actually prevent?
4. How much time does it add to tasks?
5. Has it already been replaced by code-level enforcement?
6. Does it still need to exist as a prompt rule?

This would evolve the rule set from:

> a long policy document

into:

> **an operational policy system with incident evidence, measurable effects, and the ability to retire obsolete rules.**

---

# 18. Core KPIs Worth Establishing

Do not use “number of agents” or “cards per day” as primary KPIs.

I recommend five layers.

## 18.1 Flow

```text
created tasks / day
accepted tasks / day
WIP
lead time
cycle time
queue time
review wait time
integration wait time
```

Bucket these by task size / risk / category.

## 18.2 Quality

```text
first-pass acceptance rate
review rounds per task
confirmed findings per task
must-fix findings per task
post-done defect reopen rate
escaped defect rate
```

## 18.3 Review quality

```text
finding confirmation rate
finding rejection rate
finding unverifiable rate
high/medium precision
incremental review recurrence rate
review finding → regression test conversion rate
```

The last metric deserves particular attention:

> How many bugs found by review eventually become durable automated regression tests?

That indicates whether the system is actually learning.

## 18.4 Reliability

```text
dispatch retry rate
same-ID reconciliation rate
lost-session rate
recovery success rate
CAS conflict rate
stale state rejection rate
integration conflict rate
```

## 18.5 Economics

If token/cost/latency data becomes available:

```text
cost / accepted task
cost / complexity point
review cost / confirmed must-fix
latency / accepted task
cost of rework
```

These metrics could directly guide model routing.

---

# 19. How Model Routing Should Be Evaluated Scientifically

The future question should not be:

> “Which model is best at coding?”

Instead, construct something closer to:

```text
P(success | model, role, task_type, complexity, context_size, effort)
```

## 19.1 Implementation side

Recommended stratification dimensions:

```text
language
subsystem
task type
files changed
LOC delta
contract surface
concurrency risk
cross-platform count
```

## 19.2 Review side

Primary metrics:

```text
confirmed finding precision
must-fix recall proxy
false positive rate
review latency
review token cost
fix convergence rounds
```

## 19.3 Avoid a common trap

A reviewer finding more bugs does not automatically mean that reviewer is stronger.

It may simply have reviewed worse implementations.

Calibration should therefore use approaches such as:

- matched pairs of similar tasks;
- small blinded dual-review samples on the same commit;
- randomized benchmark sampling;
- hidden seeded defects.

---

# 20. Kander Could Build Its Own Real-World Benchmark

I think this is a particularly promising direction.

There is no need to simply reproduce SWE-bench.

Kander has a distinct advantage:

> **Real tasks include not only issue + patch, but the entire plan → execution → review → finding → fix → re-review → integration lifecycle.**

Historical tasks could be anonymized into several benchmark types.

## 20.1 Implementation benchmark

Provide:

- base commit;
- task spec;
- acceptance criteria.

Hide:

- final patch;
- review findings;
- regression tests.

Then measure whether an agent can complete the task independently.

## 20.2 Review benchmark

Provide:

- task contract;
- base;
- candidate commit.

Hide:

- historical confirmed findings.

Then measure whether the reviewer can rediscover real historical defects.

This is more valuable than artificially seeded bugs because the findings came from real development.

## 20.3 Orchestration benchmark

Provide a set of dependent and overlapping tasks and ask the orchestrator to decide:

```text
split
serialize
parallelize
group
review
integration order
```

That is much closer to Kander's true core capability.

---
# 21. Reviewer Hard Sandbox Should Remain a High Priority

Kander already uses several protections depending on reviewer CLI capabilities:

- read-only shell access;
- tool allowlists;
- removal of Edit/Write capabilities;
- prompt restrictions;
- post-run validation of Git-visible state.

But the rules themselves correctly acknowledge that:

> Writes outside the worktree, as well as writes to ignored paths, cannot be fully detected through Git state alone.

Long term, the reviewer trust boundary should therefore be enforced at the OS layer.

### Linux

- mount namespace;
- read-only bind mounts;
- user namespace;
- seccomp;
- network policy.

### macOS

- evaluate Seatbelt/sandbox mechanisms or disposable container/VM runners.

### Windows

- Job Objects / AppContainer / disposable sandbox runners, or similar mechanisms.

The core principle is:

> A reviewer's inability to write should ideally be guaranteed by the operating system, not by the model's willingness to follow instructions.

---

# 22. Rule Complexity: Introduce Risk-Tiered Workflows

I recommend four internal tiers, not all of which necessarily need to be exposed directly to users.

## Quick

Typical characteristics:

- one small file;
- non-core code;
- no public contract;
- no auth/security impact;
- no concurrency/state-machine behavior.

Flow:

```text
implement
→ targeted verification
→ delivery check
```

## Standard

Ordinary feature/bug work:

```text
implement
→ tests
→ PMQA
→ integrate
```

## Strict

Typical characteristics:

- cross-platform;
- protocol changes;
- concurrency;
- persistent state;
- task groups.

Add:

- stronger review;
- cross-platform verification;
- batch/provenance requirements;
- regression requirements.

## Critical

Typical characteristics:

- authentication;
- credentials;
- cryptography;
- sandboxing;
- migrations;
- irreversible user data.

Add:

- mandatory Security review;
- hard-sandboxed reviewer;
- possibly two independent reviewers;
- stronger release gates.

This avoids forcing small tasks through the full weight of a large-task protocol while preserving stronger controls on critical paths.

---

# 23. Kander Should Not Become an AI IDE

The data makes me more confident about this than before.

QuickTUI is already a complex product, and the value Kander provides is not:

- an editor;
- a file browser;
- browser preview;
- a Git GUI;
- a chat UI.

Its value is closer to:

```text
intent
→ task contract
→ split/dependency
→ safe scheduling
→ isolated execution
→ durable dispatch
→ verification
→ independent review
→ finding verification
→ repair
→ incremental review
→ integration
→ cleanup
→ provenance
```

That is a fundamentally different product direction.

If Kander stays focused here, it has a chance to become:

> **a coding-agent workflow runtime / control plane**

rather than another agentic IDE in a crowded market.

---

# 24. Reassessing Kander's Current Moat

Previously, I would have attributed Kander's moat mainly to its architecture and review protocol.

After examining the QuickTUI history, I would now describe it in four layers.

## Layer 1: Code

Can be copied.

## Layer 2: Rules

Can also be copied, but the copier does not know which rules are actually important.

## Layer 3: Causal experience connecting incidents to rules

This is where replication becomes harder.

For example:

> Why is a generation fence necessary?  
> Why should fix re-review be incremental?  
> Why must the task-group branch anchor be frozen?  
> Why can a reviewer finding not be accepted blindly?  
> Why must test-environment isolation be strict?

That knowledge comes from real failures.

## Layer 4: Long-term task provenance dataset

This is the hardest layer to copy.

Once the dataset reaches:

```text
1,000
5,000
10,000
```

real tasks, Kander can begin answering empirically:

- Which kinds of tasks fail most often?
- Which models are better for implementation?
- Which models are better for review?
- Which finding categories are most often confirmed?
- Which rules rarely trigger?
- Which modules generate the most rework?
- Which task-splitting strategies most often lead to integration conflict?

At that point, Kander's advantage is no longer merely software functionality.

---

# 25. Recommended Roadmap

## P0: Do Now

### 1. Analytics/event schema

Do not wait for more tasks.

The longer this is delayed, the more historical data will require reverse-parsing from Markdown.

### 2. Structured finding disposition

This is foundational for evaluating reviewer/model quality.

### 3. Rule provenance

Link rules to real incidents/tasks.

### 4. Stable schema versioning

Assign explicit versions to:

- task metadata;
- reports;
- reviews;
- dispatch records;
- events.

---

## P1: Near Term

### 5. Analytics CLI

For example:

```bash
kander analytics summary --since 30d
kander analytics export --format jsonl
kander analytics export --format parquet
kander analytics model-matrix
kander analytics review-quality
kander analytics rules
```

### 6. Risk profiles

Quick / Standard / Strict / Critical.

### 7. Failure taxonomy

Classify historical real-world issues. Reviewers may suggest a category automatically, but closure should make the final classification durable.

### 8. Model-aware routing

Start with recommendations; do not immediately automate all model selection.

---

## P2: Medium Term

### 9. Hard reviewer sandbox

OS-level enforcement.

### 10. Historical benchmark builder

Generate anonymized replay cases from real tasks.

### 11. Conflict-risk prediction

Use historical path overlap, hotspots, and integration conflicts to improve task splitting and scheduling.

### 12. Rule lint / dead-rule detector

Identify:

- rules that have not triggered for a long time;
- prompt rules already replaced by code enforcement;
- overlapping or redundant rules.

---

## P3: External Communication

### 13. Publish a “1,000-task retrospective”

Do not center the message on claims like:

> “AI made me X times more productive.”

The current data does not provide a reliable baseline for that statement.

More valuable public material would cover:

- a real failure taxonomy;
- how review catches false-green tests;
- how the workflow evolved from August to September;
- why controlled parallelism matters more than agent count;
- which rules were forced into existence by real incidents.

That would be more credible than a marketing-style benchmark.

---

# 26. Recommended Data Schema

Below is a draft that could be implemented directly.

## Task

```yaml
task_id:
task_group_id:
parent_task_id:
title:
type:
size:
risk_profile:
created_at:
started_at:
completed_at:
final_status:
final_reason:
origin:
  type: user|issue|review_finding|followup
  ref:
```

## Execution

```yaml
actor_role:
agent:
provider:
model:
effort:
session_id:
dispatch_id:
terminal_backend:
branch:
worktree:
base_commit:
head_commit:
```

## Change

```yaml
files_changed:
code_files_changed:
lines_added:
lines_deleted:
subsystems:
languages:
contracts_touched:
security_surface:
concurrency_surface:
```

## Verification

```yaml
command:
environment:
started_at:
finished_at:
exit_code:
passed:
test_count:
failed_count:
skipped_count:
artifact_ref:
```

## Review Run

```yaml
plan_id:
batch_id:
run_id:
round:
role:
reviewer:
model:
effort:
base:
commit:
previous_run:
full_or_incremental:
result:
latency_ms:
tokens_in:
tokens_out:
cost:
```

## Finding

```yaml
finding_id:
severity:
category:
mechanical:
observed_or_inferred:
confidence:
path:
evidence:
disposition:
verified_by:
fix_task:
fix_commit:
closed_by_run:
```

## Reliability Event

```yaml
event:
dispatch_id:
epoch:
error_class:
retry_count:
recovered:
recovery_latency_ms:
```

---

# 27. Recommended Rule-Audit Template

For future audits of `rules/`, I recommend evaluating every important rule with a template like this:

```markdown
## Rule: review.incremental.same-base

### Purpose
Prevent conclusion-chain drift caused by restarting a full review after a fix.

### Provenance
- Incident/task:
- Date:
- Historical symptom:

### Enforcement
- prompt-only / CLI validation / transaction / sandbox / test

### Trigger frequency
- 7d:
- 30d:
- all-time:

### Prevented failures
- count:
- examples:

### Cost
- median latency:
- model tokens:
- user friction:

### False-positive / unnecessary trigger
- count:

### Replacement
Can this now be fully replaced by code-level enforcement?

### Decision
keep / simplify / automate / remove
```

This would evolve Kander rule maintenance from “editing policy text” into real operational engineering.

---

# 28. Recommended Order for Historical Data Analysis

If the next step is to dig more deeply into the existing near-1,000-task history, I recommend this order.

## First batch: easiest to obtain, highest value

1. cards created/completed per day;
2. small/large ratio;
3. task-group ratio;
4. percentage with/without independent review;
5. review-round distribution;
6. must-fix finding distribution;
7. finding categories;
8. completed / superseded / cancelled / failed reasons;
9. subsystems touched;
10. model/role distribution.

## Second batch: requires joining with Git

1. task → commit mapping;
2. change size;
3. files touched;
4. hotspots;
5. integration conflicts;
6. whether later bugs modify the same areas.

## Third batch: where the durable competitive advantage begins

1. confirmed finding rate;
2. escaped defect rate;
3. task-complexity-normalized model quality;
4. rule effectiveness;
5. review ROI;
6. relationship between scheduling/splitting strategy and rework.

---

# 29. Research Questions This Dataset Can Support

The dataset is already rich enough to support serious questions in AI software engineering.

For example:

### RQ1
How many confirmed must-fix defects can an independent reviewer still find after the author's tests are fully green?

### RQ2
How sensitive are different defect categories to the choice of reviewer model?

### RQ3
Does using different model families for implementation and review reduce correlated failure?

### RQ4
Can incremental re-review reduce cost relative to full re-review without increasing escaped defects?

### RQ5
How much does task-group path-overlap prevention reduce integration conflict?

### RQ6
Which kinds of acceptance criteria most often produce false-green tests?

### RQ7
At what level of rule complexity do agents begin making more procedural errors?

### RQ8
In real engineering work, what is the relationship between optimal parallelism and the number of agents?

These questions are closer to what real developers care about than “what score did a model get on SWE-bench?”

---

# 30. Revised Assessment of Kander

Based on this production history, I would update the earlier evaluation as follows.

| Dimension | Previous assessment | Assessment after real-task evidence |
|---|---:|---:|
| Product positioning | 8.5 | **9.0** |
| Differentiation | 9.0 | **9.5** |
| Orchestration protocol design | 9.0 | **9.5** |
| Review system | 9.5 | **9.5** |
| Production validation | 6.5–7 | **9.0** |
| External ecosystem validation | 4.5 | **still ~4.5** |
| Analytics / observability | 6 | **6.5** |
| Data-asset potential | previously underweighted | **9.5** |
| Workflow-complexity risk | medium | **medium-high** |

The most important change is this:

> I previously worried that many parts of the Kander protocol might be design-first. The QuickTUI history makes me much more confident that a substantial portion of the protocol was forced into existence by real production friction.

That does not mean every rule deserves to remain forever.

Quite the opposite: now that Kander has real history, it can finally begin using data to **remove** rules rather than only adding them.

---

# 31. Final Assessment

If someone looks only at Kander's GitHub front page, it is easy to interpret it as:

> Kanban + multiple AI coding agents.

But when viewed together with the QuickTUI task history, a more accurate definition is:

> **A local-first workflow runtime that turns AI coding from interactive chat into a recoverable, auditable, traceable, parallel-but-constrained software-engineering execution process.**

The most valuable areas to strengthen are not “more agents” or “a prettier board,” but four things.

### 1. Provenance

Every execution, review, finding, fix, and recovery should be able to answer precisely: “What happened?”

### 2. Empirical policy

Rules should be able to answer: “Why do I exist, and am I still useful?”

### 3. Model specialization

Implementation / planning / review / security should select different models based on historical performance rather than assuming one universal model.

### 4. Learning loop

A problem discovered in review should do more than fix the current bug. It should be able to become:

```text
regression test
+ rule evidence
+ model-performance evidence
+ scheduling evidence
```

and thereby contribute to system-level learning.

If Kander continues in this direction, its most interesting future is not:

> “A convenient personal multi-agent Kanban board.”

It is:

> **The execution and quality-control layer for AI software engineering.**

The nearly 1,000 QuickTUI tasks already form a rare first body of real training data for that system—not for training model parameters, but for training **the workflow itself**.

---

# Appendix A: Evidence Sampled in This Analysis

## Kander

- `rules/KANDER-REVIEW-RULES.md`
- `rules/KANDER-TASK-INTAKE-RULES.md`
- `rules/KANDER-TASK-GROUP-RULES.md`

## QuickTUI / Kanban

### Early period

- `done/20260801-agent-model-effort-settings-task/`
- `done/20260802-server-account-login-relay-bind-task/report.md`

### Mid-period large E2E

- `done/20260815-herdr-e2e-hardening-task/plan.md`
- `done/20260815-herdr-e2e-hardening-task/report.md`

### September review/fix deep dive

- `done/20260912-android-e2e-liveness-auth-task/report.md`
- `done/20260912-android-e2e-liveness-auth-task/reviews/`
- `done/20260912-android-e2e-liveness-auth-task/reviews/20260912-android-p2-b1-pm-r1/manifest.json`
- `.kander/groups/00000000-dispatch-group/20260912-android-liveness-fix1.json`
- `done/20260912-android-logical-reconnect-convergence-task/report.md`

### Recent cross-platform UI / codegen

- `done/20260917-dpad-3x3-reduce-keys-task/report.md`
- `done/20260917-continuous-switch-server-task/report.md`
- `done/20260917-continuous-switch-client-task/report.md`
- `done/20260917-herdr-tab-startup-screen-task/report.md`

---

# Appendix B: Data Worth Recording Immediately but Not Yet Uniformly Structured

```text
TASK
  created_at
  started_at
  completed_at
  type
  size
  risk
  subsystem

IMPLEMENTER
  agent
  model
  effort
  context size
  tokens/cost

CHANGE
  base/head
  files
  LOC
  contract touched
  concurrency touched

REVIEW
  reviewer
  model
  effort
  role
  round
  full/incremental

FINDING
  severity
  category
  observed/inferred
  confirmed/rejected/unverifiable
  fix commit

FLOW
  state transitions
  wait time
  recovery
  conflict

OUTCOME
  completed/superseded/cancelled/failed
  acceptance result
  escaped bug link
```

---

# Appendix C: Claims the Current Data Does Not Support Directly

To keep future public materials credible, I recommend explicitly avoiding the following claims unless the experimental design is strengthened:

- “Kander makes development X times faster”;
- “Model X is the best coding model”;
- “Reviewer X has a bug-detection rate of X%” without disposition/ground truth;
- “trash rate equals task failure rate”;
- “20 cards/day means completing 20 human-sized tickets/day”;
- “There are no bugs after review” without long-term escaped-defect tracking;
- “Multi-agent is always faster than single-agent”—the real variables are safe parallelism and rework.

A more defensible public statement would be:

> Kander has been used continuously on nearly 1,000 tasks in a real cross-platform product, producing end-to-end task→implementation→verification→review→repair→integration provenance. Historical examples show independent review identifying and driving fixes for real state-machine, lifecycle, and test-validity problems even after the author's test suite was fully green.

That is already a strong and auditable story.

---

**Report generated: 2026-09-18**
