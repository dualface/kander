# GitHub Issue Import

The `kander issue import NUMBER` command converts a remote GitHub issue into a local Kander card in `backlog/`, storing the raw, untouched remote text in dedicated attachments.

The import operation is **idempotent**, **bounded**, and published within a **single exclusive-board transaction**, ensuring a card is never published without its source snapshot.

---

## 1. Import Command & Overlay Shortcut

```sh
kander issue import NUMBER [--repo HOST/OWNER/REPO] [--comments]
                           [--type feature|bug|chore|research] [--large]
                           [--language LANG] [--json]
```

- `--repo`: Target repository identity (e.g. `github.com/owner/repo` or `owner/repo`). Reuses existing `gh` CLI credentials without asking for tokens.
- `--comments`: Fetches the issue conversation thread. Omitted by default to minimize bandwidth.
- `--type`: Overrides automatic label inference.
- `--large`: Explicitly sets `SIZE: large`. Imported cards are always created in directory form.
- `--language`: Sets the card's `LANGUAGE` field (defaults to the configured `agent_language`).
- `--json`: Emits machine-readable output: `task_id`, `state`, `path`, `existing`, `source_key`, `source_url`, `comments_loaded`.

### Interactive TUI Overlay (`g`)

Inside the terminal kanban board, pressing `g` opens the GitHub Issues overlay:
- `i` / `I`: Imports the selected issue (`I` includes comments).
- `g`: Jumps directly to the linked local task card if already imported.
- `s`: Starts an interactive triage takeover session for unbound issues, or starts [result reconciliation](github-issue-results.md) for completed cards.

---

## 2. Source Key & Idempotency Invariants

The unique identity binding of an imported card is its canonical source key:

```text
github://HOST/OWNER/REPO/issues/NUMBER
```

- **Atomic Uniqueness Check**: Checked inside an exclusive `board.lock` transaction.
- **Concurrent Imports**: Repeated calls or concurrent processes importing the same issue resolve to the identical task card (`existing: true`) without duplicate fetches.
- **Readable Collision Resolution**: If a slug collision occurs, `-<8-hex-digits>` of the source key hash is appended to the task ID.

---

## 3. Attachments & Security Sanitization

Every imported task directory stores two machine-managed source artifacts alongside `spec.md`:

```text
kanban/backlog/<task-id>/
  ├── spec.md
  └── source/
      ├── github-issue.json    (Schema-versioned machine snapshot)
      └── github-issue.md      (Read-only Markdown rendering)
```

### Security Boundary: Untrusted Remote Data

To prevent prompt injection, privilege escalation, or corrupted board states:
1. **No Metadata Injection**: Remote text **never** touches YAML frontmatter, metadata headers, or review sections. An issue author cannot forge `CARD_REVIEW` or `SELF_REVIEW` records.
2. **Title Sanitization**: Only the issue title appears in `spec.md` as the `H1` heading, strictly stripped to a single line.
3. **Text Sanitization**: Terminal escape sequences, C0/C1 control characters, bidi overrides, and malformed UTF-8 are stripped before writing.

### Payload Bounds

| Bound | Maximum Limit | Action on Exceed |
|---|---|---|
| Issue Body | 512 KiB | Rejected with error hint |
| Single Comment | 64 KiB | Rejected with error hint |
| Comment Count | 50 comments | Truncated to 50 latest comments |
| Total Payload | 1 MiB | Rejected with error hint |

---

## 4. Issue Snapshot Cache

The issues overlay maintains a local disk cache under:

```text
<board>/.kander/caches/issues/v1/<sha256(source_key)>.json
```

- **Instant Paint**: Displays cached body and comments immediately upon row selection while the network fetches updates in the background.
- **Privacy & Permissions**: Stored with `0600` permissions, capped at 200 records or 16 MiB total. Evicts oldest entries first.

---

## 5. Anchor Cards vs. Sibling Cards

When work for a single large issue is decomposed across multiple cards:

```text
[GitHub Issue #42]
        |
        v (kander issue import)
+-------------------------------+
| Anchor Card (owns source key) |
+-------------------------------+
        |
        +---> Sibling Card 1 (references anchor)
        +---> Sibling Card 2 (references anchor)
```

- **Anchor Card**: The only card holding `source/github-issue.*` attachments and owning the `source_key` binding.
- **Sibling Cards**: Do not re-import the issue. Instead, declare the relationship in their `DISCUSSION` block:
  ```text
  SOURCE_ISSUE: github://HOST/OWNER/REPO/issues/42 (anchor: <anchor-task-id>)
  ```
