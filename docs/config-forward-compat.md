# Configuration written by a newer version

A user can move back to an older Kander for a while after a newer one has written the configuration. The configuration files must stay readable then, and the older version must not erase what it does not know when it saves. This page defines how the validator treats such content. It covers the scope `config.json` (or the file `KANDER_CONFIG` names) and the project overlay `.kander-config.json`.

## What Is Ignored

- **An unknown key**, at any object level: the top level, the sections (`models`, `tui`, `rules`, `git`, `issue`, `server`, `project`, `reviewers`, `review_stages`, `kanban_agents`), an agent or a role inside `models.*`, and a field of an `agents.<name>` definition, nested objects included. The key has no effect: it never reaches an argument list, an environment variable, or a path. `schema_version` and `welcome_complete` keep their rules, and the overlay keys it forbids (`overlayForbiddenKeys`) still fail the load.
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
- An invalid value of a known field (wrong type, bad format, out of range) fails the load as before, also when the entry it sits in is unusable for another reason: the `allocate` command and the `discovery` block of a session mode this version registers are checked before any reason, so an unknown `dialect` does not hide them. This includes every other enumerated value: `launcher`, `tui.theme*`, `git.branch_mode`, `issue.result_on_done`, `review_stages` values, `language`, and `agent_language`.
- A misspelled key is now ignored instead of rejected. It shows up only in the warnings below. This is the accepted cost of forward compatibility.

## Where It Is Reported

- `kander doctor` lists each ignored key with its JSON path (and the overlay file when the key came from there), each unusable agent entry with its reason, and each role that names an unknown agent. These lines are reports, not problems to repair: they never make `doctor` unhealthy, and the repair never rewrites them.
- `kander config` prints the same notices on stderr; with `--json` they are the `notices` of the result object and stderr stays empty (see "Version 2" below). Stdout is what the known settings alone produce.

## Saving

`internal/config` keeps what it ignored (`Config.Ignored`, `Config.UnusableAgents`; neither is serialized from the struct) and writes it back on every write of the scope file:

- `Save`, `SaveIfUnchanged`, `Update`, `UpdateAt`, `SetLanguageIfPresent`, and `Repair` read the file inside the write lock, find the ignored items in that content, and place each one back at its JSON path in the new document, after the keys Kander writes. The base is the scope file only, so overlay content never enters `config.json`.
- An ignored item is placed only where its parent object exists in the new document, so a section the user removed does not return. Parents that exist only for the kept content are created: the `agents` object, an `agents.<name>` entry, and a section of an entry that the encoding left out (because it was cleared, or held only unknown keys), the `server` and `project` sections (the encoding omits them while their known keys are empty), and the legacy role object of `models.review_roles` that holds only unknown fields. When the new document already holds the key, the current value wins.
- A legacy review role key (`PM`, `QA`, `CSA`, `Hacker`) is folded into its current role as before. What remains under the legacy key is only the fields this version does not know, which stay at their own path; the current role keeps its own unknown fields.
- `Repair` repairs the known settings only. It keeps unknown keys, role references to unknown agents, and unusable agent entries, and a valid file that holds only such content is not rewritten.
- The overlay write is sparse and works on the overlay's own map, which now keeps unknown keys. Options edits the keys it owns and leaves the others in place.
- In the Options panel a role that names an unknown agent shows that name as its current value, and a `kanban_agent` that names an unknown agent is shown as a note on the execution page; saving without editing that field keeps it, also before the first-run welcome completes (the preparation that picks installed agents for missing roles leaves such a role alone). After a save of the Global tab the Project tab works from the document that was written, so restoring a page keeps the overlay's models of a built-in agent the scope made unusable. Editing the large scale rewrites the legacy `kanban_agent` mirror only while it names a known agent equal to the replaced value.
- The `install_rules` switch of an unusable built-in entry stays in force and can be edited; the edit is patched into the otherwise untouched entry. An edit conflict check covers it, so a stale Options snapshot is refused instead of restoring a revoked switch.

## Limits

Releases up to 1.5.0 validate strictly. The rollback target must be a release that contains this behavior; the change does not alter versions already published.

## Version 2

The v2 branch adds the `server` and `project` sections and the client-server runtime. The rules above apply to them as follows.

### The `server` and `project` sections

