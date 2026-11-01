# kander uninstall

Source: new in this file

## Purpose and users

Removes what Kander wrote for the agents in the current scope: the Kander load commands in the agent rules files, the agent rules extensions, and the rule files and state files of the rules root. Then turns rules injection off, so a later `kander doctor` or bare `kander` start writes no agent file again. Used by the user.

## Syntax

```text
kander uninstall [--yes]
```

## Arguments

- `--yes`: skip the confirmation. Required when standard input or standard error is not a terminal.

## Preconditions

- An installed Kander (global or project scope); the scope is the one of the running entry.
- A readable, valid scope `config.json`, or none.

## Effects

- Lists every object first: the rules files it edits or deletes, the extensions it deletes, the number of rule files it deletes from the rules root, and every path it keeps with the reason. On a terminal it then asks y/N (default N); any answer other than `y` changes nothing.
- Writes `install_rules: false` into the scope `config.json` and drops every `agents.<name>.install_rules` before any other change; a missing `config.json` is created from the defaults with `welcome_complete` false.
- Drops every Kander load command from each embedded agent's existing rules file and `AGENTS.override.md` sibling, whatever the switch value and whatever entry the command names. Other bytes stay. A rules file left with only blank lines, or a `.mdc` rule left with only the `alwaysApply` frontmatter Kander wrote, is deleted; `AGENTS.override.md` and symlinked rules files are only edited.
- Deletes each agent rules extension whose digest is a released version; a locally modified copy stays and is reported.
- Deletes the rule files and state files Kander installed below the rules root, then the directories left empty. Any other file stays and is reported.
- Never follows a symlink, junction, or other reparse point to delete a file; a project rules file that resolves outside the main worktree is not edited and is reported.
- Keeps the binary (global or `.kander/bin/`), `config.json`, the share directory, caches, and the board.
- Running `kander` or `kander doctor` afterwards extracts the rules root again but writes no agent file until `install_rules` is turned back on.

## Output

- The plan on standard output, the y/N prompt on standard error, then one line per change.
- Exit 0 on success, 1 when cancelled or when a change failed, 2 for a usage error or a missing `--yes` without a terminal.

## Rejections

- Any argument other than `--yes` is a usage error.
