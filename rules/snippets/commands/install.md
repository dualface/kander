# kander install

Source: new in this file; the rule files did not describe this command beyond its syntax line

## Purpose and users

Runs the interactive installer: copies the binary, writes the rules root (rule files, templates, snippets), and connects the agent rules files. Used by the user.

## Syntax

```text
kander install
```

## Arguments

None.

## Examples

```text
# Run the interactive installer
kander install
```

## Preconditions

- An interactive terminal.

## Effects

- After the language and scope, lists every agent file the install would write, one line per rules file, existing `AGENTS.override.md`, rules extension, and bundled leader skill documents, as they will be after the legacy-root migration, each marked create, append, rewrite, replace, update, or unchanged. The agents are then selected per rules file; an agent starts selected when its `install_rules` switch is on, so a fresh install selects all. Agents that share one rules file (a project `AGENTS.md`, or a rules file that is a symlink to another agent's) are selected together.
- Cancelling the confirmation writes nothing: no binary, rules root, reference, extension, skill, or `config.json`.
- Unselected agents are recorded as `agents.<name>.install_rules: false` in the scope `config.json` before any rules file is written; a selected agent whose switch was off is recorded as `true`. A missing `config.json` is created for this with `welcome_complete` false.
- Writes below the install root and the rules root; preserves existing agent rules entries. An agent whose switch is off gets no rules file, override, extension, or skill write; the rules root is written either way.
- Installs the bundled `kander-leader` skill under the selected Codex and Claude skill directories in the chosen scope. A pre-existing unmanaged directory or link stays in place. Reinstallation preserves locally edited documents. Later, `kander doctor` and the first bare `kander` start after an upgrade install a missing skill and update a managed one the same way.

## Output

- Interactive prompts and a final summary. `--json` is refused with `interactive_required` and exit status 2 before anything is written.

## Rejections

- Any argument is a usage error.
