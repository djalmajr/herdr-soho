# Architecture

The skill, CLI, and optional Herdr plugin are distributed together so
their contracts can be reviewed and released as one change.

## Responsibilities

| Layer | Owns |
|---|---|
| Agent skill | When to delegate, role instructions, brief and report contracts, review and integration guidance |
| CLI | Configuration, role and model resolution, pane orchestration, prompt delivery, report-based completion, collection, and statistics |
| Plugin | Herdr menu actions, terminal UI, context selection, event-driven presentation, and shortcuts |

The CLI is the programmatic interface for agents and scripts. Plugin
commands invoke it with an explicit workspace context; they do not copy
its state machine or report parsing. The plugin is optional and its
installation is user-wide, while CLI state is scoped to the current
project and Herdr workspace.

## Boundary for new features

- Keep an operation in the CLI when it takes dynamic arguments, must work
  without the plugin, or defines an observable workflow contract. `spawn`,
  `dispatch`, `wait`, `collect`, and `setup` meet this test.
- Expose an existing CLI operation in the plugin when Herdr context or a
  terminal interface makes it easier to use. `doctor`, `roster`, and
  later interactive collection or layout actions fit here.
- Put a feature only in the plugin when it exists solely to customize
  Herdr's interface, such as a keybinding, Agent view, popup, or link
  handler.

Herdr plugin actions and panes are declared in a manifest. An
interactive terminal pane can gather runtime choices; a fixed action
cannot receive arbitrary CLI arguments. Plugin actions receive the
focused Herdr context, which can differ from the invoking shell's
`HERDR_*` variables. Any action that mutates project state must show
the resolved target and confirm it first.

The report file remains the completion signal for a delegated task.
Herdr agent status and events can prompt a check, but do not replace
that contract.
