# Configuration written by a newer version

A user can move back to an older Kander for a while after a newer one has written the configuration. The configuration files must stay readable then, and the older version must not erase what it does not know when it saves. This page defines how the validator treats such content. It covers the scope `config.json` (or the file `KANDER_CONFIG` names) and the project overlay `.kander-config.json`.

## What Is Ignored

- **An unknown key**, at any object level: the top level, the sections (`models`, `tui`, `rules`, `git`, `issue`, `reviewers`, `review_stages`, `kanban_agents`), an agent or a role inside `models.*`, and a field of an `agents.<name>` definition, nested objects included. The key has no effect: it never reaches an argument list, an environment variable, or a path. `schema_version` and `welcome_complete` keep their rules, and the overlay keys it forbids (`overlayForbiddenKeys`) still fail the load.
- **A `models.*` entry for an agent this version does not know**, for example `models.kanban.future-agent` that a newer release writes for its new built-in agent.
- **An `agents.<name>` entry this version cannot turn into a usable agent.** Only that agent becomes unavailable; the rest of the configuration applies. The entry is unusable when it
  - uses `install_rules` or `config_dir` on a custom name (both exist only for built-in names),
  - names a `dialect` this version does not know,
  - has neither `dialect` nor `args` on a custom name, or
  - declares a `session.mode` this version does not register (`hook:<name>` or any other value).

  An unusable entry also takes a built-in namesake out of service, so the embedded definition never runs in place of what the user declared.

Legacy keys this version still folds (`PM`, `QA`, `CSA`, `Hacker`, the flat `reviewers` and `review_stages` forms, `models.kanban.<agent>.model`) are known keys. They are rewritten or removed on save as before and never appear in the ignored list.

## What Is Downgraded

`kanban_agent`, `kanban_agents.<scale>`, `chat_agent`, and `reviewers.<scale>.<role>` accept any well-formed agent name (`ValidAgentName`). A name this version does not know loads and stays as written. The role fails only when it is used: `start`, `resume`, `orchestrate`, Chat, and `review` report that the agent is not recognized by this version, or, for an unusable entry, the agent name and the reason (including the `session.mode`). A malformed name still fails the load.

## What Stays Strict

- The embedded agent definitions, the user terminal definitions, the board and card data, and review results reject unknown fields as before.
- An invalid value of a known field (wrong type, bad format, out of range) fails the load as before. This includes every other enumerated value: `launcher`, `tui.theme*`, `git.branch_mode`, `issue.result_on_done`, `review_stages` values, `language`, and `agent_language`.
- A misspelled key is now ignored instead of rejected. It shows up only in the warnings below. This is the accepted cost of forward compatibility.

## Where It Is Reported

- `kander doctor` lists each ignored key with its JSON path (and the overlay file when the key came from there), each unusable agent entry with its reason, and each role that names an unknown agent. These lines are reports, not problems to repair: they never make `doctor` unhealthy, and the repair never rewrites them.
- `kander config` and `kander config --json` print the same notices on stderr. Stdout is what the known settings alone produce.

## Saving

`internal/config` keeps what it ignored (`Config.Ignored`, `Config.UnusableAgents`; neither is serialized from the struct) and writes it back on every write of the scope file:

- `Save`, `SaveIfUnchanged`, `Update`, `UpdateAt`, `SetLanguageIfPresent`, and `Repair` read the file inside the write lock, find the ignored items in that content, and place each one back at its JSON path in the new document, after the keys Kander writes. The base is the scope file only, so overlay content never enters `config.json`.
- An ignored item is placed only where its parent object exists in the new document, so a section the user removed does not return. Parents that exist only for the kept content are created: the `agents` object, an `agents.<name>` entry, and a section of an entry that the encoding left out (because it was cleared, or held only unknown keys), and the legacy role object of `models.review_roles` that holds only unknown fields. When the new document already holds the key, the current value wins.
- A legacy review role key (`PM`, `QA`, `CSA`, `Hacker`) is folded into its current role as before. What remains under the legacy key is only the fields this version does not know, which stay at their own path; the current role keeps its own unknown fields.
- `Repair` repairs the known settings only. It keeps unknown keys, role references to unknown agents, and unusable agent entries, and a valid file that holds only such content is not rewritten.
- The overlay write is sparse and works on the overlay's own map, which now keeps unknown keys. Options edits the keys it owns and leaves the others in place.
- In the Options panel a role that names an unknown agent shows that name as its current value, and a `kanban_agent` that names an unknown agent is shown as a note on the execution page; saving without editing that field keeps it. Editing the large scale rewrites the legacy `kanban_agent` mirror only while it names a known agent equal to the replaced value.
- The `install_rules` switch of an unusable built-in entry stays in force and can be edited; the edit is patched into the otherwise untouched entry. An edit conflict check covers it, so a stale Options snapshot is refused instead of restoring a revoked switch.

## Limits

Releases up to 1.5.0 validate strictly. The rollback target must be a release that contains this behavior; the change does not alter versions already published.
