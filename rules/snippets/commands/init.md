# kander init

Source: moved from `KANDER-KANBAN-RULES.md` "Command Contract" ("Initializing the Board")

## Purpose and users

Binds a checkout to a service project. Used by the user when configuring a client. Normal clients never open local board storage.

## Syntax

```text
kander init [--maintenance] [project-path]
```

This historical syntax remains indexed for compatibility. `--maintenance` is rejected with the service operator's maintenance direction; it never changes a local board.

## Arguments

- `project-path`: checkout whose project binding to initialize; default is the current checkout.
- `--maintenance`: unsupported client maintenance request. Use the offline server command below.

## Preconditions

- A readable scope configuration and reachable compatible service.
- A service URL and project name, from configuration or the additional flags below.

## Effects

- Registers or reads the service project, verifies its canonical Git origin, and sparsely saves explicitly requested binding fields.
- Never creates or recovers local `kanban/`, changes Git excludes, migrates cards, or prunes evidence. `KANBAN_DIR` does not select a client board.

## Output

- The service project binding.

## Rejections

- More than one path, an unknown option, invalid configuration, incompatible protocol, unreachable service, or origin mismatch refuses binding.
- A concurrent project overlay change refuses the save.

## Offline service maintenance

Stop `kander-server` and pause every executor and external writer. Run `kander-server maintain [--data DIR] --project NAME`. If active cards require form migration, add `--maintenance` only after all writers are paused. The command takes the exclusive server data-directory lock and never starts an HTTP listener or stops agents. It converts imported legacy file cards using the existing board redo journal. A repeat reports zero changes. Original import snapshots and evidence bytes stay intact. Keep writers paused after failure and retry the same maintenance command; never choose or delete a partial copy manually.

## Additional Syntax

```text
kander init [--server <URL>] [--project <name>] [project-path]
```

The optional URL and name are validated with the normal configuration schema. The client checks protocol compatibility, registers the project with its canonical Git origin and saves only the requested binding fields in the project's sparse `.kander-config.json`. A concurrent overlay edit refuses the save. Repeating the binding returns the same project. Different checkout paths do not affect project identity. A name already bound to another origin is refused.

Without these flags, `init` uses the existing binding at the selected project path. `--maintenance` does not open a local board: run explicit import or service maintenance through `kander-server`. Unconfigured or unreachable service commands fail; they never fall back to local storage.
