# kander init

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Initializing the Board")

## Purpose and users

Creates the board and recovers interrupted transactions. Used by the user, and by an agent only when a rule file names `kander init` as the recovery step.

## Syntax

```text
kander init [--maintenance] [project-path]
```

## Arguments

- `project-path`: the project whose board to initialize; default is the current directory's project.
- `--maintenance`: acknowledge the maintenance preconditions of `KANDER-KANBAN-RULES.md` "Explicit Migration and Maintenance" for migration and recovery.

## Preconditions

- Migration and recovery require every writer paused per "Explicit Migration and Maintenance".

## Effects

- `init` idempotently creates the board and the 7 state directories (`backlog`, `todo`, `working`, `review`, `done`, `archived`, `trash`); rerunning only creates missing directories, and Git projects only update the local `info/exclude`.
- Migrates legacy `<task-id>.md` files and replays prepared transactions per "Explicit Migration and Maintenance"; cleans archived-only data left by earlier releases.

## Output

- What was created, migrated, or recovered.

## Rejections

- When `working/` or `review/` cards exist and migration is needed, `init` refuses by default and lists the affected IDs; only `--maintenance` proceeds.
- More than one path argument or an unknown option is a usage error.

## Distributed client

Normal client execution always uses `/api/v1`. `init` registers or reads a service project and never creates local board state. An existing scope configuration and an explicit service binding are required. `KANBAN_DIR` does not select a client board.

## Additional Syntax

```text
kander init [--server <URL>] [--project <name>] [project-path]
```

The optional URL and name are validated with the normal configuration schema. The client checks protocol compatibility, registers the project with its canonical Git origin and saves only the requested binding fields in the project's sparse `.kander-config.json`. A concurrent overlay edit refuses the save. Repeating the binding returns the same project. Different checkout paths do not affect project identity. A name already bound to another origin is refused.

Without these flags, `init` uses the existing binding at the selected project path. `--maintenance` does not open a local board: run explicit import or service maintenance through `kander-server`. Unconfigured or unreachable service commands fail; they never fall back to local storage.
