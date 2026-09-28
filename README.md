# herdr-soho

The standalone home of the `herdr-soho` skill and plugin. Role-based
coding agents run in [Herdr](https://herdr.dev). The calling agent
orchestrates workers in Herdr panes, sends self-contained briefs, waits for
file-based reports, and owns integration and Git operations.

The name *soho* comes from **SO**ftware **HO**use: the calling agent runs a
small software house of role agents — builders, reviewers, researchers —
and keeps the planning, integration and release work to itself.

The repository contains:

- [`skills/herdr-soho/`](skills/herdr-soho/) — the agent skill, roles,
  templates, dependency-free Node/Bun CLI, and tests.
- [`plugin/`](plugin/) — an optional local Herdr plugin that exposes
  workspace-scoped, read-only actions (`doctor`, `roster`) through the same
  CLI. Its actions inspect the focused project and never write to it (they
  run the CLI with `HERDR_SOHO_NOWRITE=1`: no `.gitignore` entry, no state
  directory).
- [`docs/guide.md`](docs/guide.md) — usage and configuration guide.

## Install the skill

```sh
bunx skills add djalmajr/herdr-soho --skill herdr-soho -g
```

The CLI needs Herdr and Node.js 20+ or Bun. Run it from a Herdr-managed
agent pane. The skill works without the plugin. The global install command
above updates any existing `herdr-soho` installation; skip it if you want
to keep your current version. `herdr-soho` replaces the former
`herdr-agents` skill; see [Migrating from herdr-agents](#migrating-from-herdr-agents).

## Migrating from herdr-agents

`herdr-soho` is the new name of the `herdr-agents` skill, CLI and plugin.
Replace a global installation with:

```sh
bunx skills remove herdr-agents -g -y
bunx skills add djalmajr/herdr-soho --skill herdr-soho -g -y
```

Then run `herdr-soho setup` (or `setup --local` in a fork) once in each
project set up with `herdr-agents`. It replaces the old instruction block
and the old Claude hooks in place and keeps the rest of those files. Until
then, the old `SessionStart` hook prints `herdr-agents doctor: skill script
not found` and exits 0, and the old block names a skill that is no longer
installed.

The CLI keeps reading the old names while the new ones are absent, so no
project configuration or state is lost:

- `HERDR_AGENTS_*` variables, `~/.config/herdr-agents/config`,
  `.agents/herdr-agents.conf`, and the `.herdr-agents/` state directory.
  The first write to a config file (`config set`, `setup --panes`,
  `doctor --fix`) copies the legacy file to its new name and leaves the old
  file in place; `setup` alone only replaces the block and hooks.
- `doctor` prints a `legacy …` warning for each old name still in use.

## Optional Herdr plugin

The plugin supports Herdr 0.9.1+ on Linux, macOS and Windows (it runs the
CLI with the `node` on the Herdr host's `PATH`). From a local checkout of
this repository, link it once:

```sh
herdr plugin link "$PWD/plugin"
herdr plugin action list
```

A checkout linked before the rename is registered as
`djalmajr.herdr-agents`; unlink it and link the checkout again so Herdr
loads `djalmajr.herdr-soho`.

The `doctor` and `roster` actions inspect the workspace currently focused
in Herdr, which can differ from the invoking shell's `HERDR_*` variables.
Their output names the resolved workspace and pane. To inspect action
results from the CLI, run `herdr plugin log list`.

## Develop locally

```sh
node --test skills/herdr-soho/scripts/test/
bun test skills/herdr-soho/scripts/test/
skills/herdr-soho/scripts/run-tests.sh
```

For configuration, commands, and the report contract, see the
[guide](docs/guide.md) and the [skill](skills/herdr-soho/SKILL.md).
The [architecture](docs/architecture.md) explains the boundary between
the CLI and optional plugin.
