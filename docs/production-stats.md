# Kander Production Stats

[简体中文](production-stats-cn.md) | [日本語](production-stats-ja.md)

As of 2026-10-07. Read-only counts from `kanban/done/` in four projects that use Kander.

## In One Line

One developer, 6 agents, 68 days, 4 projects, **1,544 task cards**. 855 of them have structured review records from an independent review by a different agent; older cards used a different review record format and are not in the review counts.

## By Project

| Project | What it is | Period | Cards done | Review batches | Batches blocked | Review runs | Blocking findings |
| --- | --- | --- | ---: | ---: | ---: | ---: | ---: |
| [QuickTUI](https://quicktui.ai/) | Cross-platform product (iOS / Android / Qt desktop / backend) | 08-01 to 10-07, 68 days | 1,315 | 565 | 257 (45%) | 1,002 | 887 |
| [Kander](https://github.com/dualface/kander) | This project, built with itself | 09-04 to 10-05, 32 days | 155 | 103 | 69 (67%) | 312 | 280 |
| Quick Compress | iOS app (closed source) | 10-02 to 10-06, 5 days | 51 | 50 | 29 (58%) | 93 | 64 |
| [Ullage](https://github.com/dualface/ullage-cli) | Subscription usage CLI | 09-16 to 10-03, 18 days | 23 | 23 | 18 (78%) | 77 | 75 |
| **Total** | | **68 days** | **1,544** | **741** | **373 (50%)** | **1,484** | **1,306** |

- About 23 cards per day. Across the 878 cards with start and finish times, the median card took 53 minutes.
- Half of all review batches were blocked: independent review found blocking issues, and each one had to be dispositioned (fixed, rejected, or deferred) before delivery.

## By Agent

| Agent | Cards executed | Review runs |
| --- | ---: | ---: |
| Claude Code | 424 | 164 |
| Codex | 193 | 357 |
| Pi | 98 | 158 |
| Cursor | 72 | 223 |
| Grok | 48 | 439 |
| Devin | 42 | 143 |

Every agent both writes code and reviews code written by others. OpenCode also executed 1 card, not listed above.

## Definitions

- **Cards done**: card directories under `kanban/done/`; `archived/` and `trash/` are excluded.
- **Period**: first and last date from the card ID date prefix, both days included.
- **Review batches**: `reviews/batches/<batch>/disposition.json`, deduplicated by batch ID. A batch that covers several cards in a task group is stored under each card and counted once.
- **Review runs**: one review by one role (such as PMQA or Security), deduplicated by run ID.
- **Batches blocked**: any review run in the batch reported a blocking finding.
- **Blocking findings**: entries in the review report's `FINDINGS` (blocking / high / medium tiers); non-blocking suggestions are excluded.
- **Cards executed**: the card header `OWNER` field. Only the newer card format (since 2026-09) has it: 878 cards.
- **Card counts measure throughput, not value or difficulty.** Task sizes differ by project; do not compare projects directly.

For the earlier analysis, see [Kander in Production](KANDER_PRODUCTION_RETROSPECTIVE.md) (data through 2026-09-18).
