# Deployment and release

The assigned owner follows the user's recorded procedure. While an operation is active, retain the owner, target host or repository, commit or version, and last confirmed step.

## Deployment

1. Build the specified commit and verify the version before transfer. Run the checklist's read-only readiness checks and, for server projects, the [v2 upgrade checks](server-mode.md#upgrade). Defer while a Kander write or review is running, a review runtime lock is held, or the upgrade could prevent publication of a finalized review. Record the blocker and recheck at the next approved patrol or deployment opportunity.
2. Perform the prescribed transfer, atomic replacement, and restart. Confirm readiness still holds before replacement or restart. If a step fails, preserve the resulting state and report the failed step and its output immediately.
3. Verify service and claim health, including running worker versions for server projects. Notify the remote orchestrator when idle, giving the verified version, commits, and resolved items.

## Interrupted operations

After a lost response or interruption, verify the last step against its target before resuming:

| Operation | What to check |
| --- | --- |
| Deployment | Inspect the installed and running builds and service health. If a version label was reused, match the target commit, build ID, or digest to the running instance. If its identity is unavailable, record the step as unconfirmed. |
| Publication | Inspect the remote tag target, release, and uploaded assets. If publication failed after the tag push, preserve the tag and present options with a recommendation. Obtain the user's decision before retagging or publishing another version. |

Resume only steps confirmed outstanding under existing authorization. Report ambiguous effects before retrying a consequential step. Remove the temporary progress entry after verified completion.
