# Multi-Model Routing for Coding Workflows

Kander can use different execution agents for `small` and `large` cards and different reviewers for `PMQA` and `Security`. This makes it possible to optimize for complementary failure modes instead of choosing one model for the whole workflow.

This page describes routing principles and one concrete example profile. Model capabilities and provider names change quickly, so the example is guidance rather than a Kander default.

## Routing Principles

### Separate implementation from judgment

Prefer a PMQA reviewer from a different model family than the implementation model. The goal is not only raw benchmark strength: independent training, post-training, tool use, and reasoning behavior can produce different blind spots.

Avoid using the author as the only final correctness judge. Kander still requires the executing agent to verify findings, but the reviewer should form its first-round judgment independently from the author's rationale.

### Spend the strongest implementation model on `large`

Kander's `SIZE` already encodes risk rather than line count. `large` cards include changes such as cross-module contracts, state transitions, concurrency/retry behavior, security boundaries, and substantial design uncertainty. Route the model with the strongest repo-level implementation and verification behavior to `large` work.

Use a faster model for `small` only when the card is genuinely contained. PMQA remains the quality gate.

### Use Security as a specialist pass

Security review has a different objective from PMQA: trace untrusted input across trust boundaries and establish realistic exploit chains. A model with strong security-oriented coding behavior can be useful here even when another model is preferred for general implementation or PMQA.

### Preserve native harness strengths

A model's coding performance depends on its agent harness, tools, context management, and verification loop. Prefer the CLI or harness in which that model was designed and evaluated when practical. Kander custom agents exist so execution does not have to be normalized into one universal CLI.

### Optimize for the workflow, not a leaderboard

The relevant outcome is the probability that the final merged change is correct. A model that is slightly weaker as a standalone implementer can still be valuable as a reviewer when it catches different errors. Conversely, a very fast implementer is not automatically a good final reviewer.

## Example Profile: quality-max

As of September 2026, one quality-first profile worth evaluating is:

| Kander role | Example model | Rationale |
| --- | --- | --- |
| `small` executor | DeepSeek-V4.1-Flash | Strong coding capability with good throughput for contained tasks; keep PMQA independent. |
| `large` executor | SWE-2 | Allocate the strongest repo-level implementation behavior to high-risk cards. |
| `PMQA` reviewer | Grok-4.6 | Cross-family review for both DeepSeek and SWE-2 implementations; use the highest practical reasoning setting. |
| `Security` reviewer | DeepSeek-V4.1-Flash | Specialist second pass on security-triggered work; for `large` cards the author is normally SWE-2, preserving model independence. |
| fast-lane executor | Cursor Composer 2.5 | Optional throughput-oriented executor for low-risk work, not the default final correctness judge. |

The intended flow is:

```text
small card -> DeepSeek-V4.1-Flash --\
                                      -> Grok-4.6 PMQA -> Security when triggered -> closure
large card -> SWE-2 ----------------/
```

For security-sensitive work, Kander's scale rules normally make the card `large`, giving a useful separation:

```text
SWE-2 implementation -> Grok-4.6 PMQA -> DeepSeek-V4.1-Flash Security
```

This yields three independently trained model families across implementation, general review, and security review.

## Example Profile: quality-fast

For higher throughput while retaining independent review:

| Kander role | Example model |
| --- | --- |
| `small` executor | Cursor Composer 2.5 |
| `large` executor | SWE-2 |
| `PMQA` reviewer | Grok-4.6 |
| `Security` reviewer | DeepSeek-V4.1-Flash |

This profile spends the faster executor on contained cards but keeps the quality gates unchanged.

## Card Review and Planning

When an independent CARD_REVIEW is required before execution, use a model different from the planned large-card executor when possible. For the example profile above, DeepSeek-V4.1-Flash is a useful planning/card-review counterpart to SWE-2.

CARD_REVIEW should focus on requirement completeness, acceptance-criteria quality, scope contradictions, dependencies, and whether the proposed card split can be verified independently. It should not pre-justify the implementation approach for the later PMQA reviewer.

## Disagreements Between Author and Reviewer

Multi-model routing is most useful when disagreement is treated as information rather than noise.

When an author rejects a must-fix reviewer finding, require a factual basis that falsifies a material premise. If neither side can establish the decisive runtime fact, prefer `unverifiable` over an unsupported rejection.

A future arbitration layer can route only disputed findings to a third model instead of running another full review. That keeps the expensive third opinion narrow and preserves independent reasoning.

A sensible dynamic arbitration policy is:

| Author | PMQA | Third opinion |
| --- | --- | --- |
| SWE-2 | Grok-4.6 | DeepSeek-V4.1-Flash |
| DeepSeek-V4.1-Flash | Grok-4.6 | SWE-2 |
| Composer 2.5 | Grok-4.6 | SWE-2 |

The third model should receive the task contract, relevant code/diff, reviewer finding and evidence, author rejection basis, and objective verification evidence. It should not receive hidden chain-of-thought from either model.

## What Not to Hard-Code

Do not make the example model names permanent protocol rules. Provider models, effort levels, and harness behavior change faster than Kander's review semantics.

The stable Kander concepts are:

- route by task risk (`small` / `large`);
- keep implementation and PMQA independent when practical;
- select Security separately;
- bind model/effort to the selected agent and scale;
- preserve durable evidence for every finding and disposition;
- use objective verification to resolve model disagreement.

Configure concrete agents and model names through Kander's existing `kanban_agents`, `reviewers`, `models.kanban`, `models.review`, and `models.review_roles` settings. See [Custom Execution Agents](custom-agents.md) for the configuration and review-template contract.
