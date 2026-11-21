# Structured JSON output

Every command except the bare `kander` and the internal `worker` accepts the global flag `--json`. It is declared once, on the root command, next to `--lang`; no command declares it again and no Syntax line of a contract lists it. The result of a command with `--json` is for an agent or a program to parse, never for a person to read.

## Output convention

- **Success.** Standard output holds one JSON object and a newline: the result object of the command form. Standard error is empty. Field names are `snake_case`. There is no common wrapper, so the object is the object the contract `## Output` section of the command describes.
- **Failure.** Standard output is empty. Standard error holds one object and a newline: `{"error":{"code":"...","message":"..."}}`. The exit status is the same as without `--json`. `code` is a stable English value from the table below. `message` is the localized text of the failure and follows `--lang`; the service text of a board rejection is the one exception, see "Code priority".
- **Notices.** A command that prints a remark on standard error without `--json` puts it in the result object as `notices`, an array of `{"code":"...","message":"..."}` (the field is left out when there is no remark). A failure that came with remarks carries them as `notices` beside `error`. `move` puts the completion report template in the field `completion_report` instead.
- **One shape.** The forms that always print JSON (`dispatch`, `coordinator`, `notify`, bound `move`, `request show`, `subscribe`) print the same object with and without `--json`; only the place of the notices differs.
- **A finished analysis is a result.** A command whose contract ties an exit status to a completed analysis writes the result object to standard output and exits with that status. Standard error stays empty. These are `check` (exit 1 for findings), `check delivery` and `check overlap` (exit 1), `coordinator reconcile` (exit 1 or 3), `doctor` (exit 1 for an unhealthy installation), `terminal list` and `terminal test` (exit 1), and a `review` run whose evidence reached every card (the exit status of the run). A check that could not finish is a failure.
- **Not in scope.** The bare `kander` (the board) and `worker` reject `--json` as an argument error. Help is out of scope here: `--help` and `kander help` are described in [Command help](command-help.md).

## Error codes

| `code` | Exit | Meaning |
| --- | --- | --- |
| `usage` | 2 | An argument error: an unknown option, an option without a value, a wrong number or value of arguments, a combination the contract refuses. It also reports an argument error found by a command after parsing, and `--json` given to `kander` or `worker` |
| `interactive_required` | 2 | The form needs a terminal; see "Interactive forms" |
| `not_configured` | 1 | The checkout is not bound to a service project (`kander init`) |
| `unreachable` | 1 | The service did not answer |
| the service code | 1 | A service rejection: `invalid_request`, `not_found`, `revision_conflict`, `held_by_other`, `lease_invalid`, `lease_active`, `taken_over`, `claim_required`, `evidence_incomplete`, `protocol_incompatible`, `idempotency_mismatch`, `project_mismatch`, `project_not_empty`, `board_rejected`, `not_ready`, `internal` |
| the board code | 1 | A board rejection, for example `board.document_not_found` or `board.transaction_conflict`; the message keys of `internal/i18n` are the values |
| `issue_<kind>` | 1 | A failure of the GitHub provider: `issue_invalid_query`, `issue_not_repository`, `issue_no_remote`, `issue_ambiguous_remotes`, `issue_cli_unavailable`, `issue_unauthenticated`, `issue_not_found`, `issue_rate_limited`, `issue_timeout`, and the other kinds of `internal/issue` with `-` written as `_` |
| `check_<kind>` | 2 or 3 | An analysis of `check delivery` or `check overlap` that could not finish: `check_invalid_ref`, `check_not_ancestor`, `check_not_repository`, `check_git_unavailable`, `check_no_merge_base`, `check_output_limit`, `check_parse`, `check_internal` |
| `review_failed` | 1, 2 | A review that failed before it published evidence; the message holds the messages of the run |
| `launch_unresolved` | 3 | A start or a takeover that failed late with executor evidence; the message holds the next steps |
| `server_not_running` | 1 | `server status` found no server |
| `cancelled` | 130 | `server import` was interrupted |
| `failed` | 1 | A failure without a more specific class |

### Code priority

The code of a failure is the first of these that applies:

