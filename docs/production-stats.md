# Kander Production Stats

[简体中文](production-stats-cn.md) | [日本語](production-stats-ja.md)

As of 2026-10-29. Read-only counts from `kanban/done/` in five projects that use Kander, plus the agents' own session logs for the executor model.

## In One Line

One developer, 6 agents, 90 days, 5 projects, **2,477 task cards**. 1,727 of them have structured review records: each review is a separate review run, and in 89% of reviews the reviewer is a different agent from the executor. Older cards used a different review record format and are not in the review counts.

## By Project

| Project | What it is | Period | Cards done | Review batches | Batches with must-fix issues | Review runs | Must-fix issues |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |
| [QuickTUI](https://quicktui.ai/) | Terminal for coding agents on your phone (iOS / Android / server) | 08-01 to 10-29, 90 days | 1,980 | 1,149 | 587 (51%) | 2,039 | 1,605 |
| Backend service | Server-side project (closed source) | 08-11 to 10-28, 79 days | 236 | 152 | 54 (36%) | 300 | 112 |
| [Kander](https://github.com/dualface/kander) | This project, built with itself | 09-04 to 10-29, 56 days | 180 | 125 | 83 (66%) | 347 | 319 |
| Quick Compress | iOS app (closed source) | 10-02 to 10-27, 26 days | 56 | 53 | 29 (55%) | 98 | 69 |
| [Ullage](https://github.com/dualface/ullage-cli) | Subscription usage CLI | 09-16 to 10-19, 34 days | 25 | 25 | 20 (80%) | 81 | 77 |
| **Total** | | **90 days** | **2,477** | **1,504** | **773 (51%)** | **2,865** | **2,182** |

- About 28 cards per day. Across the 2,469 cards with start and finish times, the median card took 57 minutes.
- About half of all review batches found must-fix issues. Each issue is verified first, then fixed, rejected (with evidence), or deferred; all of them must be handled before delivery. 78% of the final dispositions are fixes.

## About the Projects

Lines of code were counted on 2026-10-07 with `cloc` over git-tracked files: programming languages only, tests included, protobuf and other generated code excluded, JSON / Markdown / YAML and other data and docs excluded. They were not recounted for this edition. The creation date is the repository's first commit.

- **QuickTUI**: a terminal product for operating coding agents on your computer from your phone. Repository created 2026-04-14; driven with Kander since August.
  Architecture: a self-hosted Go server (Linux / macOS / Windows), an iOS client (Swift), an Android client (Kotlin), a WebView component shared by the clients (TypeScript), cloud relay and account services (Go), and a Rust installer.
  About 1.1 million lines: Go 500k, Swift 240k, Kotlin 200k; about 400k of them are tests.
- **Backend service**: a closed-source production service. Repository created 2026-06-22.
  Architecture: a Go server, a TypeScript admin console, PostgreSQL and Redis.
  About 210k lines: Go 150k, TypeScript 48k; about 90k of them are tests.
- **Kander**: this project. Repository created 2026-09-04.
  Architecture: a single Go binary with a CLI and a TUI board (Bubble Tea), for macOS / Linux / Windows on amd64 and arm64.
  About 137k lines of Go; about 70k of them are tests.
- **Quick Compress**: a closed-source iOS media compression app. Repository created 2026-10-02; the first TestFlight build shipped on day 4 (10-05).
  Architecture: a native iOS 26 app (Swift), split into the QCCore / QCMedia / QCUI packages.
  About 22k lines; about 6.5k of them are tests.
- **Ullage**: a daemon and CLI that shows usage across AI subscriptions. Repository created 2026-08-27; driven with Kander since mid-September.
  Architecture: a Rust workspace (7 crates); one binary provides the daemon, CLI, TUI, and an optional HTTP API, for macOS / Linux / Windows.
  About 60k lines of Rust (unit tests live inside source files and are not counted separately).

## By Agent

| Agent | Cards executed | Review runs |
| --- | ---: | ---: |
| Claude Code | 1,113 | 166 |
| Codex | 646 | 781 |
| Grok | 340 | 906 |
| Cursor | 219 | 390 |
| Pi | 116 | 419 |
| Devin | 42 | 203 |

Every agent both writes code and reviews code written by others. OpenCode also executed 1 card, not listed above.

## By Model: Executor Send-Backs

A send-back is a review round that found at least one must-fix issue in the card's own delivery. The card goes back to its executor, which verifies and fixes it, and the next round reviews only the fix.

| Executor | Model | Reviewed cards | Large cards | Review rounds | Send-backs | Send-backs per card | Cards sent back at least once | Must-fix issues per card |
| --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| Claude Code | `claude-opus-5-5` | 765 | 88% | 1,386 | 595 | 0.78 | 463 (61%) | 1.29 |
| Claude Code | not resolved | 193 | 75% | 337 | 138 | 0.72 | 101 (52%) | 1.33 |
| Codex | not resolved | 113 | 95% | 189 | 48 | 0.42 | 36 (32%) | 0.64 |
| Codex | `gpt-6-astra` | 71 | 62% | 158 | 65 | 0.92 | 41 (58%) | 3.52 |
| Pi | `swe-2` | 48 | 73% | 99 | 49 | 1.02 | 31 (65%) | 1.79 |
| Pi | `deepseek-v4-flash` | 39 | 51% | 90 | 46 | 1.18 | 29 (74%) | 3.13 |
| Codex | `gpt-5.6-sol` | 32 | 94% | 63 | 27 | 0.84 | 23 (72%) | 1.78 |
| Devin | not resolved | 31 | 68% | 70 | 35 | 1.13 | 18 (58%) | 2.16 |
| Grok | `grok-4.7-build` | 27 | 67% | 40 | 11 | 0.41 | 9 (33%) | 0.48 |
| Codex | `gpt-6-sol` | 22 | 50% | 31 | 8 | 0.36 | 7 (32%) | 0.36 |
| Grok | `grok-4.6-build` | 20 | 65% | 44 | 22 | 1.10 | 12 (60%) | 3.00 |
| Cursor | `default` | 17 | 24% | 27 | 14 | 0.82 | 8 (47%) | 2.94 |
| Cursor | `grok-4.6` | 16 | 69% | 39 | 23 | 1.44 | 13 (81%) | 5.75 |
| Cursor | `grok-4.7` | 16 | 44% | 21 | 7 | 0.44 | 7 (44%) | 0.44 |
| Grok | not resolved | 13 | 23% | 23 | 13 | 1.00 | 8 (62%) | 1.54 |
| Claude Code | `claude-fable-5-1` | 12 | 83% | 22 | 8 | 0.67 | 6 (50%) | 1.17 |
| **All** (including 36 cards in smaller rows) | | **1,471** | **80%** | **2,704** | **1,141** | **0.78** | **835 (57%)** | **1.52** |

- On average a reviewed card needs 1.8 review rounds and is sent back 0.78 times. 57% of reviewed cards are sent back at least once.
- Claude Code executes most large cards: 88% of its reviewed `claude-opus-5-5` cards are large. Read each row together with its share of large cards.

## By Model: Reviewer Findings, Rejections, and Deferrals

| Role | Reviewer | Model | Review runs | Runs with must-fix issues | Must-fix issues per run | Dispositions | Rejected | Must-fix rejected | Deferred |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| PMQA | Grok | `grok-4.7-build-fast` | 422 | 189 (45%) | 0.70 | 457 | 26 (5.7%) | 15 / 297 (5.1%) | 57 (12.5%) |
| PMQA | Codex | `gpt-6.1-sol` | 422 | 223 (54%) | 1.09 | 544 | 29 (5.3%) | 22 / 461 (4.8%) | 29 (5.3%) |
| PMQA | Grok | `grok-4.7` | 345 | 159 (47%) | 0.74 | 365 | 15 (4.1%) | 6 / 256 (2.3%) | 46 (12.6%) |
| PMQA | Cursor | `cursor-grok-4.6-xhigh` | 142 | 67 (48%) | 0.86 | 190 | 17 (8.9%) | 6 / 120 (5.0%) | 23 (12.1%) |
| PMQA | Devin | `swe-2-max` | 112 | 23 (27%) | 0.31 | 106 | 19 (17.9%) | 1 / 26 (3.8%) | 43 (40.6%) |
| PMQA | Cursor | `grok-4.7-high` | 94 | 34 (37%) | 0.41 | 49 | 3 (6.1%) | 3 / 39 (7.7%) | 2 (4.1%) |
| PMQA | Cursor | `grok-4.7-xhigh` | 68 | 28 (42%) | 0.79 | 66 | 5 (7.6%) | 2 / 53 (3.8%) | 6 (9.1%) |
| PMQA | Codex | `gpt-5.6-sol` | 58 | 33 (63%) | 1.35 | 74 | 1 (1.4%) | 1 / 70 (1.4%) | 0 (0.0%) |
| PMQA | Codex | `gpt-6-sol` | 56 | 27 (48%) | 0.70 | 39 | 4 (10.3%) | 4 / 39 (10.3%) | 0 (0.0%) |
| PMQA | Grok | `grok-4.6` | 25 | 11 (44%) | 1.08 | 58 | 5 (8.6%) | 0 / 28 (0.0%) | 18 (31.0%) |
| **PMQA, all** | | | **1,821** | **819 (47%)** | **0.82** | **2,060** | **127 (6.2%)** | **61 / 1,450 (4.2%)** | **246 (11.9%)** |
| PM / QA | Codex | `gpt-6-astra` | 231 | 127 (57%) | 1.69 | 394 | 8 (2.0%) | 8 / 393 (2.0%) | 0 (0.0%) |
| PM / QA | Claude Code | `opus` | 157 | 67 (68%) | 1.58 | 304 | 23 (7.6%) | 4 / 160 (2.5%) | 84 (27.6%) |
| PM / QA | Pi | `deepseek-v4-flash` | 110 | 50 (52%) | 1.04 | 214 | 18 (8.4%) | 6 / 101 (5.9%) | 58 (27.1%) |
| PM / QA | Grok | `grok-4.6` | 45 | 16 (46%) | 1.06 | 93 | 0 (0.0%) | 0 / 39 (0.0%) | 40 (43.0%) |
| **PM / QA, all** | | | **558** | **261 (57%)** | **1.47** | **1,008** | **49 (4.9%)** | **18 / 694 (2.6%)** | **184 (18.3%)** |
| Security | Pi | `opencode-go/deepseek-v4.1-flash` | 189 | 31 (18%) | 0.20 | 110 | 9 (8.2%) | 1 / 37 (2.7%) | 35 (31.8%) |
| Security | Devin | `swe-2-max` | 91 | 12 (13%) | 0.19 | 66 | 10 (15.2%) | 2 / 15 (13.3%) | 25 (37.9%) |
| Security | Pi | `devin/swe-2` | 74 | 8 (14%) | 0.21 | 52 | 11 (21.2%) | 2 / 12 (16.7%) | 23 (44.2%) |
| Security | Cursor | `grok-4.7-high` | 37 | 5 (14%) | 0.16 | 6 | 0 (0.0%) | 0 / 6 (0.0%) | 0 (0.0%) |
| Security | Grok | `grok-4.7-build-fast` | 27 | 2 (8%) | 0.08 | 2 | 0 (0.0%) | 0 / 2 (0.0%) | 0 (0.0%) |
| **Security, all** | | | **475** | **70 (16%)** | **0.20** | **257** | **30 (11.7%)** | **5 / 86 (5.8%)** | **83 (32.3%)** |
| **All roles** | | | **2,865** | **1,150 (43%)** | **0.82** | **3,328** | **206 (6.2%)** | **84 / 2,230 (3.8%)** | **515 (15.5%)** |

- The role matters more than the model. PMQA runs find must-fix issues in 47% of runs, Security runs in 16%. The PM / QA rows are the two separate roles that PMQA replaced; each one reviewed half of today's PMQA scope. "All roles" also includes 11 runs of the earlier CSA and Hacker roles.
- Rejections are rare and mostly hit non-blocking items: 6.2% of all dispositions, but only 3.8% of must-fix issues.
- Only non-blocking items can be deferred, and the executor deferred 47% of them (515 of 1,098). A model that reports more non-blocking items shows a higher deferral rate. The Codex models report almost no non-blocking items, so their deferral rate is near zero.

## Definitions

- **Cards done**: cards under `kanban/done/`, both directory cards and single-file cards; `archived/` and `trash/` are excluded.
- **Project scope**: 8 other projects with 1 to 12 cards each (27 cards in total) are not counted.
- **Period**: first and last date from the card ID date prefix, both days included.
- **Structured review records**: the card has `reviews/plan.json`. 1,471 of these cards had at least one review run; the others were exempt from review by their review plan.
- **Review batches**: `reviews/batches/<batch>/disposition.json`, deduplicated by batch ID. A batch that covers several cards in a task group is stored under each card and counted once.
- **Review runs**: one review by one role (such as PMQA or Security), deduplicated by run ID. The run count includes failed and interrupted runs. Findings come only from the 2,647 runs that finished normally; the "Runs with must-fix issues" percentages use those runs as the denominator.
- **Batches with must-fix issues**: any review run in the batch reported a must-fix issue.
- **Must-fix issues**: Kander sorts review findings into six tiers. The top three (blocking / high / medium) must be handled and go in the report's `FINDINGS`; the bottom three (low / recommend / suggest) are suggestions only, go in `NON_BLOCKING`, and are not counted as must-fix. 89% of the 2,182 issues are medium: defects that fail only under specific conditions, or documentation that disagrees with the code, dead code, and similar.
- **Cards executed**: the card header `OWNER` field (written as `负责人` on older cards). All 2,477 cards have it.
- **Executor model**: the model that the executor's own session log used most, found from the card's `SESSION` field, from the `kander pick` of the card in an interactive Claude Code or Codex session, or from the Claude Code sub-agent that executed the card. The model was resolved for 1,106 of the 1,471 reviewed cards (75%); the others are listed as "not resolved". Cursor `default` is Cursor's automatic model choice.
- **Review round**: the normally finished review runs of one batch on one reviewed commit. PMQA and Security runs on the same commit form one round.
- **Send-back**: a review round with at least one must-fix issue assigned to the card. The assignment comes from the run's `assignment.json`; a run without one assigns its issues to every card it reviewed.
- **Reviewer model**: the `model` field of the review run as configured, including aliases such as `opus`.
- **Dispositions, rejected, deferred**: the latest disposition record of each finding, counted against the review run that reported it. "Rejected" and "Deferred" use all dispositions as the denominator; "Must-fix rejected" uses only blocking / high / medium findings. A rejection needs a factual basis or the user's quoted decision, and also covers findings superseded by a user decision, so the rejection rate is an upper bound for reviewer false positives.
- **Smaller rows**: the model tables list executor models with at least 10 reviewed cards and reviewer models with at least 20 runs in a role. The "all" rows include every card and run.
- **Reproducing**: every number except lines of code comes from the scripts in [`scripts/production-stats/`](../scripts/production-stats/README.md).
- **Card counts measure throughput, not value or difficulty.** Task sizes differ by project; do not compare projects directly.
- **The model tables are not a leaderboard.** Card size, project, role, and period differ between rows, and the model routing changed during the 90 days.

For the earlier analysis, see [Kander in Production](KANDER_PRODUCTION_RETROSPECTIVE.md) (data through 2026-09-18).
