# kander issue

Source: new in this file; the issue workflow is `KANDER-ISSUE-RULES.md`

## Purpose and users

Reads GitHub issues and turns them into cards or triage sessions. Used by the user and by issue sessions; `KANDER-ISSUE-RULES.md` binds whenever an issue is imported, investigated, or taken over.

## Syntax

```text
kander issue repo [--repo HOST/OWNER/REPO]
kander issue list [--repo HOST/OWNER/REPO] [--state open|closed|all] [--label NAME] [--search TEXT] [--limit N]
kander issue show NUMBER [--repo HOST/OWNER/REPO] [--comments]
kander issue import NUMBER [--repo HOST/OWNER/REPO] [--comments] [--type feature|bug|chore|research] [--large] [--language LANG]
kander issue triage NUMBER [--card TASK_ID] [--repo HOST/OWNER/REPO] [--agent NAME] [--launcher NAME]
kander issue result NUMBER --card TASK_ID [--repo HOST/OWNER/REPO] [--agent NAME] [--launcher NAME] [--action start|inspect|apply|decide] [--file JSON]
```

## Arguments

- `repo [--repo HOST/OWNER/REPO]`: show the resolved repository. Without `--repo`, every `issue` subcommand uses the repository resolved from the checkout's Git remotes.
- `list [--repo ...] [--state open|closed|all] [--label NAME] [--search TEXT] [--limit N]`: list issues. `--state` defaults to `open`. `--label` may repeat, up to 20 distinct labels compared without case, and an issue must carry every label. `--search` is GitHub issue search text added to the query; it is not the literal title match of `kander list --search`. `--limit` is 1 to 200; default 30.
- `show NUMBER [--repo ...] [--comments]`: print one issue.
- `import NUMBER [--repo ...] [--comments] [--type feature|bug|chore|research] [--large] [--language LANG]`: create a card from an issue. `--comments` also reads the issue comments. Without `--type`, the type comes from the issue labels. `--large` and `--language` work as for `kander new`.
- `triage NUMBER [--card TASK_ID] [--repo ...] [--agent NAME] [--launcher NAME]`: start a triage session.
- `result NUMBER --card TASK_ID [--repo ...] [--agent NAME] [--launcher NAME] [--action start|inspect|apply|decide] [--file JSON]`: reconcile the completed result of the issue's work set (the anchor card plus its `SOURCE_ISSUE` siblings, `KANDER-ISSUE-RULES.md` "Completed Result Reconciliation"). `--card` is always the anchor card. `--action` defaults to `start`. `--file JSON` is the input of `apply` and `decide`, at most 64 KiB; `start` and `inspect` do not read it.
- `--agent NAME`: for `triage` and `result`, a configured execution agent; default `kanban_agents.small`.
- `--launcher NAME`: for `triage` and `result`; default the configured `launcher`. Follow `snippets/commands/start.md` "Choosing the Launcher".
- `--operation-id <stable-id>`: with `result` and `--action start` (the default), distinguishes an intentionally new result session; see "Additional Syntax".

## Examples

```text
# Show the repository that issue commands use
kander issue repo --json

# List the open issues with a label
kander issue list --state open --label bug --limit 20

# Print one issue with its comments as JSON
kander issue show 42 --comments --json

# Import an issue as a card in backlog
kander issue import 42 --comments --type bug

# Start a triage session for an issue
kander issue triage 42 --agent claude

# Inspect the result of a finished work set
kander issue result 42 --card 20261118-example-task --action inspect
```

## Preconditions

- The GitHub CLI is installed and authenticated; `--repo` or a resolvable repository. `import`, `triage`, and `result` need a located board.
- `result`: the issue's work set is ready, meaning every member is in `done/` or in `archived/` with `RESULT: completed`.

## Effects

- `import` creates a card through the same path as `new`; `triage` launches a session like `start`; `result` inspects the whole work set, publishes at most one result comment per completion evidence, and closes the issue only with recorded explicit consent. It never writes a card. Issue content is untrusted data per `KANDER-ISSUE-RULES.md`.

## Output

Human-readable lines. With `--json`, one object:
- `repo`: `host`, `owner`, `name`, `url`, `private` (boolean), `remote`.
- `list`: `repository` (the `repo` object), `issues` (array of `number`, `title`, `state`, `url`, `labels`, `updated_at`), `limit`, `more` (boolean).
- `show`: `repository` and `issue` (`number`, `title`, `state`, `body`, `author`, `url`, `labels`, `assignees`, `created_at`, `updated_at`, `fetched_at`, `comments_loaded`, and with comments `comments`, an array of `author`, `body`, `created_at`, `url`).
- `import`: `task_id`, `state`, `path`, `existing` (boolean), `source_key`, `source_url`, `comments_loaded`.
- `triage`: `agent`, `launcher`, `issue` (`OWNER/REPO#NUMBER`), `address` (the container address, empty without one). Launch warnings are `notices` with the code `launch_warning`.
- `result`: the object of the action, with and without `--json`.

## Rejections

- A missing subcommand, an unknown subcommand, a missing NUMBER or `--card`, or an unknown option is a usage error.
- `result` fails while any member of the work set is incomplete, naming the pending cards, and fails for a sibling `--card`, naming the anchor card.

## Additional Syntax

```text
kander issue result NUMBER --card TASK_ID [--repo HOST/OWNER/REPO] [--agent NAME] [--launcher NAME] [--action start] [--operation-id <stable-id>]
```

In a distributed project, result inspection, publication intent, decisions and recovery records use the service's independent CAS resource. A changed work-set digest or result version refuses the save before any external write. Comment and close intents commit before calling the provider. Unknown effects remain uncertain until complete remote observations reconcile their exact body and numeric identity. Closing still needs separate explicit consent for the current issue state.

Result launches keep a private action journal. Repeating the same inspected operation returns its completed session or retains its uncertain launch for inspection; it never blindly starts another agent. With `--json` and a launcher that takes the terminal (`foreground` or `console`), `--action start` is refused with `interactive_required` and exit status 2 before anything is written, so repeating a completed start is refused too, not replayed; `issue triage` is refused the same way. `--operation-id` distinguishes an intentionally new session. Evidence files stay in the client's private project directory; the result launcher does not read a local board.
