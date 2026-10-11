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

## Preconditions

- An interactive terminal.

## Effects

- Writes below the install root and the rules root; preserves existing agent rules entries.

## Output

- Interactive prompts and a final summary.

## Rejections

- Any argument is a usage error.
