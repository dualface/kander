# dsh Kander View

The Kander view is the page half of the `kander-dsh` plugin: a global panel in dsh Web and dsh Desktop that shows a Kander board and the terminals of the agents the [dsh launcher](dsh-launcher.md) runs. Install the plugin as described there; the same installation serves the view.

## Using the View

- Open it with **Kander** in the global panel list of the dsh sidebar. It shows the board of the selected workspace: the workspace of the session the main view shows.
- A session header has a **Kander** button. It opens the view on the board of that session's workspace.
- The board selector at the top lists every board once. A board is the `kanban/` of a Git main worktree; every workspace in that main worktree or in one of its worktrees shows the same board. Selecting another session's workspace returns the view to that workspace's board.
- A workspace without a board shows that and the `kander init` command to create one in its main worktree.
- The tab bar starts with the board tab, which cannot be closed. Every agent terminal opens in a tab of its own; any number can be open at the same time.

### Board Tab

The board tab shows the columns backlog, todo, working, review, and done; archived and trash cards are not shown. A card shows its title, task ID, size, launcher, and, for `working` and `review` cards with a `WINDOW`, the agent status from `kander list --json --agent-status`.

- A `todo` card has **Start**. After confirmation the plugin runs `kander start --launcher dsh <task-id>` in the board's main worktree. On success the new agent's terminal tab opens; on failure the view shows the output of `kander start` and the card stays in `todo`.
- A `working` or `review` card whose `WINDOW` launcher is `dsh` has **Open terminal**; it opens the agent's tab, or switches to it when it is open. Cards of other launchers (tmux, herdr, and others) show the agent status only.
- A card marked **Needs attention** has a dsh agent that waits for terminal input, a dsh agent that exited while the card is still in `working`, or an agent whose snapshot `status` is `stopped` or whose `runtime_state` is `blocked`.

### Terminal Tabs

- A terminal tab shows the agent's terminal and takes keyboard input. Input reaches the agent in the order it was typed: the page sends one request at a time and joins the keys typed meanwhile into the next request.
- Every input request carries the tab's terminal size, so the agent's terminal takes the size of the tab that sent input last. Showing a tab or resizing it sends its size without text.
- A hidden tab keeps receiving output. Closing a tab does not end the agent.
- A tab whose agent exited shows that it exited; a tab whose terminal is gone (the dsh host restarted) shows that.
- The open tabs of each board are kept in the browser's local storage and come back after a page reload.

### Focus

