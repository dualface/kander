# kander uninstall

Source: new in this file

## Purpose and users

Removes what Kander wrote for the agents. That is the Kander load commands earlier releases wrote into the agents' global rules files, the agent rules extensions, and the rule files and state files of the rules root. Then turns rules injection off, so a later `kander doctor` or bare `kander` start writes no agent file again. Used by the user.

## Syntax

```text
kander uninstall [--yes]
```

## Arguments

- `--yes`: skip the confirmation. Required when standard input or standard error is not a terminal.

## Examples

```text
# Remove what Kander wrote for the agents
kander uninstall --yes
```

## Preconditions

- An installed Kander.
- A readable, valid scope `config.json`, or none.

## Effects

- Lists every object first: the rules files it edits or deletes, the extensions it deletes, the number of rule files it deletes from the rules root, and every path it keeps with the reason. On a terminal it then asks y/N (default N); any answer other than `y` changes nothing.
- Writes `install_rules: false` into the scope `config.json` and drops every `agents.<name>.install_rules` before any other change; a missing `config.json` is created from the defaults with `welcome_complete` false.
- Drops every Kander load command from each embedded agent's existing global rules file and `AGENTS.override.md` sibling, whatever the switch value and whatever entry the command names: the same cleanup every other command runs once (`snippets/commands/install.md`). Other bytes stay, including comment text on the same line as a load command. A rules file left with only blank lines, or a `.mdc` rule left with only the `alwaysApply` frontmatter Kander wrote, is deleted; a rules file that is a symlink to the rules entry is removed as a link; `AGENTS.override.md`, other symlinked rules files, and files whose directory is reached through a symlink are only edited. The project references `kander init` wrote into repositories stay.
- Deletes each agent rules extension whose digest is a released version; a locally modified copy stays and is reported.
- Deletes the rule files and state files Kander installed below the rules root, including the global cleanup marker `kander-global-cleanup.json`, then the directories left empty. Any other file stays and is reported. Uninstall never writes the marker.
- Never follows a symlink, junction, or other reparse point to delete a file.
- Keeps the binary, `config.json`, the share directory, caches, and the board.
- Running `kander` or `kander doctor` afterwards extracts the rules root again but writes no agent file until `install_rules` is turned back on.
- Removes the unmodified managed leader skill files. A pre-existing skill, user link, or locally modified bundle stays in place.

## Output

- The plan on standard output, the y/N prompt on standard error, then one line per change.
- Exit 0 on success, 1 when cancelled or when a change failed, 2 for a usage error or a missing `--yes` without a terminal.
- With `--json` and `--yes`: `mode` (always `global`), `config_file`, `rules_dir`, and `items` (array of `action` (`edit`, `remove`, `keep`), `path`, and `reason`). `--json` without `--yes` is refused with `interactive_required` and exit status 2 before anything changes.

## Rejections

- Any argument other than `--yes` is a usage error.
