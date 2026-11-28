# Commands

Before running any `kander` command in this session, read `<rules root>/snippets/commands/README.md` once. Before the first use of a command, read its contract file.

`kander help --json [<command>]` prints JSON help.
Global flags: `--lang`, `--json`.

| Command | Purpose | Users | Contract |
| --- | --- | --- | --- |
| `server` | Administer | user | `snippets/commands/server.md` |
| `service` | Services | user | `snippets/commands/service.md` |
| `config` | Configuration | agents | `snippets/commands/config.md` |
| `doctor` | Repair | user | `snippets/commands/doctor.md` |
| `version` | Version | user | `snippets/commands/version.md` |
| `install` | Installer | user | `snippets/commands/install.md` |
| `uninstall` | Uninstall | user | `snippets/commands/uninstall.md` |
| `gc` | Reclaim | user | `snippets/commands/gc.md` |
| `terminal` | Launchers | user | `snippets/commands/terminal.md` |
| `init` | Initialize | user | `snippets/commands/init.md` |
| `policy` | Project policy | user | `snippets/commands/policy.md` |
| `host` | Hosts | user/orchestrator | `snippets/commands/host.md` |
| `order` | Start orders | user/orchestrator | `snippets/commands/order.md` |
| `list` | Cards | creator/user | `snippets/commands/list.md` |
| `show` | Card/revision | executors | `snippets/commands/show.md` |
| `new` | Create | creator | `snippets/commands/new.md` |
| `assign` | Assignment | user | `snippets/commands/assign.md` |
| `decision` | Decisions | user/leader/orchestrator | `snippets/commands/decision.md` |
| `move` | Move | executor/orchestrator/creator | `snippets/commands/move.md` |
| `pick` | Pick | creator/orchestrator | `snippets/commands/pick.md` |
| `confirm` | Confirm request | submitter | `snippets/commands/confirm.md` |
| `update` | Publish | executor/creator | `snippets/commands/update.md` |
| `start` | Launch | creator/orchestrator | `snippets/commands/start.md` |
| `orchestrate` | Orchestration | creator | `snippets/commands/orchestrate.md` |
| `resume` | Resume | orchestrator (notify)/user | `snippets/commands/resume.md` |
| `notify` | Notification | orchestrator | `snippets/commands/notify.md` |
| `dispatch` | Dispatch | orchestrator | `snippets/commands/dispatch.md` |
| `dismiss` | Dismiss | orchestrator/user | `snippets/commands/dismiss.md` |
| `check` | Validate | executor/orchestrator | `snippets/commands/check.md` |
| `guard-write` | Preflight | agent hooks | `snippets/commands/guard-write.md` |
| `subscribe` | Events | orchestrator | `snippets/commands/subscribe.md` |
| `coordinator` | Checkpoints | orchestrator | `snippets/commands/coordinator.md` |
| `review` | Evidence | main agent, orchestrator | `snippets/commands/review.md` |
| `issue` | Issues | user, issue session | `snippets/commands/issue.md` |

