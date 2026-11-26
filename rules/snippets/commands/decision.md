# kander decision

## Purpose and users

Records or lists a card's decisions and changes who may answer for the card. The service distinguishes a person's decision from a Leader answering under user authorization.

## Syntax

```text
kander decision record <task-id> --kind <human|leader> --file <JSON>
kander decision list <task-id>
kander decision mode <task-id> <leader|assignee>
```

## Arguments

- `task-id`: the card.
- `--kind`: `human` for a decision by the recorded person, or `leader` for a decision under a recorded user authorization.
- `--file`: a path to one UTF-8 JSON object, at most 1 MiB. The object contains `question`, `options` (an array of objects with `key` and `label`, or an empty array for free text), `answer` (the selected key or the free text), and `channel` (`web`, `cli`, `pane`, or `notify`). `recorded_by` defaults to the Git `user.name` when empty or omitted. The embedding host's attribution takes precedence. For `leader` also include `authorization` with `granted_by`, verbatim `wording`, and `date` in YYYY-MM-DD.
- `leader`, `assignee`: the decision mode. `leader` allows authorized delegation and is the default when the header is missing. `assignee` reserves the decision for the assigned person.

## Examples

```text
# Record a delegated answer using a JSON file
kander decision record 20261118-example-task --kind leader --file /tmp/decision.json

# Print complete decision records
kander decision list 20261118-example-task --json

# Reserve decisions for the assignee
kander decision mode 20261118-example-task assignee
```

## Preconditions

- The project is bound to a server. Read this command contract before sending or recording a delegated answer.
- Writes need a card outside `archived` and `trash`. These operations do not require a claim. The embedding host can deny any of them through the matching authorization route.
- A Leader or an orchestration session runs `mode` only when the user's explicit instruction names the card and the target mode. Delegated authorization, unattended mode, and recommended options never authorize a mode change.

## Effects

- `record` adds one immutable record under `decisions/` and a line in the managed `DECISIONS` section. `mode` changes the managed `DECISION_MODE` header without changing the contract. Each write advances the card revision and emits a `task_changed` event.
- `list` reads decisions in publication order, including after archive. It does not modify the card.

## Output

- Without `--json`, `record` prints the new record ID, `mode` prints the new card revision, and `list` prints one row per record with ID, kind, UTC time, recorder, question, and answer.
- With `--json`, `record` prints the full record, `mode` prints `task_id`, `mode`, and `revision`, and `list` prints a `decisions` array of full records.

## Rejections

- The service refuses `leader` records on cards in `assignee` mode with `decision_reserved`.
- An attributed write requires a recorder. If a configured hook cannot identify the recorder, the write fails with `attribution_required` and Kander does not use the client value.
- Invalid or missing fields are rejected. Question and answer have a 4096-byte limit each. Each of at most 32 options has a unique key of at most 128 bytes and a label of at most 512 bytes. Authorization wording has an 8192-byte limit. The recorder and grantor are single-line identities limited to 256 UTF-8 bytes after trimming for validation. A human decision must not have an authorization object.
