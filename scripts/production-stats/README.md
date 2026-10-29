# Production Stats Scripts

These scripts produce the numbers in `docs/production-stats*.md` and Appendix D of `docs/KANDER_PRODUCTION_RETROSPECTIVE_FULL_EN.md`. They only read board files and agent session logs; they never write to a board.

## Requirements

- Python 3.9 or later, standard library only.
- `rg` (ripgrep) and `zstd` for `sessions.py`.
- Run them on the machine that holds the boards and the agents' session logs (`~/.claude`, `~/.codex`, `~/.grok`, `~/.cursor`, `~/.pi`, `~/.local/share/devin`).

## Usage

Write the outputs outside the repository.

```sh
OUT=/tmp/kander-stats
mkdir -p "$OUT"
W=~/works

# 1. Board facts. Each NAME=PATH is a project root with kanban/done/.
python3 -I scripts/production-stats/collect.py --out "$OUT/board.json" --cutoff 20261029 \
  QuickTUI="$W/quicktui-mono" Backend="$W/zos-go" Kander="$W/kander" \
  QuickCompress="$W/quick-compress" Ullage="$W/ullage-cli"

# 2. Executor models from session logs. Exclude the session that runs this,
#    because its log mentions card IDs without executing them.
python3 -I scripts/production-stats/sessions.py --board "$OUT/board.json" --out "$OUT/models.json" \
  --exclude-session <current-session-id>

# 3. Numbers and Markdown tables.
python3 -I scripts/production-stats/report.py --board "$OUT/board.json" --models "$OUT/models.json"
```

`--cutoff` keeps cards whose ID date is on or before the given day. A card created before the cutoff and finished after it is included once it is in `done/`, so rerunning an old cutoff can give slightly higher counts than the edition that used it.

## What Each Script Does

- `collect.py` reads every card under `kanban/done/`: header fields, `reviews/*/sidecar.json` (one review run each, deduplicated by run ID), the `kander-findings` block of each run's `report.md`, `assignment.json`, the latest disposition record of each finding, and `reviews/batches/*/disposition.json` (deduplicated by batch ID).
- `sessions.py` finds each card's executor model from the card's `SESSION` field, the Codex rollout whose first user message names the card, a `kander pick` of the card in an interactive Claude Code or Codex session, or the Claude Code sub-agent that executed the card. The session's most used model wins.
- `report.py` prints the project, agent, executor send-back, and reviewer tables. Findings count only for review runs that finished normally (`execution_status` is `ok`).

The definitions of every column are in the "Definitions" section of `docs/production-stats.md`. The model tables list executor models with at least 10 reviewed cards and reviewer models with at least 20 runs in a role; the "all" rows include everything.

Lines of code in `docs/production-stats*.md` are not produced here; they were counted separately with `cloc`.
