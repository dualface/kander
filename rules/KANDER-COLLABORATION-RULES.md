# Communication and Collaboration Rules

## Communication and Formatting

- Tables and diagrams are at most 100 ASCII characters wide; wrap inside cells when they exceed it.
- Number user options from `1`, stating the action and the outcome clearly; replying with only the number is valid. Any other numbering must be clearly distinguishable from user options.

## Working Principles

- Choose the smallest verifiable solution. Analyze the architecture, boundaries, and root cause; conclusions need evidence.
- For new features, first look for opportunities to reuse or extend. Follow the surrounding style.
- Never overwrite, revert, or clean up user changes.
- When blocked during splitting, verification, or integration, preserve the working state and report.
- Write stable architecture, APIs, and long-term rules into repository documentation.
- Maintain the repository documentation index in `AGENTS.md`.

## User-Maintained Files

- Files the project declares as manually maintained by the user must not be modified by the agent.

## Security

- Credentials are injected only through environment variables, a secret manager, or the repository's agreed gitignored secret files.
- Never write real tokens, credentials, sensitive service addresses, or local machine state into the repository, logs, test fixtures, dry-run output, or release output.
