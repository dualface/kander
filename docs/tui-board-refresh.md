# TUI Board Refresh and Targeted Reads

Board snapshots and single-card detail are coordinated reads owned by `internal/board`. The TUI must not parse card files itself.

## Board APIs

- `BoardPayload` / `BoardPayloadContext` capture every committed card under shared locks and build list summaries. Invalid-entry check warnings stay suppressed; journal advisories return in `warnings`.
- `TaskPayload` / `TaskPayloadContext` use `ScanTargets`: they still enforce path safety, duplicate IDs of the requested card, LANGUAGE/SIZE (including historical form), and pending-transaction rejection, but they do not open unrelated card bodies.
- TUI display calls these APIs. Callers that need lock-wait cancellation pass a context; the context bounds lock acquisition, not every OS read.

## Asynchronous TUI scheduler

Periodic refresh (`r` / the refresh interval), opening or updating a detail, and start-result recovery queue dedicated board/detail reads. They do not occupy the single `pendingWork` slot used by start, Issues, chat, task-action writes, or focus.

- Board and detail are separate kinds: at most one in-flight read of each kind.
- A fresh board request (manual refresh, start result) coalesces onto an in-flight read so the later generation is applied. Periodic ticks do not pile extra reads while one is in flight.
- Each result carries a sequence. A stale board snapshot cannot replace a newer operation result; a detail result for a previous card cannot land after the selection changes.
- A running task-action write invalidates in-flight board reads and blocks new UI board reads until the write worker finishes. The write worker still loads a fresh payload on both success and failure.
- Every Issue import result cancels any older board read, invalidates the affected summary (or the full cache when no task ID is available), and queues a fresh board read before checking whether its Issues overlay or request sequence is still current. Closing the overlay or superseding the request therefore cannot suppress board recovery after an import write.
- A failed read keeps the last valid snapshot (or open detail) and sets the existing refresh/detail error string.
- Quit and closing a detail cancel the reads this session owns.

Keyboard, mouse, and resize continue to flow through `Update` while a read is in flight, except where an existing popup already captures input.

## Agent status observation

The board marks each `working/` or `review/` card whose `WINDOW` names an `AgentIdentity` terminal (herdr, luvus) with the state of its executing agent. Summaries carry the raw `SESSION` and `WINDOW` values, so the TUI still parses no card file.

- One observation calls `liveness.ObserveAgentStatus` for every such card. It lists each backend that declares `agent_list` once, probes `PaneFacts` only for a pane missing from that list or a backend without it, and never runs a reverse lookup. A failed list marks only that backend's cards unknown.
- The classification follows `kander check`: a gone pane, another agent or a different session is lost; an undecidable session or an unclassifiable status is unknown; a pane without a reported session shows its status. `working` shows working, `blocked` blocked, and `idle` or `done` (the agent finished its turn) waiting for input.
- Automatic observations start every 5 seconds, a fixed constant independent of `tui.refresh`; `r` queues one immediately. At most one is in flight and a queued one starts after it. Without observable cards nothing starts, so no terminal process runs.
- The observation runs outside `Update`, carries a sequence, and a result whose sequence is not the in-flight one is dropped. Quit cancels it.
- A status is shown only while the card keeps the `WINDOW` it was observed at. The card title starts with the mark, the panel title (or, on a narrow board, its state tab) adds the blocked count, and the detail view shows the status, observation time and the sanitized reason in the separator row, so neither the card nor the detail geometry changes.

## Process-local summary cache

`SummaryIndex` is a display cache owned by `internal/board`, isolated by a canonical board root (`Abs` + `Clean`, no symlink follow). It is not an authorization source: start, pick, archive, trash, and other writers still use `ReadSnapshot` / `Expect` / CAS.

- The TUI binds `GetBoard` / `GetBoardCtx` to `SummaryIndex.View`. Detail still uses `TaskPayload`. Switching board roots closes the previous index; quit closes the current one.
- First load and a full `Invalidate()` rebuild run a coordinated scan (shared board lock, per-card locks, pending-journal rejection) and parse every committed card. Summaries and sort order match uncached `BoardPayload`.
- A later incremental `View` compares fingerprints (state, path, file vs directory form, revision, size, mtime, volume, file index) and rereads a body only for new, dirty, or fingerprint-changed cards. Unchanged incremental rounds reuse the last sorted `Tasks` slice and do not parse. Deleted IDs are dropped; the map holds only live cards.
- Strong verification starts at most every `max(60s, TUI refresh interval)`. `SetStrongEvery` follows live TUI `RefreshSecs` changes. That pass rereads bodies and checks SHA-256 so same-size/same-mtime edits surface. It does not claim zero body reads. If hashes match, the sorted result may still be reused.
- Fingerprint-visible external add/delete/move/replace appears on the next successful incremental scan. A content edit that preserves every fingerprint field appears on the next successful strong pass.
- `Invalidate(ids...)` marks those IDs (or the whole index when called with no IDs) and bumps a generation so an in-flight scan cannot publish. Scan I/O runs without the index mutex; `Invalidate` and `Close` return without waiting for it. TUI start, pick, return to backlog, archive, and trash call it on completion, including failure and rollback, then request a coordinated refresh. In-flight board reads still follow the generation/sequence rules above.
- A pending prepared journal, unreadable card, duplicate-ID/path failure, canceled/incomplete scan, or shared-lock close failure returns an error and does not publish a mixed generation as a fresh success. The TUI keeps the last valid snapshot and shows the existing refresh error. The cache is memory-only: no SQLite, watchers, or extra config keys.