1. The code the command gave it: `usage`, `interactive_required`, `launch_unresolved`, `check_<kind>`, `review_failed`, `server_not_running`, `cancelled`.
2. A service error: the board code when the service reports one, otherwise the service code. The message of a service error that carries a board code is the text the service sent; its language is decided by the service, not by `--lang`. Any other service error has a message of the client (`--lang`) followed by the service text.
3. An unreachable service: `unreachable`.
4. A checkout without a binding: `not_configured`.
5. A board rejection of a local operation: the board code.
6. A failure of the GitHub provider: `issue_<kind>`.
7. Everything else: `failed`.

## Notices and stream lines

| `code` | Where | Meaning |
| --- | --- | --- |
| `agent_warning` | `config`, `check` | An agent of the configuration is unusable |
| `config_ignored` | `config` | The configuration holds a key or a `models.*` agent entry that this version ignores; one notice per item |
| `agent_unusable` | `config` | An `agents.<name>` entry this version cannot use, or a role that names an agent it does not know; one notice per item |
| `check_diagnostic` | `check` | A diagnostic line of the structural check |
| `launch_warning` | `start`, `resume`, `orchestrate`, `issue triage` | A warning of the launch |
| `rules_embedded_fallback` | any command that renders a rules template | The installed copy of a template is missing and the embedded one was used; run `kander doctor` |
| `worker_start_failed` | `move --owner`, `coordinator claim` | The renewal worker did not start |
| `coordinator_release_final` | `coordinator release` | The release ended the coordinator execution; the text names the execution, the group, and the generation |
| `server_version_warning` | any command that opens the service | The service runs another version than the client |
| `server_log` | `server start --foreground` | A log line written before the server can serve, among them one warning per setting the configuration holds that this version ignores |
| `issue_result_handoff` | `move` | The move made the work set of a GitHub issue ready for its result |
| `reconcile_blocked` | `coordinator reconcile` | Every member was blocked; the text is the error |
| `review_archive_identity`, `review_card_published`, `review_interpretation_pending`, `review_process_evidence`, `review_output`, `review_error` | `review` | The remarks of a review run |

A command that makes a remark has to put it through the notice function of `internal/command`; it never writes to standard error itself.

## Streaming commands

`subscribe --follow` and `server start` (the foreground form) keep running after they started to write. Their standard output is not buffered.

- A failure before the first line leaves standard output empty and writes one error object to standard error.
- A failure after output started keeps the lines already written and ends standard error with one error object. The exit status is the same as without `--json`.
- Every other line on standard error is one JSON object: `{"notice":{"code":"...","message":"..."}}`. `server start --foreground` is the exception for its logs, which were JSON lines already.
- `server start --foreground --json` writes one line to standard output as soon as the server can serve: `listen`, `data_dir`, `pid`, `version`. `subscribe` writes its event lines as described in its contract.

## Interactive forms

A form that needs an answer in a terminal, or hands the terminal to an agent, is refused with `interactive_required` and exit status 2. The refusal comes before any side effect: no configuration is written, no container is created, no agent is started, no card is claimed.

| Form | Why |
| --- | --- |
| `install` | The wizard asks questions |
| `uninstall` without `--yes` | It asks for a confirmation (with `--yes` the result lists the items) |
| `pick` and `start` without a task ID | They ask which card |
| `start`, `resume` (a takeover or a continuation), `orchestrate`, `issue triage`, `issue result --action start` with the `foreground` or `console` launcher | The agent takes the terminal, or a console window, of the person who ran the command |
| `doctor` when a repair needs a confirmation | A modified rule file to back up and replace, an unusable agent to replace, an agent session file to store, the herdr install, the Homebrew upgrade, or the kander executable to keep when several are on PATH. The version guard (a running binary older than one on PATH) comes before the check and is reported as the result. The check is read-only and runs before any repair; with nothing to ask, `doctor --json` repairs and reports as usual |

