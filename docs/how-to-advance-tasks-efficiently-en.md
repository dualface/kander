# How to Advance Tasks Efficiently with kander

Author: [dualface](https://github.com/dualface)

[中文](how-to-advance-tasks-efficiently-cn.md) | [日本語](how-to-advance-tasks-efficiently-ja.md)

## What is kander?

- A task card system defined in Markdown.
- A rule-driven task orchestration system.
- Plus an easy-to-use TUI.

## How to Install

1. Download the latest release from <https://github.com/dualface/kander>.
2. Run `kander install`.
3. Run `kander init` in your project to create the `./kanban/` directory structure.

## Where Do Task Cards Come From?

1. The user discusses requirements with the Agent, or asks the Agent to look at a GitHub issue.
2. The user and the Agent agree on the task to be done.
3. The user asks the Agent to create a task card.

## What's in a Task Card?

- Depending on task complexity, a task card consists of one or more Markdown files.
- All task cards are stored under the project's `./kanban/` directory.
- The Markdown contains everything needed to complete the task:
  - Task description
  - Task checklist
  - Acceptance checklist

## How to Start a Task Card?

- The Agent usually asks proactively whether to start the task card.
- The user can also explicitly tell the Agent to start it.

## How Is a Task Card Executed?

- Following the rules, the Agent runs `kander start` to launch an Agent that executes the task described in the card.
- The user can run `kander` to open the TUI and view the status of all tasks.

## TUI

![kander TUI board with Backlog / Todo / Working / Review / Done columns](images/how-to-advance-tasks/board.png)

## Tune the Executing Agent

- Assign different Agents and models to tasks of different sizes.
- Balance quality and cost.

![Execution settings: Agent, model, and effort for large and small tasks](images/how-to-advance-tasks/execution-agents.png)

## Tune the Review Agents

- Assign different Agents and models to each review role.
- Cross-review gives better results.

![Review settings: Agent, model, and review stage for each review role](images/how-to-advance-tasks/review-agents.png)

## Customize Rule Modules as Needed

- The full workflow yields the highest quality.
- Unneeded rule modules can be disabled.

![Rule module settings: workflow preset and per-module switches](images/how-to-advance-tasks/rule-modules.png)

## Open Source, Auditable, Customizable

- Audit the rule definitions thoroughly.
- Custom rules take higher priority.
- Disable kander's rule modules and replace them with your own.

## Thank You

<https://github.com/dualface/kander>

Please give it a Star ;-)
