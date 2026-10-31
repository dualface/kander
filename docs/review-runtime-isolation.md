# Review Runtime Isolation

How `kander review` keeps a reviewer read-only and how the review runtime is protected. The released rules (`rules/KANDER-REVIEW-RULES.md` "Reviewer Isolation") state only the posture an agent must know; the mechanics below are implementation contracts for this repository, verified by the tests of `internal/review`, `internal/process`, and `internal/fs`.

## Reviewer Entry

- Reviewers are the nine built-in agents plus any configured agent that declares a review template (`args.review` together with `review.*`). The public review entry on all platforms is `kander review` under the command root, which enters the single gate implementation.
- On Windows, the reviewer `.exe` is preferred. When only `.cmd`/`.bat` exists, the launch goes through an explicit `cmd.exe /d /s /v:off /c` and the argument encoding of that reviewer's adapter layer. This is not a general invocation contract for arbitrary batch scripts.
- Built-in isolation arguments live on each agent's definition. A custom reviewer's read-only posture is the definition author's responsibility; Kander still validates the result and isolates review-private directories. Apart from the CLI and isolation arguments declared for that reviewer, every review rule is identical for all reviewers.

| reviewer | argument | CLI | isolation |
| -------- | -------- | --- | --------- |
| Codex | `codex` | `codex` | from the embedded definition |
| Claude | `claude` | `claude` | from the embedded definition |
| Grok | `grok` | `grok` | from the embedded definition |
| Cursor | `cursor` | `cursor-agent` | from the embedded definition |
| Pi | `pi` | `pi` | from the embedded definition |
| Devin | `devin` | `devin` | from the embedded definition |
| OpenCode | `opencode` | `opencode` | from the embedded definition |
| Kimi | `kimi` | `kimi` | from the embedded definition |
| Droid | `droid` | `droid` | from the embedded definition |
| custom | the agent name | `review.path` or `path` | author's responsibility |

## Per-Reviewer Posture

