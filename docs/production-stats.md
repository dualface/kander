# Kander Production Stats

[简体中文](production-stats-cn.md) | [日本語](production-stats-ja.md)

As of 2026-10-07. Read-only counts from `kanban/done/` in five projects that use Kander.

## In One Line

One developer, 6 agents, 68 days, 5 projects, **1,726 task cards**. 975 of them have structured review records: each review is a separate review run, and in about 80% of reviews the reviewer is a different agent from the executor. Older cards used a different review record format and are not in the review counts.

## By Project

| Project | What it is | Period | Cards done | Review batches | Batches with must-fix issues | Review runs | Must-fix issues |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |
| [QuickTUI](https://quicktui.ai/) | Terminal for coding agents on your phone (iOS / Android / server) | 08-01 to 10-07, 68 days | 1,317 | 567 | 259 (46%) | 1,006 | 889 |
| Backend service | Server-side project (closed source) | 08-11 to 09-29, 50 days | 180 | 97 | 36 (37%) | 206 | 63 |
| [Kander](https://github.com/dualface/kander) | This project, built with itself | 09-04 to 10-05, 32 days | 155 | 103 | 69 (67%) | 312 | 280 |
| Quick Compress | iOS app (closed source) | 10-02 to 10-06, 5 days | 51 | 50 | 29 (58%) | 93 | 64 |
| [Ullage](https://github.com/dualface/ullage-cli) | Subscription usage CLI | 09-16 to 10-03, 18 days | 23 | 23 | 18 (78%) | 77 | 75 |
| **Total** | | **68 days** | **1,726** | **840** | **411 (49%)** | **1,694** | **1,371** |

- About 25 cards per day. Across the 1,718 cards with start and finish times, the median card took 60 minutes.
- About half of all review batches found must-fix issues. Each issue is verified first, then fixed, rejected (with evidence), or deferred; all of them must be handled before delivery. 75% of the disposition records are fixes.

## About the Projects

Lines of code are counted with `cloc` over git-tracked files: programming languages only, tests included, protobuf and other generated code excluded, JSON / Markdown / YAML and other data and docs excluded. The creation date is the repository's first commit.

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
| Codex | 561 | 358 |
| Claude Code | 466 | 166 |
| Grok | 340 | 508 |
| Cursor | 203 | 251 |
| Pi | 113 | 228 |
| Devin | 42 | 183 |

Every agent both writes code and reviews code written by others. OpenCode also executed 1 card, not listed above.

## Definitions

- **Cards done**: cards under `kanban/done/`, both directory cards and single-file cards; `archived/` and `trash/` are excluded.
- **Project scope**: 8 other projects with 1 to 3 cards each (12 cards in total) are not counted.
- **Period**: first and last date from the card ID date prefix, both days included.
- **Review batches**: `reviews/batches/<batch>/disposition.json`, deduplicated by batch ID. A batch that covers several cards in a task group is stored under each card and counted once.
- **Review runs**: one review by one role (such as PMQA or Security), deduplicated by run ID.
- **Batches with must-fix issues**: any review run in the batch reported a must-fix issue.
- **Must-fix issues**: Kander sorts review findings into six tiers. The top three (blocking / high / medium) must be handled and go in the report's `FINDINGS`; the bottom three (low / recommend / suggest) are suggestions only and are not counted. 88% of the 1,371 issues are medium: defects that fail only under specific conditions, or documentation that disagrees with the code, dead code, and similar.
- **Cards executed**: the card header `OWNER` field (written as `负责人` on older cards). All 1,726 cards have it.
- **Card counts measure throughput, not value or difficulty.** Task sizes differ by project; do not compare projects directly.

For the earlier analysis, see [Kander in Production](KANDER_PRODUCTION_RETROSPECTIVE.md) (data through 2026-09-18).