`kander` focus on a dsh pane (the TUI's focus action) records a focus request in the plugin. The view polls it: it shows the Kander panel, selects the board of the pane's main worktree, and opens the agent's tab. Only requests made after the page loaded are followed.

### Two dsh Hosts

When dsh Web and dsh Desktop run on the same `DSH_HOME`, only the carrier host runs dsh agents ([Carrier Host](dsh-launcher.md#carrier-host)). The view of the other host shows the board normally, a notice that the dsh agents run in another dsh, dsh agents by status only, and a disabled **Start**. When the carrier exits and this host takes the socket over, the view enables Start and the terminals on its next poll, without a page reload.

The notice follows the `reason` of `carrierStatus()`:

| Reason | Notice |
| --- | --- |
| `held` | The dsh agents run in another dsh, with its pid when the socket reported one |
| `waiting` | Another dsh is taking over; the agents show their status only for now |
| `starting` | This dsh is starting to host the agents |
| `blocked` | The socket path exists and is not a socket; remove it |
| `unreachable` | This dsh cannot serve the socket; see the dsh log |
| `stopped` | The plugin of this dsh stopped hosting the agents |

Any other reason shows a generic notice that names it.

## Plugin Configuration

The plugin entry in the dsh profile takes two optional settings:

| Key | Value | Effect |
| --- | --- | --- |
| `kander` | absolute path | The `kander` the view runs for every board. When set and not an executable absolute path, the view shows an error instead of searching further. |
| `env` | array of names | Host environment variables forwarded to the agent panes ([Environment of the Agent](dsh-launcher.md#environment-of-the-agent)). |

Set them in the profile's `cordis.patch.yml`, for example:

```yaml
- id: kander-dsh
  config:
    kander: /home/me/.local/bin/kander
```

Without `kander`, the view looks per board, in this order: `<main worktree>/.kander/bin/kander` (a project install), then `kander` on the `PATH` of the dsh host. When none is found, the view shows the places it searched and how to fix it.

The `kander` processes inherit the dsh host's environment, including `DSH_HOME` and `KANDER_CONFIG`, except `KANBAN_DIR`: every call sets it to the selected board's `kanban/`, because `kander` reads `KANBAN_DIR` before it looks at its directory. Every `kander` and `git` call runs asynchronously: `kander start --launcher dsh` calls back into the same host's socket, so a synchronous call would deadlock the host.

## HTTP Routes

The routes are registered through dsh's `connection.fetch.register` under `/api/kander-dsh/`. dsh serves them only to its authenticated owner (401 without a valid cookie) and refuses requests whose `Host` or `Origin` does not name the dsh address, or that are cross-site (403). The plugin opens no network port of its own.

| Route | Method | Answer |
| --- | --- | --- |
| `state` | GET | `{carrier, panes, focus}`: `KanderHost.carrierStatus()`, `listPanes()`, `lastFocus()` |
| `boards?path=...` | GET | `{boards: [{path, project}]}`: the main worktree holding each workspace's board, or `null` |
| `board?project=...` | GET | `{kander, snapshot}`: the output of `kander list --json --agent-status` |
| `start` | POST | body `{project, task}`; `{ok, code, stdout, stderr}` of `kander start --launcher dsh <task>`; 409 `not-carrier` on a non-carrier host |
| `input` | POST | body `{pane, data, cols, rows}`; resizes the pane when the size changed, then writes `data` |
| `stream?pane=...` | GET | `text/event-stream`: `output` events (JSON string payload) with the buffered output first, an `exit` event when the program exited, and a comment every 15 s |
| `xterm.js`, `xterm.css`, `addon-fit.js` | GET | The vendored `@xterm/xterm` 5.5.0 and `@xterm/addon-fit` 0.10.0 |

Errors are `{error: {code, message, ...}}`, with the codes `bad-request` (400), `no-board` and `gone` (404), `not-carrier` and `write-failed` (409), and `kander-not-found` (with `searched`), `kander-failed` (with `stderr`), `bad-output`, and `internal` (500).

The output stream uses `text/event-stream` because dsh's gzip middleware buffers every other chunked response and skips only event streams; with `text/event-stream` the output arrives at once even when the browser accepts gzip.

## Page Implementation

`lib/client.js` is a hand-written classic script that dsh loads through `window.__ModuleLoader__`, with React and the dsh UI primitives from the host. xterm is loaded from the plugin's routes, because dsh does not share its own xterm with plugins.

- The selected workspace comes from the dsh Session and Workspace controllers (`ctx.sessions`, `ctx.workspaces`): the main view's session is the one whose `retainedBy.mainView` count is positive, and its workspace is the one whose `sessionIds` contain it.
- The view polls `state` every 2 seconds (also while the panel is hidden, for focus requests), the shown board every 4 seconds, and the workspace boards every 15 seconds. The board redraws only when `change_token` changes; pane state (exited, input waiting) updates the cards and tabs on every `state` poll.

## Verification

- `node --test "test/*.test.mjs"` in `integrations/kander-dsh/`: board resolution and the `kander` lookup order (`boards.test.mjs`), the routes including input order, resizing, the event stream, and the non-carrier refusal (`routes.test.mjs`), and the page logic (`client.test.mjs`).
- End to end: a real dsh Web host with the plugin, driven by a headless browser. Each card's `report.md` records the run.

## Not Supported

Windows; dsh Desktop is verified only as far as `report.md` of the delivering card says; more board write actions than Start; terminals for agents of other launchers; forwarding terminals from a non-carrier host; a count of cards that need attention on the sidebar entry.
