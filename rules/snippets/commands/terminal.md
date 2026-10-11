# kander terminal

Source: new in this file; the rule files did not describe this command beyond its syntax line

## Purpose and users

Lists the available terminal launchers (built-in and loaded definitions) and tests one of them. Used by the user when setting up herdr, tmux, or luvus.

## Syntax

```text
kander terminal list
kander terminal test <name> [--keep] [--skip-focus]
```

## Arguments

- `list`: print every launcher and whether its environment holds.
- `test <name> [--keep] [--skip-focus]`: open a container through that launcher and verify it; `--keep` leaves the container open, `--skip-focus` skips the focus check.

## Preconditions

- `test` needs the launcher's environment (see `snippets/commands/start.md` "Launchers").

## Effects

- `test` creates and, without `--keep`, closes one container. No board access.

## Output

- Human-readable lines.

## Rejections

- A missing subcommand, a missing name after `test`, or an unknown option is a usage error.
