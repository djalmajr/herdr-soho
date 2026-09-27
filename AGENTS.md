# herdr-soho

This repository contains the distributable skill in `skills/herdr-soho/`
and an optional Herdr plugin in `plugin/`.

- The skill, CLI command, and plugin identifier are named `herdr-soho`.
  They replace the former `herdr-agents`; keep the legacy fallbacks
  (`HERDR_AGENTS_*`, `herdr-agents` config and state paths, the old
  instruction markers and hooks) so projects set up under the old name keep
  their configuration and state and migrate with `herdr-soho setup`. Never
  publish a second installable skill for the old name.

- Keep the CLI's report, dispatch, model-family, and configuration rules as
  the shared implementation. Plugin entrypoints call those rules; they must
  not maintain a second copy.
- Check the workspace, repository root, and pane context before a plugin
  action changes state. A plugin invocation can follow the focused Herdr
  workspace rather than the invoking shell's `HERDR_*` variables.
- Keep skill, plugin, and documentation portable. Do not add machine paths,
  private provider names, credentials, or project-specific examples.
- Run focused tests while editing, then the Node, Bun, and Bash matrices
  before shipping a change to the skill.
