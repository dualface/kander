# Repository Guidelines

This file is the development contract for the Kander repository itself. The workflow rules the repository ships live in `rules/`; those files are deliverables, not a description of this repository's package boundaries.

## Repository-Specific Exceptions

- In this repository the second-stage security roles `CSA` and `Hacker` are always marked N/A and never run; `PM` and `QA` remain applicable.
- When every change since the review base is Markdown rules or documentation, skip the review. As soon as any script, code, or other non-Markdown file is included, run `PM` and `QA` per the applicable rules; `CSA` and `Hacker` stay N/A per the previous bullet.

## Language Conventions

- Commit messages are English only: title, body, and trailers all in English, with no Chinese left. History is already unified to English; later commits must not regress.
- Code comments are English only: line, block, and doc comments in `.go`, plus comments in `.sh` / `.ps1` and other scripts.
- Task card titles are English only: every card created for this repository (`kander new`, cards under `kanban/`, and cards created by hand) gets an English title, no matter what the card's `LANGUAGE` field or the configured `agent_language` says. That field still governs the card body, execution records, reports, and conversation with the user; it does not exempt the title.
- The released rules `rules/*.md` are English only and kept as the single copy; no per-language translations are maintained. The language the agent uses with the user is decided by the `agent_language` setting, and the "Language" section of the rules entry `KANDER-AGENTS.md` requires agents to honor it; keep that section in place when changing the rules.
- Repository documentation (`AGENTS.md`, `docs/`) is written in English. The README defaults to the English `README.md`, with the Chinese and Japanese translations in `README-CN.md` and `README-JA.md`; keep the three versions in sync. User-facing strings still go through the `internal/i18n` message catalog and are not rewritten because of this bullet.

## Go Module and Package Map

- Module path: `github.com/dualface/kander`.
- Single binary entry point: `cmd/kander`. `main.go` only calls `internal/cli.Run`; the other files in that directory wire in implementation packages via blank imports, triggering each package's `init` bindings. Do not add a second command entry point.
- Running `kander` without a subcommand opens the terminal kanban directly: `internal/cli` exposes `DefaultRunner`, set by `internal/tui` at registration time; `internal/cli` does not depend back on `internal/tui`.
- `internal/cli` centrally maintains the command-name and Runner registry. `doctor`/`config`/`version` and the board commands are wired in this package; launch/liveness/notify/review/takeover override their Runners from the implementation packages. `check` is first wired to the board structural check, and the full binary lets liveness override it with structural check plus the liveness section. The storage commands of `dispatch` are first wired to board, and the full binary lets launch add the Git integration evidence and exit-fact verification entry.
- Package responsibilities:

