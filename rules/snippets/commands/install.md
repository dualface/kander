# kander install

Source: new in this file; the rule files did not describe this command beyond its syntax line

## Purpose and users

Runs the interactive installer: copies the binary, writes the rules root (rule files, templates, snippets), and installs the agent extensions and the `kander-leader` skill. Kander is installed globally only, and the installer writes no agent rules file: `kander init` writes the project reference that makes a project load the rules. Used by the user.

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

- After the language, lists every agent file the install would write, one line per rules extension and bundled leader skill document, each marked create, update, or unchanged. The agents are then selected; an agent starts selected when its `install_rules` switch is on, so a fresh install selects all.
- Cancelling the confirmation writes nothing: no binary, rules root, extension, skill, or `config.json`.
- Unselected agents are recorded as `agents.<name>.install_rules: false` in the scope `config.json` before anything is written; a selected agent whose switch was off is recorded as `true`. A missing `config.json` is created for this with `welcome_complete` false. The switch also decides the project rules file `kander init` writes (`snippets/commands/init.md`).
- Writes below the global bin directory and the rules root. An agent whose switch is off gets no extension or skill write; the rules root is written either way.
- Removes what earlier releases wrote into the agents' global rules files (the global injection), as `kander doctor` and the first run of every other command do: Kander load commands and reference blocks, a rules file that is a symlink to the rules entry, and a `~/.cursor/rules/kander.mdc` Kander created and nobody changed. Other bytes stay; a symlinked rules file is edited in its real file. The summary names what was removed and asks for `kander init` in each project that uses Kander.
- Installs the bundled `kander-leader` skill under the selected Codex and Claude skill directories. A pre-existing unmanaged directory or link stays in place. Reinstallation preserves locally edited documents. Later, `kander doctor` and the first bare `kander` start after an upgrade install a missing skill and update a managed one the same way.

## Output

- Interactive prompts and a final summary. `--json` is refused with `interactive_required` and exit status 2 before anything is written.

## Rejections

- Any argument is a usage error.
