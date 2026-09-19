# Kander

**English** | [简体中文](README-CN.md) | [日本語](README-JA.md)

[![Kander - Kanban Orchestration for Multiple AI Agents](docs/star-please.png)](https://github.com/dualface/kander)

Strict rule-driven multi-agent parallel development with built-in independent review and delivery gates to ensure automated delivery quality.

> **Born from real engineering**  
> Since August 2026, Kander has driven nearly 1,000 production tasks in the [QuickTUI](https://quicktui.ai) production environment. Refined through handling actual code conflicts, race conditions, and complex bugs, it establishes truly reliable agent orchestration, independent review, and crash recovery. Even with lower-cost models, it guarantees delivery quality.
>
> In-depth reading: [Kander in Production](docs/KANDER_PRODUCTION_RETROSPECTIVE.md) | [Full Production Retrospective](docs/KANDER_PRODUCTION_RETROSPECTIVE_FULL_EN.md) (Deep Dive)

![Kander workflow](docs/workflow-en.svg)

### Key Features

- **Two-Stage Independent Review Gates**: Physical isolation between execution and review. PMQA catches semantic and state-machine bugs, blocking false-green tests.
- **Conflict-Aware Scheduling**: Static analysis of Git branch overlaps and file modification boundaries ensures controlled concurrency without write conflicts.
- **Terminal and Workspace Isolation**: Isolated parallel agent execution powered by Git Worktrees and tmux/herdr containers.
- **Zero-Token GitHub Integration**: Seamlessly reuses local `gh` credentials to browse issues, import task cards, and sync progress directly from the board.

## 1. Quick Start

Running requires Git, plus at least one of Codex, Claude, Grok, Cursor, or Pi.

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
2. Once the task is confirmed, the agent (with Kander rules loaded) confirms whether to launch it through the kanban flow. Confirm, and the task is created and started automatically.
3. When you have multiple requirements, repeat steps 1-2 for each one, continuously scheduling and launching tasks.
4. Check task status with the command-line interface:

```sh
kander
```

![Terminal kanban](docs/kanban-screenshot-01.png)

> The board contents above come from my real project [https://quicktui.ai](https://quicktui.ai). QuickTUI is a tool for remotely operating the agents on your computer; it supports iOS/Android/macOS/Linux/Windows and is free to use.

### Terminal Kanban Shortcuts

| Key               | Description                                                             |
| ----------------- | ----------------------------------------------------------------------- |
| `Space` / `Enter` | View task details with Vim-style navigation                             |
| `m`               | Task action menu (start, move, archive)                                 |
| `g`               | Open GitHub Issues overlay (browse and one-click import)                |
| `c`               | Open standalone Chat session (quick discussion without creating a card) |
| `y`               | Copy selected task ID to clipboard                                      |
| `/`               | Filter and search task cards                                            |
| `r`               | Refresh board data                                                      |

Further reading: the slides [How to Advance Tasks Efficiently](docs/how-to-advance-tasks-efficiently-en.pdf) (PDF).

## 2. Key Commands

- `kander`: Open the interactive terminal kanban board.
- `kander doctor`: Check and repair environment dependencies, agent availability, launchers, and rules configuration.

## 3. GitHub Integration

Linking a project to a GitHub repository needs the [GitHub CLI](https://cli.github.com/) (`gh`). Kander never asks for, reads, or stores a token; it reuses the credentials `gh` already manages.

On the terminal board, press `g` to open the GitHub issue list in an overlay.

## 4. Frequently Asked Questions (FAQ)

#### Q: How do I hand off a task card to a different Agent?

**A:** Stop the agent currently working on the card, launch a new agent, and prompt it to take over and continue task card `TASK-ID` (replace `TASK-ID` with the actual card ID). In the terminal kanban board, select the card and press `y` to copy its task ID.

#### Q: How do I instruct an Agent to take over an entire task group?

**A:** Copy the task card ID, launch an agent, and instruct it to act as the coordinator to take over and advance the task group containing `TASK-ID`.

#### Q: How do I check the progress and status of unfinished tasks?

**A:** Start an agent (with Kander rules loaded) and directly ask for the current status and progress of any unfinished task cards.

## 5. Advanced Documentation

- [Review Disposition & Completion Gate](docs/review-disposition.md)
- [Card Transactions & Crash Recovery](docs/card-transactions.md)
- [Terminal Backends & Container Definitions](docs/terminal-backend.md)
- [Durable Dispatch Protocol](docs/durable-dispatch.md)
- [GitHub Issue Import & Result Protocol](docs/github-issue-import.md)

## 6. License

This project is under the MIT License; see [LICENSE](LICENSE).

## 7. Changelog

Release notes live in [CHANGELOG.md](CHANGELOG.md).
