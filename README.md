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
  CLI, plus a session picker (`pick`) that copies a session reference to
  the clipboard. Its actions inspect the focused project and never write
  to it (they run the CLI with `HERDR_SOHO_NOWRITE=1`: no `.gitignore`
  entry, no state directory).
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

## Instalação do binário

No macOS ou Linux, baixe e execute o instalador:

```sh
curl -fsSL https://raw.githubusercontent.com/djalmajr/herdr-soho/main/install.sh -o install.sh
sh install.sh
```

No Windows PowerShell 5.1+, baixe e execute o instalador:

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/djalmajr/herdr-soho/main/install.ps1 -OutFile "$env:TEMP\herdr-soho-install.ps1"
powershell -NoProfile -ExecutionPolicy Bypass -File "$env:TEMP\herdr-soho-install.ps1"
```

Para instalar uma versão específica, passe `--version vX.Y.Z` ao `install.sh`
ou `-Version vX.Y.Z` ao `install.ps1`. Sem versão, os instaladores baixam a
última release estável; uma pré-release (tag com hífen, como `v0.2.0-rc.1`)
só é instalada pedindo a versão, e enquanto não houver release estável é
preciso passá-la. Os instaladores verificam o arquivo
`SHA256SUMS` da mesma release antes de substituir o binário. Eles instalam no
diretório do usuário sem alterar o `PATH`; mostram a linha necessária quando o
diretório ainda não está nele. No Windows, `-AddToPath` altera o `PATH` do
usuário.

Também é possível baixar um binário manualmente. Escolha `herdr-soho_<os>_<arch>`
na página de [releases](https://github.com/djalmajr/herdr-soho/releases), baixe
`SHA256SUMS` da mesma release e confira o nome exato do arquivo:

```sh
sha256sum -c SHA256SUMS --ignore-missing
```

No macOS, use `shasum -a 256 herdr-soho_darwin_arm64` e compare o hash impresso
com a linha correspondente em `SHA256SUMS`. No Windows PowerShell, use:

```powershell
Get-FileHash .\herdr-soho_windows_amd64.exe -Algorithm SHA256
```

Compare o hash exibido com a linha do mesmo nome em `SHA256SUMS` antes de rodar
o binário baixado.

## Migrating from herdr-agents

`herdr-soho` is the new name of the `herdr-agents` skill, CLI and plugin.
Replace a global installation with:

```sh
bunx skills remove herdr-agents -g -y
bunx skills add djalmajr/herdr-soho --skill herdr-soho -g -y
```

Then run `herdr-soho setup` (or `setup --local` in a fork) once in each
project set up with `herdr-agents`. It renames the old instruction block in
place — markers and `herdr-agents` names — keeping the block's text,
including lines the project added, replaces the old Claude hooks, and keeps
the rest of those files. Until
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

### Finding a session

The `pick` action ("Find a session and copy its reference") opens a
picker over the focused pane, listing the panes of the local Herdr server
and of every enabled machine from `herdr machine list` — local first,
remotes appended as they arrive (a machine that fails to load shows a
status line, not an error). Type to filter: letters, digits, space and
punctuation match case-insensitively over the reference, name, kind,
status, workspace and tab labels, cwd and machine (`↑`/`↓` select,
`Enter` copies the selection to the clipboard and closes, `Esc` or
`Ctrl-C` close without copying). The copied text puts the session
reference first, so it can be pasted into the chat as-is:

```text
local/w12:p1 (orchestrator-10, claude, working) /Users/…
```

A name, kind or cwd that is null is copied as `-`. The copy prefers the
platform's native tool (macOS `pbcopy`; Windows PowerShell
`Set-Clipboard`; Linux `wl-copy`, then `xclip -selection clipboard`,
then `xsel --clipboard --input`) and, when none exists or they fail,
writes OSC 52, which Herdr forwards to the user's terminal. A successful
copy also shows a `herdr-soho` notification.

To bind the action to a key, add an entry to the Herdr config (any key
you like):

```toml
[[keys.command]]
key = "prefix+l" # any key
type = "plugin_action"
command = "djalmajr.herdr-soho.pick"
```

## Develop locally

```sh
node --test skills/herdr-soho/scripts/test/
bun test --timeout 60000 skills/herdr-soho/scripts/test/
skills/herdr-soho/scripts/run-tests.sh
```

Bun's default per-test timeout (5 s) is too short for the process-heavy
tests on slower hosts such as Windows, hence `--timeout`.

For configuration, commands, and the report contract, see the
[guide](docs/guide.md) and the [skill](skills/herdr-soho/SKILL.md).
The [architecture](docs/architecture.md) explains the boundary between
the CLI and optional plugin.
