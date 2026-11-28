# kander order

## Purpose and users

Reads and withdraws the start orders `kander start --host` creates: the request that a named host starts a `todo/` card. Used by the user and by an orchestrator after `kander start --host` ended without a final state.

## Syntax

```text
kander order show <order-id>
kander order cancel <order-id>
```

## Arguments

- `order-id`: the ID `kander start --host` printed (`so-` and 16 hex digits).
- `show`: print the order.
- `cancel`: withdraw an order that no claim took yet.

## Examples

```text
# Read an order after kander start --host timed out
kander order show so-0123456789abcdef

# Withdraw it
kander order cancel so-0123456789abcdef
```

## Preconditions

- A checkout bound to the project of the order.

## Effects

- An order is `pending` until its target host takes it (`accepted`), then `started` once the start it ran confirmed, or `failed`, `cancelled`, or `expired`. The last four are final and never change. The server decides each state change with its start order state machine and appends a `start_order_changed` event to the project's event stream.
- `cancel` applies to a `pending` order and to an `accepted` one whose start has not claimed the card yet; it ends the order as `cancelled`. Once a claim took the order, only that start decides it.
- The server expires an order that no host takes within 120 seconds, and fails an accepted order whose host sends no heartbeat for 60 seconds before it claims (`host_lost`). A claim of the card by anybody else fails an open order (`already_claimed`); the end of the bound generation's claim before its start confirmed fails it as `superseded`, and a rolled back start as `launch_rolled_back`.

## Output

- Text: the order ID, state, task, and host; the agent and launcher it names; the generation and window of its start; the reason of a final state other than `started`; and when it was created and last changed.
- With `--json`: the order object: `id`, `project_id`, `task_id`, `target_client_id`, `target_host`, `agent`, `launcher`, `requester_client_id`, `state`, `reason` (`code` and `message`), `generation`, `window`, `created_at`, and `updated_at`.

## Rejections

- A missing or malformed order ID is a usage error; an unknown order fails with `not_found`.
- `cancel` of an order in another state fails with `start_order_refused`.
- A server without hosts and start orders fails with `start_order_unsupported`; upgrade the server.
