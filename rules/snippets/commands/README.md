# Commands

Before running any `kander` command in this session, read `<rules root>/snippets/commands/README.md` once. Before the first use of a command, read its contract file.

One contract per command, seven sections: Purpose and users, Syntax, Arguments, Preconditions, Effects, Output, Rejections. `kander <command> --help` prints the Syntax block and the contract path. The table at the end equals the block in `KANDER-KANBAN-RULES.md` "Command Contract".

Read first: `show`, `update`, `move`, `check`.

| Command | Purpose | Users | Contract |
| --- | --- | --- | --- |
| `config` | Print the effective configuration | every agent, at session start | `snippets/commands/config.md` |
| `doctor` | Check and repair the installation | user | `snippets/commands/doctor.md` |
| `version` | Print the binary version | user | `snippets/commands/version.md` |
| `install` | Interactive installer | user | `snippets/commands/install.md` |
| `terminal` | List and test launchers | user | `snippets/commands/terminal.md` |
| `init` | Create or recover the board | user, maintenance | `snippets/commands/init.md` |
| `list` | List cards by state | card creator, user | `snippets/commands/list.md` |
| `show` | Print a card and its revision | every agent on a card | `snippets/commands/show.md` |
| `new` | Create a card in `backlog/` | card creator | `snippets/commands/new.md` |
| `move` | Move a card between states | executing agent, orchestrator, creator | `snippets/commands/move.md` |
| `pick` | Move a `backlog/` card to `todo/` | creator, orchestrator | `snippets/commands/pick.md` |
| `update` | Publish a card document | executing agent, creator | `snippets/commands/update.md` |
| `start` | Launch an executing agent | creator, orchestrator | `snippets/commands/start.md` |
| `orchestrate` | Hand a plan to an orchestrator session | creator | `snippets/commands/orchestrate.md` |
| `resume` | Wake or take over an executing agent | orchestrator (via notify), user | `snippets/commands/resume.md` |
| `notify` | Dispatch items back to the executing agent | orchestrator | `snippets/commands/notify.md` |
| `dispatch` | Inspect and end durable dispatches | orchestrator | `snippets/commands/dispatch.md` |
| `dismiss` | Exit a finished agent and its container | orchestrator, user | `snippets/commands/dismiss.md` |
| `check` | Validate cards, probe liveness, Git checks | executing agent, orchestrator | `snippets/commands/check.md` |
| `guard-write` | Advisory pre-write check | agent hooks | `snippets/commands/guard-write.md` |
| `subscribe` | Stream group events | orchestrator | `snippets/commands/subscribe.md` |
| `coordinator` | Durable group checkpoints | orchestrator | `snippets/commands/coordinator.md` |
| `review` | Run a reviewer, manage review evidence | main agent, orchestrator | `snippets/commands/review.md` |
| `issue` | Import and triage GitHub issues | user, issue session | `snippets/commands/issue.md` |

```text
kander config [--json]
kander doctor
kander version
kander install
kander terminal list
kander terminal test <name> [--keep] [--skip-focus]
kander init [--maintenance] [project-path]
kander list [--mobile] [backlog|todo|working|review|done|archived|trash]
kander show [--json] <task-id>
kander new [--large] [--language <agent language>] <feature|bug|chore|research> <slug> <title...>
kander move <task-id> <backlog|todo|working|review|done|archived|trash> [--owner <agent> [--decision <reference>]] [--result <result>] [--reason <reason> --decision <reference>] [--duplicate-of <task-id>] [--expect-revision <revision>] [--dispatch-id <id> --execution-epoch <epoch>] [--delivery-commit <full-SHA>] [--disposition <card-relative-path>]
kander pick [task-id]
kander update <task-id> --document <relative-path> --file <UTF8-input> --expect-revision <revision> [--contract-decision-file <UTF8-decision>] [--dispatch-id <id> --execution-epoch <epoch>]
kander start [--agent <configured-agent>] [--launcher <launcher>] [task-id]
kander orchestrate [--agent <configured-agent>] [--launcher <launcher>] (--message TEXT | --message-file FILE) <task-id|task-group-id>...
kander resume [--agent <configured-agent>] [--timeout SECONDS] (--message TEXT | --message-file FILE) [--launcher <launcher>] [--dispatch-id <id>] [--kind fix|sync|wrap-up] [--base <full-SHA>] [--evidence-file <JSON>] <task-id>
kander notify [--pane HERDR-PANE-ID] [--timeout SECONDS] (--message TEXT | --message-file FILE) [--dispatch-id <id>] [--kind fix|sync|wrap-up] [--base <full-SHA>] [--evidence-file <JSON>] <task-id>
kander dispatch prepare <absolute-UTF8-intent.json>
kander dispatch authorize-wrap-up <absolute-UTF8-request.json>
kander dispatch show <task-id> <dispatch-id>
kander dispatch fail|cancel <task-id> <dispatch-id> <dispatch-revision> <reason> [--decision <reference>]
kander dispatch settle <task-id> <dispatch-id> <dispatch-revision>
kander dismiss [--timeout SECONDS] <task-id>
kander check [--all] [task-id ...]
kander check delivery --base <ref> [--commit <ref>] [--json]
kander check overlap --source <ref> [--head <ref>] [--json]
kander guard-write <path>
kander subscribe [--refresh SECONDS] [--heartbeat SECONDS] (<task-group> <task-id>... [--watch <task-id|task-group-id>...] | --watch <task-id|task-group-id>...)
kander coordinator show <group-id>
kander coordinator claim <absolute-claim.json>
kander coordinator reconcile <absolute-observations.json>
kander review [agent] [--task <id>]... [--run-id <id>] [--batch-id <id>] [--previous-run-id <id>] [--requirements-file <JSON>] [--advance-file <JSON>] <CWD> <base-commit> <commit> <role> <task-goal|absolute-spec-path> [review-context] [reviewed-commit]
kander review plan <absolute-CWD> <absolute-plan.json>
kander review extend-plan <absolute-CWD> <absolute-extension.json>
kander review assign <absolute-CWD> <absolute-assignment.json>
kander review disposition <absolute-CWD> <absolute-author-record.json> <expected-card-revision>
kander review interpret <absolute-CWD> <absolute-interpretation.json>
kander review map-legacy <absolute-CWD> <absolute-map.json>
kander review aggregate <absolute-CWD> <batch-id>
kander review advance <absolute-CWD> <absolute-advance-request.json>
kander review close <absolute-CWD> <absolute-close-request.json>
kander review progress <absolute-CWD> <task-id>
kander review <plan|extend-plan|assign|disposition|interpret|advance|close> --schema
kander issue repo [--repo HOST/OWNER/REPO] [--json]
kander issue list [--repo HOST/OWNER/REPO] [--state open|closed|all] [--label NAME] [--search TEXT] [--limit N] [--json]
kander issue show NUMBER [--repo HOST/OWNER/REPO] [--comments] [--json]
kander issue import NUMBER [--repo HOST/OWNER/REPO] [--comments] [--type feature|bug|chore|research] [--large] [--language LANG] [--json]
kander issue triage NUMBER [--card TASK_ID] [--repo HOST/OWNER/REPO] [--agent NAME] [--launcher NAME]
kander issue result NUMBER --card TASK_ID [--repo HOST/OWNER/REPO] [--agent NAME] [--launcher NAME] [--action start|inspect|apply|decide] [--file JSON]
```
