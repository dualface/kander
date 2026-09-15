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
- A failed read keeps the last valid snapshot (or open detail) and sets the existing refresh/detail error string.
- Quit and closing a detail cancel the reads this session owns.

Keyboard, mouse, and resize continue to flow through `Update` while a read is in flight, except where an existing popup already captures input.
