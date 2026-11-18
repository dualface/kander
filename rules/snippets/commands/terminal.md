# kander terminal

Source: new in this file; the rule files did not describe this command beyond its syntax line

## Purpose and users

Lists the available terminal launchers (built-in and loaded definitions) and tests one of them. Used by the user when setting up herdr, tmux, luvus, or dsh.

## Syntax

```text
kander terminal list
kander terminal test <name> [--keep] [--skip-focus]
```

## Arguments

- `list`: print every launcher and whether its environment holds.
- `test <name> [--keep] [--skip-focus]`: open a container through that launcher and verify it; `--keep` leaves the container open, `--skip-focus` skips the focus check.

## Examples

```text
# List the launchers and their environment
kander terminal list

# Test a launcher and keep its container
kander terminal test tmux --keep
```

## Preconditions

- `test` needs the launcher's environment (see `snippets/commands/start.md` "Launchers").

## Effects

- `test` creates and, without `--keep`, closes one container. No board access.

## Output

- Human-readable lines. With `--json`: `list` is `definitions`, an array of `status` (`pass`, `fail`, `skip`), `source`, `name`, `path`, `detail`, `active`, and `invalid`; the exit status is 1 when a definition is invalid. `test` is `launcher`, `passed`, `error` (when a step failed), `steps` (array of `name`, `status` `pass`, `skip`, or `fail`, and `detail`), and `notes` (the other report lines); a failed run is the result with `passed` false and exit status 1. An unknown launcher is the error object.

## Rejections

- A missing subcommand, a missing name after `test`, or an unknown option is a usage error.