- Codex and Grok use sandbox-enforced read-only isolation: Codex runs a read-only shell inside the target worktree.
- Claude and Grok run in an out-of-tree runtime; Grok exposes only read and search tools, while Claude runs fully authorized with its full toolset minus `Edit` and `Write` and relies on that plus the prompt for read-only. Claude's out-of-tree spec is first snapshotted into a runtime exclusive to the current user; the original parent directory is not authorized. Claude's review inherits the caller's environment for its configuration: Kander does not set `CLAUDE_CONFIG_DIR`, so Claude reads its default configuration files; `review.home_env` still names that variable only to locate and validate the review home.
- Cursor passes `--yolo` to approve tool calls without an interactive prompt and isolates configuration and session into the runtime; read-only relies on the prompt and post-run worktree verification, with no upfront blocking and no detection of out-of-tree writes.
- Pi runs without session persistence and uses its default tools and resource loading, including extensions, skills, prompt templates, and themes. Read-only inspection relies on the prompt and post-run worktree verification, not a CLI tool allowlist.
- Devin runs non-interactive print mode under `--permission-mode dangerous`, which auto-approves every tool call instead of prompting; `--respect-workspace-trust false` skips the workspace-trust dialog. There is no enforced read-only sandbox. Read-only relies on the prompt and on the post-run check of the Git-visible state of the target worktree. That check does not detect writes outside the worktree or to ignored paths inside it.
- OpenCode runs `opencode run --pure --format json` with `OPENCODE_PERMISSION` set to a literal JSON policy that denies every action (`"*": "deny") and allows only `read`, `glob`, `grep`, `list`, and `external_directory` (the last so the reviewer can read the runtime bootstrap outside the worktree); it never passes `--auto`, so nothing is auto-approved. Read-only relies on that deny-first policy plus the prompt.
- Kimi runs `kimi --prompt <instruction> --output-format stream-json` under a `kimi-reviewer.md` agent file rendered into the runtime and passed through `--agent-file`; the file allowlists `tools: [Read, Grep, Glob]` and denies shell, write/edit, subagent, and interaction tools, so the reviewer has no write, execute, or delegate capability. It never passes `--auto`/`--yolo`, and `--add-dir` exposes only the private runtime to the reviewer. Read-only relies on that tool policy plus the prompt.
- Droid runs `droid exec --output-format json` at its default autonomy, which is read-only: reading, searching, and shell commands Droid classifies as non-modifying are allowed, while an edit or a command it classifies as modifying ends the run with `is_error` and fails the review. It never passes `--auto` or `--skip-permissions-unsafe`. Read-only relies on that autonomy level plus the prompt.
- For Claude, Cursor, Pi, Devin, OpenCode, Kimi, and Droid the post-run check sees only the Git-visible state of the target worktree: writes outside that worktree and to ignored paths inside it are not detected. Worktree verification never fails a review for writes to paths excluded by `.gitignore`.
- Isolation arguments are never replaced or loosened to unify implementations or accommodate Windows.

## Prompt Delivery

- On all platforms Kander writes `prompt.txt`, a UTF-8 bootstrap file, and `review-contract.md`, a separate file rendered from embedded protocol, scope, and role resources. The bootstrap carries only runtime facts and frozen task/review context and tells the reviewer to read the contract completely. Delivery follows the reviewer definition: `review.stdin` is `instruction` (default) or `none`. With `instruction`, the reviewer receives only a short instruction naming the bootstrap path on stdin. With `none`, the same instruction is passed through `{instruction}` in argv and stdin is not that pipe. Extra files in `review.prompt_files` are rendered into the runtime and referenced as `{prompt_file:<name>}`.
- Built-in reviewers other than Devin, OpenCode, Kimi, and Droid use `review.stdin: instruction` and declare no `review.prompt_files`. Devin's print mode never reads a prompt from stdin, so its definition uses `review.stdin: none` and passes the same instruction through `{instruction}` in argv after a literal `--`, followed by a literal output contract naming the final message as the complete report. OpenCode likewise uses `review.stdin: none`: `opencode run` takes the same instruction and output contract as trailing argv positionals. Kimi also uses `review.stdin: none`: the instruction goes to `kimi --prompt` in argv, and its `kimi-reviewer` prompt file renders the read-only agent definition that `--agent-file` selects. Droid uses `review.stdin: none` too: `droid exec` takes the instruction as its trailing prompt argument, and the output contract is appended through `--append-system-prompt`. Grok's definition keeps `--prompt-file` pointing at Kander's `prompt.txt`; that bootstrap names the contract by absolute runtime path.
- The bootstrap task file does not tighten its own POSIX mode or Windows ACL; the private review runtime protects it. Before launch, Kander validates `review-contract.md` through the no-follow filesystem boundary and sets mode 0400 on POSIX. Windows retains the runtime's protected DACL because the read-only file attribute is not an access-control boundary.

The final message remains the complete report. A `kander-findings` JSON fence is recommended for automatic extraction; complete readable reports with formatting errors are retained for [receiver interpretation](review-disposition.md#receiver-interpretation). This changes no isolation, process collection, worktree or cleanup requirement.

## Process Collection

- After the reviewer exits, the process group must be forcibly reaped; failure to do so is a review failure.
- Cursor, Devin, OpenCode, Kimi, and Droid may ship helper processes that do not wait for wrap-up (`review.spawns_helpers`); for them, members the process tree attributed to the reviewer do not count as leftovers, and ordinary child processes are reaped without rejecting the result. Unattributed members count, and so does every member when the tree never completed an observation. For other reviewers, every group member counts.

### Recheck

On POSIX, a leader that exited with 0 and left a live group member gets a recheck before `SIGKILL`. The recheck polls the process table every 50 ms until no counted member is alive. Its total time, process snapshots included, never exceeds 1 second; each snapshot has at most 500 ms of that. The window is short because a counted child keeps running through it, and the worktree verification after collection still sees what such a child changed.

- A member alive in the last observation is a leftover and rejects the result, also when it appeared after the first observation. Group membership alone decides: a member with a reused PID is a leftover when it is in the group, and a process with a reused PID outside the group is not one.
- A member that exits during the recheck is not a leftover. When the group is empty at the end, Kander sends no `SIGKILL`.
- A failed or late process snapshot rejects the result, and the evidence names the failure.
- Reviewer text is never evidence of cleanup. A clean report does not remove a leftover.

A timeout, an interrupt, or a non-zero exit has no result to protect, so Kander skips the recheck, records the members once, and sends `SIGKILL` at once.

### Process Group Member Records

Kander writes member records whenever it finds the group alive after the leader ended; a run with an empty group has none. The record is written before `SIGKILL` with `cleanup.sigkill` `pending`, because the identities are gone afterwards, and again with the final cleanup result. With a run, it goes to `reviews/<run_id>/process-evidence.json` (see [Review evidence and recovery](review-evidence.md#original-artifacts-and-schema)), and every failure of that run names the path relative to the card. Without a run, one `process evidence:` line per write with the same JSON goes to stderr and the failure names no location.

- `leader_pid`, `recheck` (`performed`, `duration_ms`), and `observation` (`failed`, `reason`).
- `members`: one record per observed process incarnation, so one PID with two start times has two records. Each has `pid`, `ppid`, `pgid`, `start_time`, `executable`, `observed_at` (the last observation that saw it, UTC), and `outcome`.
- `outcome` is `leftover`, `exited`, `attributed` (helper of a tracked reviewer, matched by PID and start time), `not_rechecked` (no recheck ran), or `unknown` (the recheck failed before a conclusion).
- `start_time` is platform text. On Linux it is field 22 of `/proc/<pid>/stat`, in clock ticks since boot. Elsewhere it is the `lstart` of `ps` in the C locale. Compare it only with values of the same host.
- `executable` is a base name. On Linux it comes from the kernel `comm`, which holds at most 15 characters. Elsewhere it is the base name of the `ps` `comm` column.
- `members` holds at most 64 records, leftovers first; `omitted_members` counts the rest.
- `cleanup`: `sigkill` is `pending` (a gate that stopped before the end leaves this), `sent`, `not_sent`, or `failed`; `collected` says whether the group is gone; `error` holds a collection failure.

Records never contain argv or a path, because both can hold credentials or private locations.

Windows produces no member records. A job object reports only an active process count, so a Windows reviewer job with active processes at exit keeps rejecting at once, with no recheck.

### Recovery From a Leftover Rejection

Do not rerun the review with the same input to clear a rejection. Rerunning does not answer why a process stayed. A retry of the same run ID never starts the reviewer; it only verifies the stored evidence.

1. Read `reviews/<run_id>/process-evidence.json` of that run from the card.
2. `leftover` records with `cleanup.collected: true` are real survivors: they outlived the recheck and Kander killed them. The `executable`, `ppid`, and `start_time` identify what the reviewer left. Fix that cause in the reviewer setup.
3. `observation.failed: true` means no conclusion exists about the members. Fix the snapshot failure named in `reason`.
4. Run the role again with a new run ID only after a cause changed, per [Review evidence and recovery](review-evidence.md).

## Runtime Directory Contract

- The runtime follows the permission, pinned handle, and bounded no-follow cleanup contract of the minimal tool protocol: POSIX `0700`; Windows `CREATE_NEW`, collision-safe retry with random names, and a protected DACL exclusive to the current user. Publishing first and tightening later is forbidden.
- Windows exception for sandboxed reviewers: an embedded definition may declare `review_windows_read_groups`, local groups whose accounts run the reviewer's sandboxed commands. After every reviewer input is written (bootstrap, contract, evidence, spec snapshot, prompt files) and before launch, Kander adds a read-and-execute entry for each group to the runtime directory, inheritable by later objects, and to every entry inside it. The DACL stays protected and the current user keeps full control; nothing gains write, delete, or permission-change rights. A group must resolve to a local group of this machine's account domain (`S-1-5-21-…`); builtin groups (`S-1-5-32-*`) and well-known SIDs such as `Everyone` are rejected and fail the review, and a group that does not resolve is skipped, leaving the runtime exclusive to the current user. A reviewer with such groups also gets the task spec snapshotted into the runtime even when its definition does not set `review.snapshot_spec`, because the sandbox accounts cannot read a spec kept in the caller's private directory. Only the embedded Codex definition declares it, naming `CodexSandboxUsers`, the group of the accounts Codex's elevated Windows sandbox runs model commands under. Codex's unelevated sandbox uses per-workspace capability SIDs and is not covered. POSIX ignores the field.
- The Windows review root handle (the lease) does not share DELETE and is held continuously until sensitive files are written, the reviewer has run, the process tree is collected, worktree verification is done, and the contents are removed, which blocks renames, deletion, and in-place reparse switches. The lease itself holds no DELETE access, so a reviewer may make the runtime its working directory (Grok's `--cwd {runtime}` does): `SetCurrentDirectory` opens without `FILE_SHARE_DELETE` and would conflict with a lease holding DELETE. To remove the empty directory, Kander records the lease's volume serial number and file index, closes the lease, reopens the directory with DELETE from the volume anchor without following reparse points, and deletes it only when the identity matches; a mismatch leaves both objects in place and fails the review. The rollback after a failed DACL tightening at creation takes the same path.
- Cleanup rejects reparse points level by level from the pinned handle, with a bounded budget. Cleanup failures such as reparse points, files in use, or budget exhaustion all reject the result; silent leftovers are forbidden.
- Publication settles only after process collection, worktree verification, and runtime cleanup. See [Review evidence and recovery](review-evidence.md) for what is archived per run and how interrupted publications recover.