- An unknown key inside `server` (known: `url`) or `project` (known: `name`) is ignored, reported by its JSON path (`server.token`), and kept on save, in the scope `config.json` and in the project overlay. A section that holds nothing but unknown keys loads like an absent section and is written back with its keys.
- The known values stay strict: a `server.url` that is not an `http` or `https` URL, and a `project.name` that breaks the project name rule, fail the load as before.
- `Repair` keeps a valid `server.url` and `project.name` of the scope `config.json` and drops only the one value that is invalid, so `kander doctor` never removes a binding written there.
- `kander init` writes `server.url` and `project.name` into the project overlay through the same sparse write as Options, so the unknown keys of the overlay stay in the file.
- The `overlayAllowedKeys` list of v2 is gone: an unknown overlay key no longer fails the merge (`config.overlay_has_unknown_keys`), as in version 1. The overlay keys it forbids still fail.

### Notices with `--json`

`kander config --json` puts what it ignores into the `notices` of its result object, one notice per item, and writes nothing to stderr (see [JSON output](json-output.md)):

| Code | Item |
| --- | --- |
| `config_ignored` | each ignored item that is not an unusable agent: an unknown key, or a `models.*` entry for an agent this version does not know |
| `agent_unusable` | each unusable `agents.<name>` entry, and each role (`kanban_agent`, `kanban_agents.<scale>`, `chat_agent`, `reviewers.<scale>.<role>`) that names an agent this version does not know |

The `agent_warning` notice of an agent whose session mode is `none` is unchanged and comes from the shown configuration. The notices name the role and the agent, and the files keep what they hold. `kander doctor --json` lists the same items in `lines` (`info` for an ignored key, `warn` for an unusable entry and for an unknown role reference), and they never set `healthy` to false.

Apart from the `notices` field, the object equals the one of the **reference configuration** of the same layers, and the text output equals the text output of it. The notices come from the configuration as `Load` returns it (`Config.Ignored`, `UnknownAgentRefs`), the output from the reference configuration below.

### The reference configuration

`kander config` reads the scope file and the project overlay (the two *layers*) once (`config.LoadWithDisplay`). The loaded configuration is what `Load` returns from that read; the shown one is the *reference configuration*: the result of the same loader (`validateLayers`, the merge, the defaults and the ignored-key rules shared with `Load` and the Options preview) on the same layers, with the content this version ignores taken out. Nothing is read again: the same read gives the saved language of the output, and the display code writes nothing. The read is the one of `Load`, so it keeps `Load`'s behavior of removing a `.kander-config.json` that holds no overrides.

What "taken out" means is fixed by four rules, each applied to every layer on its own. The loader already ignores unknown keys, so no other key is removed; an unknown key that stays in a layer has no effect on the result.

1. **An unusable `agents.<name>` entry** is absent from every layer that holds it, because it can turn unusable only when the layers merge. The built-in agent it took out of service is available again, and its `models.*` entries are settings again.
2. **A role key that names an agent this version does not know** is absent from the layer that holds it, so the other layer or the default decides the role. This covers `kanban_agents.<scale>`, `chat_agent`, the overlay's `kanban_agent`, and every `reviewers` role, a legacy one under its own name (`PM`, `QA`, `CSA`, `Hacker`) before the legacy keys fold into `PMQA` and `Security`; a flat legacy `reviewers` object is read as the two-scale object the merge reads. The agent is unknown after rule 1: the entries that remain define the known agents. The scope file requires `kanban_agent`, so there the default agent stands in for an unknown one.
3. **The unknown keys of an `agents.<name>` entry** are taken out of the entry of the layer, with the loader's own pruning of agent definitions. A section of the entry that held only unknown keys (a `program_status` of a newer version) is absent from the entry, and an entry that was not empty and has nothing left is absent from the layer. A section or an entry that the layer wrote as `{}` is a declaration and stays.
4. **A section the loader requires keys of** is absent from both layers when neither layer holds one of its keys. These are `tui` and every struct section of an agents entry (`args`, `session`, `session.file`, `session.discovery`, `review`) except the two the loader accepts as `{}`: `program_status` and `prompt_delivery`.

A layer is touched only where it holds the removed key; the other layer keeps its own values. Each rule runs once, in this order, with one validation between rules 1 and 2. The display code has no retry, no trial validation and no repetition until nothing is left.

What the rules give for the cases that need a decision:

