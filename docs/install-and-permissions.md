# Installation and Permission Boundaries

How the binary installs itself and which filesystem boundaries the configuration, board, Git exclude, and review runtime enforce. The released rules (`rules/KANDER-BASE-RULES.md`) keep only the boundaries an agent must respect; the mechanics below are implementation contracts for this repository, verified by the tests of `internal/install`, `internal/config`, `internal/board`, and `internal/fs`.

## Installation

- Installation is done by the binary itself: `kander install` runs the interactive wizard (language, scope). Bare interactive `kander` with no scope `config.json` skips the wizard, lets doctor create a usable config, and opens the board options interface section; when a config already exists, bare `kander` opens the board directly.
- Global `kander install` writes configuration and rules. When the first `kander` on PATH does not identify the running executable, it copies that executable to the global entry and replaces a file already there; it also deletes retired onevoke/kanban entries in that bin directory. Bare interactive `kander` with an existing scope config asks a Y/N question (default no) for the same copy; declining continues this launch and does not persist a skip. Project installs copy into the main worktree's `.kander/bin`.
- Copying never changes shell configuration. If PATH still selects another entry or omits the destination directory, the command prints instructions. Windows does not modify `PATH` automatically.
- The installer copies the same English Markdown rule originals to the rules root and generates no customized rule files. `kander-rules-state.json` records the installed hashes; when it is absent, doctor distinguishes outdated official copies from local edits with the registry in `internal/install/legacy_hashes.go`.
- The global rules root is `~/.agents/kander/`; a project install uses `<main worktree>/.kander/rules/`. Earlier releases wrote the global files directly into `~/.agents/`. A global install or `kander doctor --repair` retires that location silently: every file named like a rule file, plus the state file, is deleted whatever its content or link status; only a directory under such a name stays. Agent rules files that import or reference the previous entry path are rewritten to the current one; a rules file that was a symlink to the previous entry is replaced by a reference file, and that replacement is reported. Removing a global install means deleting `~/.agents/kander/` and the Kander reference line in each agent rules file.

## Task Files

- On every platform the executing agent and the reviewer read the complete task from a UTF-8 temporary file; the launch arguments contain only the CLI's required control options and a one-line instruction with the file path. The file asks the agent to try deleting it when done; a failed deletion or a leftover file does not affect the result.
- Automation involving special characters must invoke the command root's `kander` through a process API argv array; PowerShell/cmd command strings are never assembled.

## Review-Private Files

- Review-private directories and files are accessible only to the current user: POSIX `0600`/`0700`. Windows applies a protected DACL with inheritance disabled at creation time; publishing first and tightening later is forbidden.
- The Windows review root handle does not share WRITE/DELETE and is held until sensitive files are written, the reviewer has run, the process tree is collected, and cleanup finishes, which blocks renames and in-place reparse switches. Cleanup rejects reparse points level by level from the pinned handle, with a bounded budget; failure means the review fails. See [Review runtime isolation](review-runtime-isolation.md).

## Configuration, Board, and Git Exclude

- The configuration does not check, migrate, or tighten permissions: POSIX keeps the existing mode, new objects follow the umask; on Windows, new config files and directories inherit the parent ACL.
- On Windows, the configuration rejects reparse points component by component from the volume/UNC anchor and uses pinned handles for reading and atomic replacement. The kanban board and Git exclude likewise reject symlinks, junctions, and other reparse points; `kander` reads, writes, and moves through validated Win32 handles, and POSIX keeps using no-follow file operations. Any failed safety check stops the operation.
- `init` creates the seven state directories under these boundaries: on Windows, new directories are created with `CREATE_NEW` relative to a pinned parent handle with a protected DACL exclusive to the current user applied at creation, and a creation race rejects the operation; existing directories only get their leaf directory ACL migrated. Git projects only update the local `info/exclude`: the parent chain rejects reparse points component by component, existing ACLs are unchanged, and deduplicating read and append happen within the same pinned leaf handle and file lock.
- Bypassing the command to operate on these boundaries directly is forbidden; see [Card transactions and recovery](card-transactions.md) for the transaction protocol behind board writes.
