# herdr-soho

The standalone home of the `herdr-agents` skill and plugin. Role-based
coding agents run in [Herdr](https://herdr.dev). The calling agent
orchestrates workers in Herdr panes, sends self-contained briefs, waits for
file-based reports, and owns integration and Git operations.

The repository contains:

- [`skills/herdr-agents/`](skills/herdr-agents/) — the agent skill, roles,
  templates, dependency-free Node/Bun CLI, and tests.
- [`plugin/`](plugin/) — an optional local Herdr plugin that exposes
  workspace-scoped, read-only actions (`doctor`, `roster`) through the same
  CLI. Its actions inspect the focused project and never write to it (they
  run the CLI with `HERDR_AGENTS_NOWRITE=1`: no `.gitignore` entry, no state
  directory).
- [`docs/guide.md`](docs/guide.md) — usage and configuration guide.

## Install the skill

```sh
npx skills add djalmajr/herdr-soho --skill herdr-agents -g
```

The CLI needs Herdr and Node.js 20+ or Bun. Run it from a Herdr-managed
agent pane. The skill works without the plugin. The global install command
above updates any existing `herdr-agents` installation; skip it if you want
to keep your current version.

## Optional Herdr plugin

The plugin supports Herdr 0.9.1+ on macOS. From a local checkout of this
repository, link it once:

```sh
herdr plugin link "$PWD/plugin"
herdr plugin action list
```

The `doctor` and `roster` actions inspect the workspace currently focused
in Herdr, which can differ from the invoking shell's `HERDR_*` variables.
Their output names the resolved workspace and pane. To inspect action
results from the CLI, run `herdr plugin log list`.

## Develop locally

```sh
node --test skills/herdr-agents/scripts/test/
bun test skills/herdr-agents/scripts/test/
skills/herdr-agents/scripts/run-tests.sh
```

For configuration, commands, and the report contract, see the
[guide](docs/guide.md) and the [skill](skills/herdr-agents/SKILL.md).
The [architecture](docs/architecture.md) explains the boundary between
the CLI and optional plugin.
