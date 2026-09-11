# Kander

**English** | [简体中文](README-CN.md) | [日本語](README-JA.md)

[![Kander - Kanban Orchestration for Multiple AI Agents](docs/star-please.png)](https://github.com/dualface/kander)

One person schedules multiple AI agents with a kanban board.

![Kander workflow](docs/workflow-en.svg)

## 1. Quick Start

Running requires Git, plus at least one of Codex, Claude, Grok, or Cursor.

**macOS** — install with Homebrew:

```sh
brew install dualface/tap/kander
kander
```

**Linux** — download the binary directly:

```sh
ARCH=$(uname -m); [ "$ARCH" = x86_64 ] && ARCH=amd64; [ "$ARCH" = aarch64 ] && ARCH=arm64
curl -fsSL "https://github.com/dualface/kander/releases/latest/download/kander-linux-${ARCH}.tar.gz" | tar xz
./kander
```

**Windows** — download `kander-windows-amd64.zip` from [Releases](https://github.com/dualface/kander/releases), unzip it, and run `kander.exe`.

On first launch, if not yet installed, an interactive wizard starts. Once installation finishes, it is ready to use.

Four steps to get going:

1. Start an agent session and discuss the requirement or task there, making the goal and acceptance criteria clear. The agent's Plan mode is recommended.
2. Once the task is confirmed, the agent asks whether to launch it through the kanban flow. Confirm, and the task launches automatically.
3. When you have multiple requirements, repeat steps 1-2 for each one, continuously scheduling and launching tasks.
4. Check task status with the command-line interface:

```sh
kander
```

![Terminal kanban](docs/kanban-screenshot-01.png)

Choose **Tide**, **Dusk**, **Slate Dark**, or **Slate Light** in `o` → Interface → Theme. The Slate pair matches a gray-blue terminal host such as herdr; existing themes and the default remain available.

> The board contents above come from my real project [https://quicktui.ai](https://quicktui.ai). QuickTUI is a tool for remotely operating the agents on your computer; it supports iOS/Android/macOS/Linux/Windows and is free to use.

Further reading: the slides [How to Advance Tasks Efficiently](docs/how-to-advance-tasks-efficiently-en.pdf) (PDF).

## 2. GitHub Integration

Linking a project to a GitHub repository needs the [GitHub CLI](https://cli.github.com/) (`gh`). Kander never asks for, reads, or stores a token; it reuses the credentials `gh` already manages.

```sh
kander issue repo                                   # resolve the repository of the current worktree
kander issue repo --repo HOST/OWNER/REPO --json     # or pass a reference explicitly
kander issue list --state open --label bug          # list issues: --state open|closed|all, repeated --label, --search, --limit, --json
kander issue show 42 --comments                     # one issue with its comments
kander issue import 42 --comments                   # import it as a backlog card, with the issue text and comments
```

`kander issue repo` confirms the canonical identity with GitHub instead of trusting a directory name. When a worktree has several distinct remotes it refuses to guess, and asks for `--repo` or for `gh repo set-default`. `kander doctor` reports the `gh` path, version, and per-host authentication state without touching credentials, remotes, or accounts.

`kander issue list` filters by state, labels, and a search term and bounds how many issues it fetches; pull requests are never mixed into the result. `kander issue show NUMBER` renders one issue, and `--comments` loads its comments under an explicit bound instead of truncating silently. Both commands support `--json` for scripting and report actionable errors (missing `gh`, unauthenticated host, rate limit, ambiguous remotes) instead of a raw failure.

`kander issue import NUMBER` creates a normal backlog card bound to the issue: `--comments` stores the discussion, `--type` overrides the label-derived type, `--large` sets the size, `--language` freezes the card language, and `--json` prints the result for scripting. The issue title, body, and comments are stored in `source/github-issue.json` and `source/github-issue.md` beside `spec.md`; the sanitized single-line title also becomes the card heading, while the card contract is authored from the confirmed repository identity. Importing the same issue twice returns the existing card, and the unique source key is checked in the same board transaction that publishes the card, so concurrent imports cannot produce duplicates. Over-limit issues are rejected with a hint instead of being truncated. See [docs/github-issue-import.md](docs/github-issue-import.md) for the snapshot format and the security model.

On the terminal board, `g` opens the same data as an overlay: selecting a row loads the issue with its comments on its own (a local snapshot cache paints first, the refresh runs in the background, and a changed refresh is reported without resetting an unchanged one), `Enter` opens the detail page, `Tab` cycles the state, `/` searches, `l` filters by label, `i` imports the issue as a backlog card (or jumps to the card already bound to it, while `I` imports with comments), `s` confirms one takeover session for the selected issue: an unbound issue starts a new agent that reads the refreshed local evidence, investigates and agrees on the plan before importing the card, a card already bound to the issue is focused on the board, and a backlog card additionally offers starting the same session to complete its contract. The TUI starts only background launchers (`herdr`, `tmux`, `tmux-session`) and points at the CLI otherwise; it never writes `CARD_REVIEW:` and never creates or moves a card on this path. `r` refreshes, `o` opens it in the browser, and `Esc`/`q` closes the overlay without changing the board. Imported issues show their bound task ID and state, and a newer remote revision is marked as an update instead of overwriting the card. Every request runs in the background, so the board never blocks.

## 3. License

This project is under the MIT License; see [LICENSE](LICENSE).