A call is refused for its launcher only when it would start an agent. A replay of a completed launch (the same command repeated after its receipt completed), a message to a live executor, and a repeated command whose receipts are only reconciled start nothing, so the launcher does not matter for them. One decision, `launchWouldStart` in `internal/launch`, takes the state of the receipt and decides; it reads nothing and writes nothing. It runs before the first write of every entry: before the session of the host is opened (which creates the client identity and registers the project), when the receipts of the current server and project, read without writing under the exact key of the invocation, hold none that the call would replay or reconcile (a host with no client identity, or a project the service does not know, holds none, whatever the receipts of other projects are); before the receipt of the launch or of the command is written; and again under the lock of that receipt, with the state it has then. A receipt of a failed launch counts as a launch where the entry may start a new attempt over it (`start`, an explicit takeover with `--agent`, `orchestrate`), and the refusal leaves it as it was. An implicit native `resume` keeps a failed attempt: it is not refused for its launcher but ends with the error of the settled attempt, as without `--json` (`rules/snippets/commands/resume.md`). `issue triage` has no receipt and is refused first; `issue result --action start` is refused first too, so repeating a completed `issue result --action start` with a launcher that takes the terminal is refused as well.

The check before the session reads the project from the service (a read that never registers it). When that read gets no answer (the service is still unreachable or not ready after the retries of the client), or a receipt cannot be read, the call ends with that error before the session is opened (`unreachable`, `not_ready`, and so on), and it does not register the project or bind its Git source; the check never hands such a call on to the session, whose registration would come before the refusal. This holds for a call that would be refused and for one that would only replay or reconcile a receipt, because without the read the two cannot be told apart. Repeat the same command once the service answers: a call that starts an agent is then refused with `interactive_required`, and a call that replays a receipt replays it. A call that is not a refusal candidate (without `--json`, or with a launcher that has a container) does not make the read. An implicit `resume` and the takeover that follows the session are launched after the session was opened, so they do not repeat the check.

The question a command asks before it starts the local service is not asked with `--json`: the command fails with `unreachable`.

## Where the result objects are

Each command contract describes the result object of every command form in its `## Output` section:

| Command | Contract |
| --- | --- |
| `check` | `rules/snippets/commands/check.md` |
| `config` | `rules/snippets/commands/config.md` |
| `coordinator` | `rules/snippets/commands/coordinator.md` |
| `dismiss` | `rules/snippets/commands/dismiss.md` |
| `dispatch` | `rules/snippets/commands/dispatch.md` |
| `doctor` | `rules/snippets/commands/doctor.md` |
| `guard-write` | `rules/snippets/commands/guard-write.md` |
| `init` | `rules/snippets/commands/init.md` |
| `install` | `rules/snippets/commands/install.md` |
| `issue` | `rules/snippets/commands/issue.md` |
| `list` | `rules/snippets/commands/list.md`, `docs/board-snapshot.md` |
| `move` | `rules/snippets/commands/move.md` |
| `new` | `rules/snippets/commands/new.md` |
| `notify`, `request` | `rules/snippets/commands/notify.md` |
| `orchestrate` | `rules/snippets/commands/orchestrate.md` |
| `pick` | `rules/snippets/commands/pick.md` |
| `resume` | `rules/snippets/commands/resume.md` |
| `review` | `rules/snippets/commands/review.md` |
| `server` | `rules/snippets/commands/server.md` |
| `show` | `rules/snippets/commands/show.md` |
| `start` | `rules/snippets/commands/start.md` |
| `subscribe` | `rules/snippets/commands/subscribe.md` |
| `terminal` | `rules/snippets/commands/terminal.md` |
| `uninstall` | `rules/snippets/commands/uninstall.md` |
| `update` | `rules/snippets/commands/update.md` |
| `version` | `rules/snippets/commands/version.md` |

## Implementation

`internal/command/output.go` is the one place that writes JSON. A command asks `command.JSON()` which form to print, hands its result to `command.Result` (or `command.Object` for the forms that always print JSON), reports a remark with `command.Notify`, and a failure with `command.Fail`. Packages register a classifier with `command.RegisterClassifier` to give the failures of their domain a code. The result is held until the command ends, so a command that fails after it handed in a result writes nothing to standard output.
