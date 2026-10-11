# kander guard-write

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract"

## Purpose and users

An advisory pre-write check that agent hooks call before writing a path; agents edit cards through `update`, never through this command.

## Syntax

```text
kander guard-write <path>
```

## Arguments

- `path`: the path about to be written.

## Preconditions

- None; a located board is used when present.

## Effects

- `kander guard-write` is an advisory pre-write check (exit 0 allows, non-zero rejects) that is not atomic with an external write and covers no arbitrary shell command. It recognizes stale state paths and the old `.md` spelling but makes no external write atomic.

## Output

- Exit 0 to allow, non-zero with a reason to reject.

## Rejections

- A missing path or an unknown option is a usage error.
