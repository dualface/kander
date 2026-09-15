# Review Runtime Isolation

How `kander review` keeps a reviewer read-only and how the review runtime is protected. The released rules (`rules/KANDER-REVIEW-RULES.md` "Reviewer Isolation") state only the posture an agent must know; the mechanics below are implementation contracts for this repository, verified by the tests of `internal/review`, `internal/process`, and `internal/fs`.

## Reviewer Entry

- Reviewers are the five built-in agents plus any configured agent that declares a review template (`args.review` together with `review.*`). The public review entry on all platforms is `kander review` under the command root, which enters the single gate implementation.
- On Windows, the reviewer `.exe` is preferred. When only `.cmd`/`.bat` exists, the launch goes through an explicit `cmd.exe /d /s /v:off /c` and the argument encoding of that reviewer's adapter layer. This is not a general invocation contract for arbitrary batch scripts.
- Built-in isolation arguments live on each agent's definition. A custom reviewer's read-only posture is the definition author's responsibility; Kander still validates the result and isolates review-private directories. Apart from the CLI and isolation arguments declared for that reviewer, every review rule is identical for all reviewers.

| reviewer | argument | CLI | isolation |
| -------- | -------- | --- | --------- |
| Codex | `codex` | `codex` | from the embedded definition |
| Claude | `claude` | `claude` | from the embedded definition |
| Grok | `grok` | `grok` | from the embedded definition |
| Cursor | `cursor` | `cursor-agent` | from the embedded definition |
| Pi | `pi` | `pi` | from the embedded definition |
| custom | the agent name | `review.path` or `path` | author's responsibility |

## Per-Reviewer Posture

- Codex and Grok use sandbox-enforced read-only isolation: Codex runs a read-only shell inside the target worktree.
- Claude and Grok run in an out-of-tree runtime; Grok exposes only read and search tools, while Claude runs fully authorized with its full toolset minus `Edit` and `Write` and relies on that plus the prompt for read-only. Claude's out-of-tree spec is first snapshotted into a runtime exclusive to the current user; the original parent directory is not authorized.
- Cursor only isolates configuration and session into the runtime; read-only relies on the prompt and post-run worktree verification, with no upfront blocking and no detection of out-of-tree writes.
- Pi runs with an explicit `--tools read,bash,grep,find,ls` allowlist and no session persistence; it exposes no edit or write tool, and read-only relies on that allowlist plus the prompt.
- For Claude, Cursor, and Pi the post-run check sees only the Git-visible state of the target worktree: writes outside that worktree and to ignored paths inside it are not detected. Worktree verification never fails a review for writes to paths excluded by `.gitignore`.
- Isolation arguments are never replaced or loosened to unify implementations or accommodate Windows.

## Prompt Delivery

- On all platforms Kander writes `prompt.txt`, a UTF-8 bootstrap file, and `review-contract.md`, a separate file rendered from embedded protocol, scope, and role resources. The bootstrap carries only runtime facts and frozen task/review context and tells the reviewer to read the contract completely. Delivery follows the reviewer definition: `review.stdin` is `instruction` (default) or `none`. With `instruction`, the reviewer receives only a short instruction naming the bootstrap path on stdin. With `none`, the same instruction is passed through `{instruction}` in argv and stdin is not that pipe. Extra files in `review.prompt_files` are rendered into the runtime and referenced as `{prompt_file:<name>}`.
- Built-in reviewers use `review.stdin: instruction` and declare no `review.prompt_files`. Grok's definition keeps `--prompt-file` pointing at Kander's `prompt.txt`; that bootstrap names the contract by absolute runtime path.
- The bootstrap task file does not tighten its own POSIX mode or Windows ACL; the private review runtime protects it. Before launch, Kander validates `review-contract.md` through the no-follow filesystem boundary and sets mode 0400 on POSIX. Windows retains the runtime's protected DACL because the read-only file attribute is not an access-control boundary.

## Process Collection

- After the reviewer exits, the process group must be forcibly reaped; failure to do so is a review failure.
- Currently only Cursor ships helper processes that do not wait for wrap-up; for it, only detached descendants that left the parent chain count as leftovers and cause the result to be rejected, while ordinary child processes do not. For other reviewers, a non-empty process group at the moment of exit causes rejection.

## Runtime Directory Contract

- The runtime follows the permission, pinned handle, and bounded no-follow cleanup contract of the minimal tool protocol: POSIX `0700`; Windows `CREATE_NEW`, collision-safe retry with random names, and a protected DACL exclusive to the current user. Publishing first and tightening later is forbidden.
- The Windows review root handle does not share WRITE/DELETE and is held continuously until sensitive files are written, the reviewer has run, the process tree is collected, worktree verification is done, and cleanup finishes, which blocks renames and in-place reparse switches.
- Cleanup rejects reparse points level by level from the pinned handle, with a bounded budget. Cleanup failures such as reparse points, files in use, or budget exhaustion all reject the result; silent leftovers are forbidden.
- Publication settles only after process collection, worktree verification, and runtime cleanup. See [Review evidence and recovery](review-evidence.md) for what is archived per run and how interrupted publications recover.
