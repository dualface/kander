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

## Example Profile: Current Production (October 2026)

This is the configuration the Kander author runs as of 2026-10-07 across QuickTUI, Kander itself, and other projects:

| Kander role | Agent | Model | Effort |
| --- | --- | --- | --- |
| `small` executor | Claude Code | Opus (1M context) | medium |
| `large` executor | Claude Code | Opus (1M context) | medium |
| `PMQA` reviewer, `small` | Devin | SWE-2-max | agent default |
| `PMQA` reviewer, `large` | Cursor | Grok-4.7-xhigh | agent default |
| `Security` reviewer, `small` | Pi | DeepSeek-V4.1-Flash (via OpenCode Go) | max |
| `Security` reviewer, `large` | Devin | SWE-2-max | agent default |

The intended flow is:

```text
small card -> Claude Code (Opus) -> Devin SWE-2-max PMQA  -> Pi DeepSeek-V4.1-Flash Security when triggered -> closure
large card -> Claude Code (Opus) -> Cursor Grok-4.7 PMQA  -> Devin SWE-2-max Security when triggered       -> closure
```

How it applies the principles above:

- **Implementation and judgment are separate.** Every review stage uses a model family other than the executor's: xAI Grok, Cognition SWE, and DeepSeek review Anthropic Opus.
- **One executor, two review weights.** A single strong executor handles both scales; the scale decides how heavy the review is. `large` cards get a Grok-4.7-xhigh PMQA pass and a SWE-2-max Security pass.
- **Security is a separate choice.** On `large` cards the PMQA and Security reviewers come from different families, so a card that triggers both gets two independent judgments.

The configuration that produces this profile:

```json
{
  "kanban_agents": { "large": "claude", "small": "claude" },
  "reviewers": {
    "large": { "PMQA": "cursor", "Security": "devin" },
    "small": { "PMQA": "devin", "Security": "pi" }
  },
  "models": {
    "kanban": {
      "claude": { "large_model": "opus[1m]", "large_effort": "medium",
                  "small_model": "opus[1m]", "small_effort": "medium" }
    },
    "review_roles": {
      "PMQA": {
        "large_agent": "cursor", "large_model": "grok-4.7-xhigh",
        "small_agent": "devin", "small_model": "swe-2-max"
      },
      "Security": {
        "large_agent": "devin", "large_model": "swe-2-max",
        "small_agent": "pi", "small_model": "opencode-go/deepseek-v4.1-flash", "small_effort": "max"
      }
    }
  }
}
```

This is an excerpt of `kander config --json`; only the keys relevant to routing are shown.

### What Actually Ran in October

Profiles change as models and harnesses change. Review runs recorded from 2026-10-01 to 2026-10-07 across five projects (359 runs):

| Role | Reviewer | Model | Runs |
| --- | --- | --- | ---: |
| PMQA | Grok CLI | grok-4.7 | 106 |
| PMQA | Grok CLI | grok-4.7-build-fast | 89 |
| PMQA | Devin | swe-2-max | 35 |
| PMQA | Cursor | grok-4.7-xhigh | 28 |
| PMQA | Cursor | gemini-3.8-flash-high | 10 |
| PMQA | Grok CLI / other | other Grok variants | 5 |
| Security | Devin | swe-2-max | 85 |
| Security | Pi | opencode-go/deepseek-v4.1-flash | 1 |

Claude Code executed 229 of the 249 October cards. Through 2026-10-05, PMQA ran mostly on the Grok CLI. From 2026-10-06, Cursor took over most PMQA runs, matching the size-split routing above.

The September 2026 example on this page (SWE-2 for `large`, DeepSeek-V4.1-Flash for `small`, Grok-4.6 for PMQA) has been replaced by this profile. See [Production Stats](production-stats.md) for per-agent totals.

## Card Review and Planning

When an independent CARD_REVIEW is required before execution, use a model different from the planned large-card executor when possible. In the profile above, where Claude Code executes both scales, a Grok or SWE-2 reviewer keeps the card review independent of the executor.

CARD_REVIEW should focus on requirement completeness, acceptance-criteria quality, scope contradictions, dependencies, and whether the proposed card split can be verified independently. It should not pre-justify the implementation approach for the later PMQA reviewer.

## Disagreements Between Author and Reviewer

Multi-model routing is most useful when disagreement is treated as information rather than noise.

When an author rejects a must-fix reviewer finding, require a factual basis that falsifies a material premise. If neither side can establish the decisive runtime fact, prefer `unverifiable` over an unsupported rejection.

Kander has no arbitration stage today: an `unverifiable` must-fix item goes to the user. One possible future direction, not a shipped feature, is to route only the disputed finding to a third model instead of running another full review. Such a model would come from a family different from both the author and the PMQA reviewer, and would receive the task contract, relevant code/diff, reviewer finding and evidence, author rejection basis, and objective verification evidence, never hidden chain-of-thought from either model.

## Per-Card Overrides

Routing by scale fits most cards. When one card needs a specific harness or model regardless of the configuration, pin it in the card header: `EXEC_AGENT`/`EXEC_MODEL`/`EXEC_EFFORT` for the executor and `REVIEW_PMQA_*`/`REVIEW_SECURITY_*` for reviewers (see `rules/KANDER-KANBAN-RULES.md` "Card Pins"). A pinned card records how each stage was actually chosen in `EXEC_RESOLVED` and `REVIEW_<ROLE>_RESOLVED`, marking every value `forced`, `cli`, or `config:<scale>`, so a later reader can tell a deliberate override from the routing default. Pins freeze with the card contract; use them for user decisions about one card, not as a substitute for a routing profile.

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