```text
kander config
kander server start [--data DIR] [--listen ADDR] [--ttl D] [--renew D] [--foreground]
kander server stop
kander server restart
kander server status
kander server import [--data DIR] --project NAME [--git-remote GIT-URL] [--from V1-BOARD-DIR] [--skip-conflicts]
kander server maintain [--data DIR] --project NAME [--maintenance]
kander server projects [--data DIR]
kander server gc [--data DIR] [--project NAME] [--dry-run] [--yes] [--older-than D] [--log-keep SIZE]
kander service install server [--listen ADDR] [--data DIR] [--ttl D] [--renew D]
kander service install worker
kander service uninstall <server|worker>
kander service status [server|worker]
kander doctor
kander version
kander install
kander uninstall [--yes]
kander gc [--dry-run] [--yes] [--older-than D] [--all-projects] [--client-only]
kander terminal list
kander terminal test <name> [--keep] [--skip-focus]
kander init [--server <URL>] [--project <name>] [project-path]
kander policy show
kander policy init [--yes]
kander policy set <key> <value>
kander host list
kander order show <order-id>
kander order cancel <order-id>
kander list [--agent-status] [backlog|todo|working|review|done|archived|trash]
kander show [--document <relative-path>] <task-id>
kander new [--large] [--language <agent language>] <feature|bug|chore|research> <slug> <title...>
kander assign <task-id> [<assignee>] [--expect-revision <revision>]
kander decision record <task-id> --kind <human|leader> --file <JSON>
kander decision list <task-id>
kander decision mode <task-id> <leader|assignee>
kander move <task-id> <backlog|todo|working|review|done|archived|trash> [--owner <agent> [--decision <reference>]] [--result <result>] [--reason <reason> --decision <reference>] [--duplicate-of <task-id>] [--expect-revision <revision>] [--dispatch-id <id> --execution-epoch <epoch>] [--delivery-commit <full-SHA>] [--disposition <card-relative-path>]
kander pick [task-id]
kander confirm <task-id> --expect-revision <revision>
kander update <task-id> --document <relative-path> --file <UTF8-input> --expect-revision <revision> [--contract-decision-file <UTF8-decision>] [--dispatch-id <id> --execution-epoch <epoch>]
kander start [--agent <configured-agent>] [--launcher <launcher>] [task-id]
kander start --host <host> [--agent <configured-agent>] [--launcher <launcher>] [--timeout SECONDS] <task-id>
kander orchestrate [--agent <configured-agent>] [--launcher <launcher>] (--message TEXT | --message-file FILE) <task-id|task-group-id>...
kander resume [--agent <configured-agent>] [--decision <reference>] [--timeout SECONDS] (--message TEXT | --message-file FILE) [--launcher <launcher>] [--dispatch-id <id>] [--kind fix|sync|wrap-up] [--base <full-SHA>] [--evidence-file <JSON>] <task-id>
kander resume --adopt --decision <reference> <task-id>
kander notify [--pane PANE] [--timeout SECONDS] (--message TEXT | --message-file FILE) [--dispatch-id <id>] [--kind fix|sync|wrap-up] [--base <full-SHA>] [--evidence-file <JSON>] <task-id>
kander dispatch prepare <absolute-UTF8-intent.json>
kander dispatch authorize-wrap-up <absolute-UTF8-request.json>
kander dispatch show <task-id> <dispatch-id>
kander dispatch fail|cancel <task-id> <dispatch-id> <dispatch-revision> <reason> [--decision <reference>]
kander dispatch settle <task-id> <dispatch-id> <dispatch-revision>
kander dismiss [--timeout SECONDS] <task-id>
kander check [--all] [task-id ...]
kander check delivery --base <ref> [--commit <ref>]
kander check overlap --source <ref> [--head <ref>]
kander guard-write <path>
kander subscribe [--follow] [--refresh SECONDS] [--heartbeat SECONDS] (<task-group> <task-id>... [--watch <task-id|task-group-id>]... | --watch <task-id|task-group-id> [--watch <task-id|task-group-id>]...)
kander coordinator show <group-id>
kander coordinator claim <absolute-claim.json>
kander coordinator reconcile <absolute-observations.json>
kander coordinator recover <absolute-request.json>
kander coordinator remove-member <absolute-request.json>
kander review [agent] [--task <id>]... [--run-id <id>] [--batch-id <id>] [--previous-run-id <id>] [--requirements-file <JSON>] [--requirement-decision <ROLE>=<decision-id>]... [--advance-file <JSON>] <CWD> <base-commit> <commit> <role> <task-goal|absolute-spec-path> [review-context] [reviewed-commit]
kander review plan <absolute-CWD> <absolute-plan.json>
kander review extend-plan <absolute-CWD> <absolute-extension.json>
kander review assign <absolute-CWD> <absolute-assignment.json>
kander review disposition <absolute-CWD> <absolute-author-record.json> <expected-card-revision>
kander review interpret <absolute-CWD> <absolute-interpretation.json>
kander review map-legacy <absolute-CWD> <absolute-map.json>
kander review abandon <absolute-CWD> <absolute-abandon.json>
kander review aggregate <absolute-CWD> <batch-id>
kander review advance <absolute-CWD> <absolute-advance-request.json>
kander review close <absolute-CWD> <absolute-close-request.json>
kander review progress <absolute-CWD> <task-id>
kander review publication <absolute-CWD> <publication-id>
kander review <plan|extend-plan|assign|disposition|interpret|advance|close|abandon> --schema
kander issue repo [--repo HOST/OWNER/REPO]
kander issue list [--repo HOST/OWNER/REPO] [--state open|closed|all] [--label NAME] [--search TEXT] [--limit N]
kander issue show NUMBER [--repo HOST/OWNER/REPO] [--comments]
kander issue import NUMBER [--repo HOST/OWNER/REPO] [--comments] [--type feature|bug|chore|research] [--large] [--language LANG]
kander issue triage NUMBER [--card TASK_ID] [--repo HOST/OWNER/REPO] [--agent NAME] [--launcher NAME]
kander issue result NUMBER --card TASK_ID [--repo HOST/OWNER/REPO] [--agent NAME] [--launcher NAME] [--action start|inspect|apply|decide] [--file JSON]
```
