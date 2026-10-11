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
- Interactive sessions may back up and replace locally modified files.
- Never changes the board.

## Output

- One line per check, then the overall result.

## Rejections

- Any argument is a usage error.
