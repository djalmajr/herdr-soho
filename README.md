# herdr-agents

Role-based coding agents in [Herdr](https://herdr.dev). The calling agent
orchestrates workers in Herdr panes, sends self-contained briefs, waits for
file-based reports, and owns integration and Git operations.

The repository contains:

- [`skills/herdr-agents/`](skills/herdr-agents/) — the agent skill, roles,
  templates, dependency-free Node/Bun CLI, and tests.
- [`plugin/`](plugin/) — an optional local Herdr plugin that exposes
  workspace-scoped actions through the same CLI.
- [`docs/guide.md`](docs/guide.md) — usage and configuration guide.

## Install the skill

```sh
npx skills add djalmajr/herdr-agents --skill herdr-agents -g
```

The CLI needs Herdr and Node.js 20+ or Bun. Run it from a Herdr-managed
agent pane. The skill works without the plugin.

## Develop locally

```sh
node --test skills/herdr-agents/scripts/test/
bun test skills/herdr-agents/scripts/test/
skills/herdr-agents/scripts/run-tests.sh
```

For configuration, commands, and the report contract, see the
[guide](docs/guide.md) and the [skill](skills/herdr-agents/SKILL.md).
