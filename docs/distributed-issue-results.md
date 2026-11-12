# Distributed Issue results

Issue completion uses the imported anchor and its complete work set. The provider remains on the client. The service stores formal result history and serializes each update through an independent resource version. No result operation borrows a completed task's former claim or changes its card revision.

## Evidence and CAS

`POST /api/v1/projects/{project}/issues/results/view` takes `source_key` and the anchor `task_id`. The service resolves the confirmed imported repository and issue number itself, requires every remaining member to be complete, and returns the work evidence, result store and resource revision. A sibling query names the canonical anchor in its rejection.

`POST .../issues/results/save` binds that query, `expected_revision`, `work_digest`, and the complete typed store. The service rechecks the work set, original source binding, member versions, completion state and report evidence under one board-exclusive transaction. It writes the next result version and exact idempotency receipt in that same redo record. New members, changed documents, a stale result revision or removed history refuse the write. Read-only transaction views grant no card or control write capability.

The first central write adopts validated result history from a migrated v1 board. Earlier comment bodies, numeric identities and decisions remain intact. Subsequent writes cannot remove records, regress a published result or reuse a settled close decision. The issue index reads the central evidence version so all clients see whether a result is still pending.

## External effects

The existing result protocol performs semantic assessment on the client. It checks every criterion, recorded verification commands, delivery identities, full-resolution evidence, safe comment content and complete provider observations. The service does not run `gh` or independently verify GitHub facts.

Before posting a comment, the client saves its exact body, random marker, numeric actor identity and uncertain status through CAS. A competing save fails before calling the provider. Before closing, it separately saves explicit user consent for the current issue state version and an uncertain close intent. Publishing a result never implies permission to close.

Provider writes are attempted once. A response error does not prove no effect. Another client can reconcile an uncertain comment only through complete remote observations that match its exact body and numeric author; a temporarily absent comment does not permit another post. A closed observation can reconcile an uncertain close. Reopening invalidates earlier consent. Refusal persists until an explicit reconsideration for that same state. Unknown outcomes remain visible for inspection.

Private save receipts freeze the original request and idempotency key before transport. An already committed response can be recovered without repeating the domain write. These receipts do not claim exactly-once provider delivery.

## Client sessions

CLI result actions and the TUI result callback bind the protocol to the typed service. Inspection files live in the client project's private directory. They are not a writable board or an offline queue. The launcher uses the downloaded anchor's frozen language and document, rather than resolving a local card path.

A result start has a private action journal. It records session allocation, the task file and launch/prompt boundaries before their effects. A completed logical retry returns the existing session. An uncertain retry retains its evidence and refuses another launch. A lost prompt delivery preserves the terminal scene and task file. `kander issue result ... --operation-id <stable-id>` identifies an intentionally new session; a different ID does not settle an earlier uncertain effect. The launched session has its own execution identity and clears inherited task/coordinator authority.

The service does not manage the lifetime of a result session as a task lease. Concurrency at the external-write boundary is controlled by the durable uncertain intent and the result resource CAS. Task workers continue to renew their own working/review claims independently.
