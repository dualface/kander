# Durable Dispatch Protocol

The Durable Dispatch protocol guarantees at-most-once instruction delivery, deterministic acceptance tracking, and traceable completion receipts when orchestrating asynchronous coding agents.

Dispatch identity is governed by `dispatch_id`. Status is never inferred from terminal screen echo or column movements.

---

## 1. Dispatch Lifecycle & State Machine

```text
[kander notify / dispatch prepare]
               |
               v
         +-----------+
         |  prepared | (Intent persisted durably to disk)
         +-----------+
               |
               v
     +------------------+
     | delivery-unknown | (Delivery attempted over terminal)
     +------------------+
               |
      (kander move working --dispatch-id --execution-epoch)
               |
               v
         +-----------+
         |  accepted | (Executor acknowledges; 120s acceptance deadline met)
         +-----------+
               |
      (kander move review / done --delivery-commit)
               |
               v
         +-----------+
         | completed | (Atomic receipt committed with state & revision)
         +-----------+
```

1. **`prepared`**: Intent is persisted to `dispatches/<id>/intent.json`.
2. **`delivery-unknown`**: Sent to the terminal. If network or IPC fails, Kander retains the message and avoids premature failovers.
3. **`accepted`**: Generated **only** when the executor agent executes `kander move <id> working --dispatch-id <id> --execution-epoch <epoch>`.
4. **`completed`**: Generated when the executor completes work and transitions the card to `review` or `done`.

---

## 2. Dispatch Kinds & Evidence Bindings

Every dispatch must declare a `kind` and attach semantic evidence:

```sh
# Send a fix dispatch
kander notify <task-id> --kind fix --base <40-char-SHA> \
  --evidence-file <evidence.json> --message-file <UTF8-message>

# Resume an unaccepted dispatch
kander resume <task-id> --dispatch-id <id> --message-file <UTF8-message>

# Inspect dispatch status
kander dispatch show <task-id> <id>
```

| Kind | Target Transition | Required Evidence Binding |
|---|---|---|
| `fix` | `working` $\to$ `review` | Binds batch ID, finding IDs, and prior author disposition records. |
| `sync` | `working` $\to$ `review` | Used for branch/rebase synchronization. Does not accept review finding bindings. |
| `wrap-up` | `working` $\to$ `done` | Binds closed review target SHA, Git integration commit, and develop branch ref. |

---

## 3. `fix` Review Evidence Binding

Fix dispatches enforce that the executor receives exact, structured review findings:

```json
{
  "fix": {
    "batch_id": "batch-one",
    "findings": [
      {
        "run_id": "pm-round-two",
        "finding_id": "PM-02",
        "previous_run_id": "pm-round-one"
      }
    ],
    "authors": [
      {
        "finding": {"run_id": "pm-round-one", "finding_id": "PM-01"},
        "record_id": "author-one",
        "author": "codex",
        "artifact": {
          "task_id": "20260908-example-task",
          "path": "reviews/pm-round-one/dispositions/author-one.json"
        }
      }
    ]
  }
}
```

- **Lineage Verification**: The system verifies that `findings` exist in the specified batch and are assigned to the target task.
- **Idempotency**: Retrying with the same `dispatch_id` inherits the existing bindings. Modifying bindings under the same ID triggers a conflict error.

---

## 4. `wrap-up` Integration Binding & Wrap-up-on-Behalf

### Standard Wrap-up

Binds the closed review final commit with the integrated Git develop branch commit:

```json
{
  "wrap_up": {
    "git": {
      "cwd": "/absolute/worktree",
      "source_commit": "<40-character-final-reviewed-SHA>",
      "target_commit": "<40-character-integrated-develop-SHA>",
      "target_ref": "refs/remotes/origin/develop",
      "author": "coordinator",
      "basis": "user confirmed push and local sync"
    }
  }
}
```

The system verifies:
1. `source_commit` equals the sealed review plan's final target commit.
2. `source_commit` is an ancestor of `target_commit`.
3. `target_commit` is contained within `target_ref`.

### Wrap-up on Behalf (Executor Exit Exception)

When an agent process terminates before wrapping up, the coordinator can obtain a dedicated epoch:

```sh
kander dispatch authorize-wrap-up <request.json>
```

- **Verification**: Verifies that the agent process has actually stopped via a fresh process probe.
- **Scope Restriction**: The dedicated epoch permits **only** workspace cleanup and appending `## WRAP_UP_RECORDS`. Code modification or forging author dispositions is prohibited.

---

## 5. Executor Atomic Receipts

The executor acknowledges and completes dispatches via atomic receipt commands:

```sh
# 1. Acknowledge work
kander move <task-id> working --dispatch-id <id> --execution-epoch <epoch>

# 2. Update spec while preserving authorization
kander update <task-id> --document spec.md --file <file> \
  --expect-revision <rev> --dispatch-id <id> --execution-epoch <epoch>

# 3. Complete fix work
kander move <task-id> review --dispatch-id <id> --execution-epoch <epoch> \
  --delivery-commit <fix-sha> --disposition <relative-artifact-path>

# 4. Complete wrap-up
kander move <task-id> done --result completed --dispatch-id <id> \
  --execution-epoch <epoch> --delivery-commit <integrated-sha>
```

- **Replay Safety**: If an acceptance command is retried, it returns `replayed: true` and the original receipt without incrementing the task revision.
- **Authorization Guard**: An old epoch or mismatched dispatch ID cannot commit state transitions.
