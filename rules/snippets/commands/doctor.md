# kander doctor

Source: new in this file; the rule files did not describe this command beyond its syntax line

## Purpose and users

Checks the installation (binary, rules root, agent rules integration, terminal tools) and repairs what it can: missing or outdated rule files, templates, and snippets are rewritten from the embedded copies; locally modified files are listed and, in an interactive session, backed up and replaced on request. Run by the user, also after every upgrade.

## Syntax

```text
kander doctor
```

## Arguments

None.

## Preconditions

- An installed Kander (global or project scope).

## Effects

- Rewrites missing and outdated files below the rules root and stamps `kander-rules-state.json`.
- Writes the missing Kander references into the covered agents' rules files and installs or updates their rules extensions, except for an agent whose `install_rules` switch is off (`agents.<name>.install_rules`, else the top-level `install_rules`; read only from the scope `config.json`). For such an agent it neither reads nor writes the files and prints one "turned off" line instead of a missing-integration warning; that line does not fail the check. A shared project `AGENTS.md` stays unwritten when any agent that reads it is off.
- A reference that appears only inside an HTML comment, a code fence, or a negated sentence is reported as inactive, not as connected.
- `install_rules` keys in the project overlay are ignored with one warning each.
- Interactive sessions may back up and replace locally modified files.
- Never changes the board.

## Output

- One line per check, then the overall result.

## Rejections

- Any argument is a usage error.