- **(a) Removing leaves an object empty.** The object stays as `{}` where the loader accepts it empty: `kanban_agents`, `agents`, `reviewers` (the scope file requires it) and `reviewers.<scale>`. Example: the scope has `reviewers: {"PM": "futureagent"}`; the reference has `reviewers: {}` and every role takes the default agent.
- **(b) An object holds only unknown content.** The loader's own result stands. `tui` becomes the default section (rule 4), `rules` with only an unknown module has every module off like `rules: {}`, `server` and `project` are zero values and are omitted, `reviewers.large` with only an unknown role has the default roles, and the `program_status` of an agents entry is absent (rule 3). Example: the scope has `tui: {"future": 1}`; the output shows the default `tui`.
- **(c) One layer declares an empty object, another holds unknown content at the same path.** The declaration stays when the loader accepts the object empty, and the unknown content is taken out of its own layer. Example: the scope has `agents.codex: {}` and the overlay has `agents.codex: {"future_key": 1}`; the output shows `"codex":{}`. Another example: the scope declares `program_status: {}` and the overlay has an unknown key in it; the declaration stays. When the loader requires keys of the section (rule 4), nothing stays: the scope has `tui: {"future": 1}` and the overlay has `tui: {}`; neither layer has a `tui` in the reference, and the output shows the default.
- **(d) A built-in agent entry of a layer holds only unknown keys.** The entry is absent from that layer, and the same name in another layer is untouched (rule 3). Example: the scope has `agents: {"codex": {"future_key": 1}}` and nothing else; the output has no `codex` entry. An entry that also holds `program_status: {}` is not empty and shows that declaration.
- **(e) The scope file's required `kanban_agent` names an unknown agent.** The default agent stands in for it (rule 2). The overlay's key is absent instead, so the scope's value shows. Example: the scope has `kanban_agent: "futureagent"` and no `kanban_agents`; the output shows the default agent (`codex`) for `kanban_agent` and both scales. With the overlay's `kanban_agent: "futureagent"` instead, the output shows the scope's `kanban_agent`.
- **(f) A legacy reviewer role names an unknown agent.** The key is removed under its own name, and the remaining legacy keys fold as usual. Example: the scope has `reviewers: {"PM": "futureagent", "QA": "codex"}`; the reference has `reviewers: {"QA": "codex"}`, so `PMQA` is `codex`.

The reference configuration has no result when the restored settings do not load: an unusable entry of a built-in agent hid `models.*` values that this version cannot read, and the agent comes back with them. The layers load, so `kander config` shows the loaded configuration (the notices still name the ignored items) instead of failing; the generated test reaches no such combination.

The property is tested on generated combinations (`TestDisplayEqualsTheReferenceConfiguration`): every kind of newer content (the keys of every known section, the `server` and `project` keys, the `models.*` entries of unknown agents, unusable `agents` entries, every role reference, sections of only unknown keys, both forms of `program_status`, the sections of a custom agent, and a built-in entry of only unknown keys) in the scope file, in the overlay and in both; every pair of them; every state of the object that holds the content (absent, empty, only another unknown key, known settings) in the other layer next to the two states it can have in its own; and the named inputs of the review findings. The reference documents of each combination are written by the test's tables from these rules, never by the display code. The combinations whose layers the loader itself rejects (a legacy flat `reviewers` object next to a scale object in one layer, an empty or partial `tui` or agent section that no layer completes) are named by predicates and counted, never skipped.

### Startup logs of the server and the worker

A server or worker runs for hours and would otherwise drop the settings of a newer version without a trace. Each of them writes one `WARN` log line per ignored item (`Config.Ignored`, which includes unusable agents entries) when it starts, with the fields `path` and `kind` and never the value, which may be a sensitive setting of a newer version. Unknown agent references are not in that list.

- **Server.** `kander server start --foreground` writes the lines after its logger exists and before it serves. `kander server start` without `--foreground` runs that path in its child process, so the lines are in `server.log` once and the parent writes none. With `--json` the lines of this phase are `server_log` notices, as every other line before the server can serve.
- **Worker.** `kander worker` reads the scope configuration once at startup and never a project overlay: it runs in the client home, which can sit below a repository or a directory that holds one, and an overlay that does not load must not hide the ignored settings of the scope. A configuration that cannot be loaded is logged as one warning and the worker starts anyway.

### Deployment

Releases of version 1 before 1.5.1 are strict. A v1.5.0 or older client rejects a `.kander-config.json` that holds `server` or `project` (`config.overlay_has_unknown_keys`), and saving its `config.json` removes the two sections. Every version 1 client that reads or writes the configuration a v2 client uses must be v1.5.1 or newer.
