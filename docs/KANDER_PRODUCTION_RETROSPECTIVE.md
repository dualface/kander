# Kander in Production

Kander is not a multi-agent demo. It is a workflow system that has been shaped by real software development.

Since August 2026, Kander has been used continuously on **QuickTUI**, a real cross-platform product covering iOS, Android, Qt/QML, backend services, networking, authentication, code generation, testing, and integration work. In less than two months, the workflow has handled **nearly 1,000 real engineering tasks**.

That matters because many of Kander's core ideas did not come from a whiteboard. They came from actual failures, review findings, race conditions, broken assumptions, integration conflicts, and recovery cases encountered during day-to-day development.

## More than “run several agents at once”

Kander is built around a simple observation:

> Once coding agents become capable enough to implement real features, the hard problem shifts from generating code to coordinating, verifying, and controlling the work.

A typical Kander task is closer to this:

```text
Task
↓
Plan
↓
Conflict Check
↓
Agent Execution
↓
Tests / Verification
↓
Independent Review
↓
Finding Verification
↓
Fix
↓
Incremental Re-review
↓
Integration
↓
Done
```

The goal is not to maximize the number of concurrent agents. The goal is to maximize **safe, useful parallelism** while keeping every task traceable and recoverable.

## Tests passing is not the same as engineering quality

One production task provides a good example.

An Android networking and connection-liveness change reached a point where:

- **2,889 tests passed**
- lint passed
- build checks passed

In a conventional workflow, that would often be enough to consider the work complete.

Kander's independent review still found several real problems, including:

- stale callbacks from an older connection generation affecting a newer connection;
- heartbeat suspend/resume state errors;
- late callbacks restarting already-stopped liveness logic;
- authentication lease lifecycle issues;
- tests that were green but did not actually verify the required behavior.

Those findings were verified, converted into concrete repair work, and reviewed again. The task eventually reached **2,935 passing tests** and a final PM/QA pass.

This is one of the clearest lessons from the production history:

> **A green test suite is necessary, but it is not always sufficient.**

## Review is part of the workflow, not a final comment

Kander does not treat review as “ask another model whether the code looks good.”

A review can be tied to:

- the task;
- reviewer role;
- reviewer/model/effort;
- base commit;
- target commit;
- review batch and run;
- findings and follow-up fixes.

Reviewer findings are not automatically treated as truth. They are independently verified before becoming repair work.

That creates a workflow closer to:

```text
Reviewer raises a finding
        ↓
Independent verification
        ↓
Confirmed / Rejected
        ↓
Fix
        ↓
Incremental re-review
```

This matters because reviewers can also be wrong. Kander treats review as an auditable engineering process rather than an oracle call.

## Controlled parallelism instead of agent count

Real projects contain shared contracts, generated files, project configuration, authentication state, protocol definitions, and other hotspots.

Running more agents against the same hotspots can easily create:

- merge conflicts;
- semantic conflicts;
- duplicated work;
- architecture drift;
- contract mismatches.

Kander therefore pays attention to task dependencies and overlapping change surfaces before dispatching work.

Some tasks should run in parallel.

Some should not.

The objective is not “more agents.” It is **better orchestration**.

## Failures are expected, so execution is recoverable

Long-running agent work can fail in many ordinary ways:

```text
Terminal crash
Agent crash
Network disconnect
Kander restart
Prompt sent but state not yet persisted
Agent finished but the controller lost the session
```

Kander tracks durable dispatch information so work can be reconciled instead of silently becoming ambiguous.

This is an important distinction between a demo and a production workflow: failure and recovery are treated as normal operating conditions.

## Different models can play different roles

Kander is not tied to one model or one coding agent.

Production tasks have already used different agents and model families for implementation and review. That makes workflows such as the following possible:

```text
Implementation model
        +
PM/QA reviewer
        +
Security reviewer
```

The goal is not to find one model that is best at everything. It is to let different models contribute where they are strongest.

## The workflow itself has been trained by production

Across nearly 1,000 real tasks, Kander has accumulated experience about:

- which tasks are safe to parallelize;
- where hidden lifecycle and concurrency bugs appear;
- which tests can become false-green;
- why review findings must be independently verified;
- when incremental review is better than repeating a full review;
- why exact commit provenance matters;
- how failed dispatches and sessions should be recovered.

This history is one of the most important parts of the project.

Kander is not trying to replace your editor, terminal, Git client, or coding agent.

It sits one level above them:

> **Kander is a workflow runtime for coding agents.**

One developer. Multiple coding agents. One controlled engineering workflow.

---

For the detailed evidence, task examples, limitations, architecture observations, and methodology, see **KANDER_PRODUCTION_RETROSPECTIVE_FULL.md**.
