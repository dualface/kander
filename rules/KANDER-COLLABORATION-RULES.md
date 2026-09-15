# Communication and Collaboration Rules

## Communication and Formatting

- Tables and diagrams are at most 100 ASCII characters wide; wrap inside cells when they exceed it.
- Number user options from `1`, stating the action and the outcome; replying with only the number is valid. At most one numbered question per message, and any other numbering in that message must be clearly distinguishable from the options.

## Working Principles

- Choose the smallest verifiable solution. Analyze the architecture, boundaries, and root cause; conclusions need evidence.
- For new features, first look for something to reuse or extend. Follow the surrounding style.
- Never overwrite, revert, or clean up user changes. Files the project declares as manually maintained by the user are never modified by the agent.
- When blocked during splitting, verification, or integration, preserve the working state and report.
- Write stable architecture, APIs, and long-term rules into repository documentation, and maintain the documentation index in `AGENTS.md`.

## Security

- Credentials enter only through environment variables, a secret manager, or the repository's agreed gitignored secret files.
- Never write real tokens, credentials, sensitive service addresses, or local machine state into the repository, logs, test fixtures, dry-run output, or release output.
