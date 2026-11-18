# kander doctor

Source: new in this file; the rule files did not describe this command beyond its syntax line

## Purpose and users

Checks the installation and repairs what it can. The check covers the binary, rules root, agent rules integration, and terminal tools; missing or outdated rule files, templates, and snippets are rewritten from the embedded copies; locally modified files are listed and, in an interactive session, backed up and replaced on request. Run by the user, also after every upgrade.

## Syntax

```text
kander doctor
```

## Arguments

None.

## Examples

```text
# Check the installation and repair it
kander doctor
```

## Preconditions

- An installed Kander (global or project scope).

## Effects

- Rewrites missing and outdated files below the rules root and stamps `kander-rules-state.json`.
- Writes the missing Kander references into the covered agents' rules files and installs or updates their rules extensions, except for an agent whose `install_rules` switch is off (`agents.<name>.install_rules`, else the top-level `install_rules`; read only from the scope `config.json`). For such an agent it neither reads nor writes the files and prints one "turned off" line instead of a missing-integration warning; that line does not fail the check. A shared rules file (a project `AGENTS.md`, or one reached through a symlink to another agent's) stays unwritten when any agent that reads it is off.
- A reference that appears only inside an HTML comment, a code fence, or a negated sentence is reported as inactive, not as connected.
- `install_rules` keys in the project overlay are ignored with one warning each.
- Interactive sessions may back up and replace locally modified files.
- Never changes the board.

## Output

- One line per check, then the overall result. With `--json`: `healthy` (boolean) and `lines` (array of `level`, one of `ok`, `warn`, `info`, `note`, and `message`). `doctor --json` never asks a question. When an interactive doctor would ask something before it repairs (the backup and replacement of a modified rule file, the replacement of an unusable agent, the storing of an agent session file, the herdr install, the Homebrew upgrade, the choice of the kander executable to keep when several are on PATH), the run is refused as an interactive form (`interactive_required`, exit 2) before any repair, and nothing is repaired; run `kander doctor` in a terminal. The version guard comes first: when the running binary is older than one on PATH, doctor stops with that diagnostic as the result, never with this refusal. Otherwise it repairs and reports as usual. An unhealthy installation is the result with `healthy` false and exit status 1.

## Rejections

- Any argument is a usage error.