| Package             | Responsibility                                                       |
| ------------------- | -------------------------------------------------------------------- |
| `internal/cli`      | Command-name and Runner registry, global `--lang`, argument parsing and dispatch |
| `internal/config` | Install scopes, optional project `.kander-config.json` overlay merge and sparse overlay writes, `config.json` schema/repair, embedded execution-agent definitions under `internal/config/agents/` (`go:embed`, `schema_version` 1; user `agents.<name>` overlays them), read access for language/agent language/launcher/agents/models/rules/TUI, and the launcher name registry (built-in defaults plus names registered by the terminal layer; `config` never imports `internal/terminal`) |
| `internal/version`  | Injected build version; String() returns it or `dev`                  |
| `internal/i18n`     | go-i18n message catalogs and template rendering; one general catalog per language plus optional topic catalogs under `locales/<topic>/`, merged into one bundle so no shared catalog crosses the reviewable file ceiling; does not depend on config, the language is passed in by the caller |
| `internal/fs`       | POSIX no-follow and Windows handle/reparse/DACL/shared and exclusive locks |
| `internal/process`  | Agent CLI resolution, UTF-8 task files, argv/env invocation construction, declarative output parsing and placeholder expansion |
| `internal/board`    | Board location, revision/CAS/multi-file transaction recovery, journal pending/committed partitions and retention cleanup, controlled updates and lifecycle commands, review run/batch identity, originals, per-card publication indexes and integrity checks, dispatch intents, review-original bindings, epochs, wrap-up-only grants and atomic receipts, start attempts and success/rollback originals, and the private machine-local cache root below `.kander/` (`CacheRoot` / `EnsureCacheDir`) |
| `internal/launch`   | start/resume, structured Start/PreviewStart entry points reused by CLI/TUI, takeover launches, board-free issue takeover sessions (`StartTriage`/`PreviewTriage`, evidence-path prompt, cleanup after a failed start), liveness confirmation and version-based failure rollback; reaches terminals only through `terminal.Backend` and degrades on backend capabilities; orchestration Git reconciliation one-way reuses review, dispatch's Git integration and exit-fact verification |
| `internal/focus`    | Read-only consumption of card WINDOW: parses the address through the owning backend and calls its `Focus` operation; called asynchronously by the TUI |
| `internal/probe`    | Generic bounded process execution with process-tree ownership, probe budgets, and localized deadline/cancellation diagnostics |
| `internal/terminal` | The `Backend` interface every terminal access goes through: operations, `WINDOW` address contract, capability flags, `CommandError` classification, runners, the backend registry and auto resolution; see [Terminal backends](docs/terminal-backend.md) |
| `internal/terminal/tmux`, `internal/terminal/herdr`, `internal/terminal/direct`, `internal/terminal/builtin` | The tmux (`tmux`, `tmux-session`), herdr (including the herdr socket session report and pane focus) and no-container (`foreground`, `console`) backends, and their registration; they never import launch, liveness, notify, takeover, focus, menu or tui |
| `internal/liveness` | check's liveness section, session reverse lookup, and the subscribe JSON Lines event stream |
| `internal/notify`   | notify direct delivery, busy/expiry decisions, resume recovery and revision-conflict handling |
| `internal/takeover` | dismiss, and old-container cleanup after a successful resume takeover, through backend topology, exit delivery and container close operations |
| `internal/window`   | Card `WINDOW` write-back; reuses board transactions, stale rollback preserves newer records |
| `internal/review`   | The single review gate of `kander review` (invocation and parsing from agent definitions) and closed-batch historical Git verification |
| `internal/flow`     | Read-only consumption of the options-session configuration, producing per-scale flowchart charts (execution node plus both review stages with each role's model/effort and stage policy) for the options panel, which draws the fix and incremental re-review loops; does not depend on TUI or menu |
| `internal/tui`      | The terminal kanban for bare `kander`, the Huh options panel, and the issue takeover confirmation dialog |
| `internal/issue`    | Provider-neutral GitHub repository identity, `[HOST/]OWNER/REPO` validation, structured errors, diagnostic sanitizing and redaction, the issue query contract (state/labels/search/limit, bounded pagination and body/comment bounds), canonical issue URLs, the `kander issue repo/list/show` command front end, the atomic import of one issue into a backlog card with its source snapshot (`kander issue import`, schema-versioned attachments, source-key uniqueness), and the machine-local issue snapshot cache (sha256 source-key file names, outer cache version/source key/fetch time/digest around the import record, read-back re-normalization, oldest-first pruning), plus the refreshed takeover evidence below the private cache (`issue.json`/`issue.md`) and the `kander issue triage` command front end that dispatches to the takeover starter the launch layer registers |
| `internal/issue/ghcli` | The GitHub CLI provider: the `gh`/`git` process boundary (direct argv, validated cwd, deadline, bounded streams, strict UTF-8/JSON), remote enumeration and ambiguity detection, canonical identity confirmation, the read-only doctor probe, and the REST-backed issue list/show queries (fixed REST API version, pull-request filtering, bounded pages and comments) |
| `internal/menu`     | doctor/config, environment probing and repair, `menu.Session` shared with the options panel |
| `internal/install`  | First-run wizard, `kander install`, rules extraction and doctor repair |

- Card creation is unified as `<task-id>/spec.md`; SIZE decides small/large semantics. `Entry.Kind`/`TaskSummary.kind` in public snapshots express the size (the in-package structural scan leaves Kind empty; attachSize fills it in), and physical form uses `Entry.IsDirectory()`. File cards are read-only compatible; init migrates them through the existing transactions inside an explicit maintenance window. Never derive completion gates or models from the directory form.
- `kander issue import` publishes a card and its `source/github-issue.json` / `source/github-issue.md` attachments in one recoverable board transaction; the source-key uniqueness check runs inside the same exclusive-board transaction, so concurrent or repeated imports return one canonical card and an interrupted publication never leaves a card without its source. The canonical source key and URL come from the confirmed repository identity and the fixed card sections are authored from that identity and the issue number only; the issue title becomes the card heading only after it is sanitized to a single line, so it cannot add a section or rewrite metadata, and the remote body and comments stay in the attachments as untrusted data. When one issue's work splits across several cards, only the imported anchor card keeps the binding; sibling cards record a `SOURCE_ISSUE: <canonical source key> (anchor: <task-id>)` line in `DISCUSSION` and never copy the source attachments.
- The runtime board data directory is still `kanban/` in the main worktree, and the override is still `KANBAN_DIR`. The config keys `kanban_agent` / `kanban_agents` / `models.kanban` keep the schema from the onevoke era (Kander's former name) and are not renamed.
- `rules` stores the seven optional module switches collaboration/code/git/review/task_intake/task_groups/reporting. New configs default to all on; a valid old config missing the whole rules section keeps all seven on, while missing keys inside the section are off. Parsing and doctor repair reuse internal/config, and the switches are independent of the `welcome_complete` initialization state. task_groups depends on git; the TUI options panel reuses `menu.Session.SetRules`, and start/resume/takeover/notify re-check the task-group dependency before side effects. Card task-group parsing reuses `board.TaskGroupFrom`, including the legacy discussion-section fields.
- `language` only decides kander's own interface and command output language, with values `cn`/`en`/`ja`. `agent_language` is the language the agent uses with the user, a free-form string (such as `en`, `zh-CN`, `ja`) that must be non-empty, single-line, and at most 64 characters; when the key is missing it is derived from `language` (cn gives `zh-CN`, en gives `en`, ja gives `ja`), and doctor repair derives it the same way. The options panel offers a fixed candidate list, while the schema still accepts hand-edited values outside the list. `kander new` writes the then-current `agent_language` (or the explicit `--language` value, same validation) into the card's `LANGUAGE` field; the card's language is frozen from then on and takes precedence over the configuration. Old cards missing the field fall back to the configuration, and `kander check` does not validate it. The task files written by `kander start` / `resume` / `notify` (direct delivery and recovery) carry a fixed English language instruction whose value matches the card `LANGUAGE` (or the configuration when the field is missing). The installer no longer extracts rules per language, and `kander-rules-state.json` no longer records a language.
- `review_stages.<large|small>.<role>` stores `auto` / `skip` / `required` for the four review roles per task scale; a missing section, scale, or role defaults to `auto`. A legacy flat `{role: mode}` object still loads and applies to both scales; saves rewrite it as the two-scale form. The role exceptions at the top of this file take precedence over this configuration. When a task-group batch mixes sizes, use the `large` scale.
- `reviewers.<large|small>.<role>` stores the reviewer agent for each role per task scale; a missing section, scale, or role falls back to the default agent. A legacy flat `{role: agent}` object still loads and applies to both scales; saves rewrite it as the two-scale form. Runtime lookup uses `config.ReviewerFor(cfg, scale, role)`.
- Models in `models.kanban.<agent>` are stored per task size in `large_model` / `small_model`. The reasoning efforts for codex/claude/grok/pi live in `large_effort` / `small_effort`, and the old shared key `model` is still accepted: an empty size model falls back to it. Cursor only accepts the two size model keys, with no shared `model` or reasoning effort. The options panel edits the size keys only.
- `models.review_roles.<role>` stores the role's own `model` / `effort` overrides plus optional per-scale `large_model` / `small_model` / `large_effort` / `small_effort`. Defaults and valid old configs may be empty; empty entries fall back at runtime via `config.ReviewModelFor(cfg, agent, role, scale)` to the shared role keys, then to the `models.review.<agent>` value of the agent given by the caller. Only when entering the "Review and models" section of the options panel does `menu.Session` try to fill missing scale entries from the current Reviewer's values; when the Reviewer's source value is empty or absent, the empty value is kept. `kander review` can specify a reviewer explicitly, which is not necessarily the role's configured Reviewer.

## TUI Stack

The terminal interface uses the Charm libraries as one set, with no hand-rolled terminal backend:

| Library    | Purpose                                                     |
| ---------- | ----------------------------------------------------------- |
| Bubble Tea | Runtime: alt-screen, input, mouse, resize, `tea.Exec` terminal suspension |
| Lip Gloss  | Styling and layout: columns composed with `JoinHorizontal`, popup borders, theme colors |
| Bubbles    | `viewport` (detail and report scrolling), `spinner` (environment probing) |
| Huh        | All options-panel forms: the root menu and each section      |
| Glamour    | Markdown rendering of card bodies                           |

- The geometry of the board and detail views (column X/width, card rows) is computed by this package itself; mouse hit testing and drag-select copying depend on it. Do not hand layout over to a component library.
- Selections and cursors are always computed on plain text with ANSI stripped (`ansi.Strip`), then recolored by span at render time.
- Popups are composited onto the underlying frame by display column via `overlay()`, not by full-screen replacement.
- The board view launches backlog/todo cards after `s` confirmation; the TUI calls `internal/launch`'s structured entry points one way and reuses board's controlled migration. Background launches and warnings flow back through pendingWork rather than writing stdout/stderr directly; foreground/console only prompt to use the CLI. Pressing `s` pops the dialog immediately, with the frame title tracking loading, confirmation, starting, and the recorded success or failure; it previews only the target card through pendingWork, discarding stale results by task ID and request sequence; while loading, the wheel keeps operating the board and changing the selection closes the old dialog. After confirmation the launching state is kept, and on completion the dialog shows success/failure and warnings; the result body keeps full content in a viewport with wheel scrolling, and any key closes the terminal state. Narrow screens compress the result footer, and when necessary a temporary overlay that does not capture input shows the full result.
- The board's `g` key opens the read-only GitHub Issues overlay; focusing the agent window moved to `f`/`F`. The overlay is composited with `overlay()` and closing it leaves the board model untouched. It shows the filtered list and, on narrow screens, a list page and a detail page; the wide layout keeps both panes side by side. List, detail, comments, and refresh run through `pendingWork` and drop stale results by request sequence and issue number, so no network call blocks input and nothing writes stdout/stderr. Comments are fetched only when a detail opens; remote text is sanitized before rendering, and `o` opens the browser only with a URL rebuilt from the validated canonical identity. Selecting a list row loads the content without a key press: a cache hit paints first, the request is debounced on the shared 200ms tick grid with at most one content request in flight, and a background refresh that changed nothing keeps the scroll position while a changed one rewrites the cache and reports the update. `i` imports the selected issue or jumps to the local card already bound to it (`I` imports with comments); both run through `pendingWork`, bind the result to the request sequence and the issue identity, and never overwrite the card when the remote revision is newer.
- The overlay's `s` key confirms a takeover session instead of editing a card contract: the TUI calls `issue.StartTriage`, the same path as `kander issue triage`, which writes the refreshed evidence below the private cache and starts one session through the starter hook `internal/launch` registers. An unbound issue opens the dialog with the resolved agent and launcher and starts on `y`/`Enter`; a bound backlog card offers the default jump (`y`/`Enter`) plus contract completion (`s`); a bound card in any other state jumps directly and reports its state. The TUI starts only background launchers and points at the CLI for `foreground`/`console`; it never writes `CARD_REVIEW:`, never creates or moves a card on this path, and every step runs through `pendingWork` and is dropped when its dialog sequence or selected issue no longer matches. While the overlay is open the local index is refreshed through the shared tick grid with a five-second throttle; list rows show the bound task ID and its state. The takeover protocol ships as the released `rules/KANDER-ISSUE-RULES.md`: it names the untrusted-data clauses, the four investigation exits, the consent-before-creation rule, the anchor-card binding, and the module degradation; the takeover prompt points at it under the scope's rules root instead of inlining it.
- `internal/menu` must not import `internal/tui`; the TUI options panel reuses the `menu.Session` configuration logic one way.
- `internal/issue` holds the provider-neutral identity, error, and command contract; it depends one way on `internal/board` for the atomic issue import and never imports launch or TUI. `issue.StartTriage` prepares the evidence and calls the `TriageStarter` hook that `internal/launch` registers at init, so the CLI and the TUI share one launch path and the `issue` package stays provider-neutral. The GitHub CLI provider lives in `internal/issue/ghcli` and is bound by the `issue` command in `internal/cli`. Kander never calls or stores `gh auth token`: credentials stay inside `gh`, and Kander neither clears nor rewrites `GH_TOKEN`, `GITHUB_TOKEN`, `GH_ENTERPRISE_TOKEN`, `GITHUB_ENTERPRISE_TOKEN`, `GH_HOST`, or `GH_REPO`. Remote candidate enumeration and ambiguity detection are Kander's own because `gh repo view` silently picks a remote when several exist.
- Cross-platform process-boundary tests inject a fake `gh`/`git` by copying the test binary under those names into a temporary directory and prepending it to `PATH` (`internal/issue/ghcli/ghclitest`); they must not depend on a scripting host.

## Subcommands

The Runner registry contains: `doctor` `config` `version` `install` `review` `init` `issue` `list`/`ls` `show` `update` `new` `move` `pick` `start` `resume` `notify` `dismiss` `check` `guard-write` `dispatch` `coordinator` `subscribe`. `help` is a special branch that prints the top-level help directly and does not enter the Runner registry. Bare `kander` opens the terminal kanban; the global flag is `--lang {cn,en,ja}`.

## TUI Tests

- The test cases for `internal/tui` stay in the package and cover stable rendering constraints and interactions: light/dark canvas backgrounds, screen filling, visible columns and focus, Markdown conversion, ANSI-stripped content, preference read/write, keys and mouse, the options panel, the GitHub Issues overlay (keys, stale-result dropping, debounce and cached first paint, scroll preservation, wide/narrow layouts, sanitized remote text, mouse, import/jump), the takeover dialog (preview and confirmation phases, bound-card jump and contract exits, foreground refusal, start failure, stale results, throttled index refresh), and PTY smoke tests for the board, the issues overlay, and the takeover dialog.
- Do not build full-screen snapshots of frequently changing borders, logos, or status-bar text. Full visual results are still checked in a real terminal.

## Test Commands

Shared by POSIX and Windows (Windows-specific tests skip on non-Windows):

```sh
go test ./...
```

For a single package:

```sh
go test ./internal/fs
go test ./internal/config
```

The repository has no Python yet. Run `go test ./...` at the module root before committing; no coverage threshold is set currently. Tests must isolate themselves in temporary directories and never rewrite the user's real board or `$HOME` configuration.

## Windows Handles / Reparse / DACL

Go runtime writes of configuration, board migration, the review runtime, Git exclude, and the binaries and rules extracted by the installer go through `internal/fs`.

- Reject symlinks, junctions, and other reparse points component by component from the volume/UNC anchor.
- Config reads/writes neither check nor automatically tighten permissions: POSIX saves keep the existing file mode and new files and directories follow the umask; on Windows new files and directories inherit the parent directory's ACL.
- Other private objects managed by `internal/fs` get a protected DACL exclusive to the current user at the moment of creation; never publish with an inherited ACL first and tighten later.
- Blocking exclusive locks use `LockFileEx`; the POSIX counterpart is `flock`.
- Atomic replacement in `internal/fs` is relative to a pinned parent handle; any secure-backend failure errors out explicitly and never silently falls back to plain path APIs.

## Released Rules

- `rules/KANDER-AGENTS.md` is the released rules entry; it first reads `kander config --json` for the current scope. `KANDER-BASE-RULES.md` and `KANDER-KANBAN-RULES.md` are the tool protocol; the other seven module booklets load per switch and per need, all live in `rules/`, and exist only in English. No customized rule files are generated, and disabled modules are not loaded through cross references.
- The root `AGENTS.md` only constrains development of this repository; when changing the released workflow, change `rules/`. Do not write implementation details into the released booklets, and do not put the package map into `KANDER-AGENTS.md`.
- The runtime-created `kanban/` is machine-local shared data; never commit it and never write it into the project `.gitignore`.

## Documentation Index

- [Review disposition and completion gate](docs/review-disposition.md): plans, author records, batch aggregation, Git evidence, and the done gate.
- [Review evidence and recovery](docs/review-evidence.md): run/batch identity, originals, per-card publication, indexes, and consumption interfaces.
- [Card transactions and recovery](docs/card-transactions.md): lock order, revisions, controlled commands, the multi-file publication interface, and the recovery format.
- [Write guard](docs/kanban-write-guard.md): guard-write integration and capability boundaries.
- [Directory cards and migration](docs/directory-cards.md): SIZE, the read-only transition, the maintenance window, and per-stage recovery.
- [Probe deadlines and cancellation](docs/probe-deadlines.md): single-card and batch budgets, the concurrency cap, observation identity and validity, the context API, process reaping, plus system I/O and platform verification boundaries.
- [Subscription facts and member sets](docs/subscription-facts.md): JSONL versions, revisions, dynamic group references, integrity warnings, coordinated read deadlines, durable dispatch summaries and confirmation-deadline attention events, plus bounded probing and the output lifecycle.
- [Durable dispatch protocol](docs/durable-dispatch.md): stable IDs, atomic accept/complete receipts, execution epochs, delivery reconciliation, and compatibility boundaries.
- [Orchestration checkpoints and recovery](docs/coordinator-recovery.md): coordinator epochs, CAS, snapshot reconciliation, originals and Git wrap-up evidence, recovery boundaries.
- [Original reproduction acceptance mapping](docs/recovery-regressions.md): the 13 original bad behaviors, their owning regressions, and cross-module recovery acceptance.
- [Custom execution agents](docs/custom-agents.md): executable names, process names, dialect/argv templates, session policies, and review boundaries.
- [Output parsing](docs/output-parsing.md): declarative `source`/`format`/`select`/`parse`/`join`/`success`, line conditions, and `{name}` / `{{` `}}` placeholders.
- [Terminal backends](docs/terminal-backend.md): the `internal/terminal` operation set, the `WINDOW` address contract, capability flags, error classification, and the rule that callers reach terminals only through `terminal.Backend`.
- [GitHub issue import](docs/github-issue-import.md): source key and idempotency, snapshot schema and limits, the snapshot cache, attachments, label mapping, card contract, the takeover session and its evidence, recovery, and the threat model.
