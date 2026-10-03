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
  workspace-scoped actions (`doctor`, `roster`, `team`) through the same
  CLI, a session picker (`pick`) that copies a session reference to the
  clipboard, and a team board (`board`). The read-only actions open the
  team panel, which inspects the focused project with the CLI running in
  `HERDR_SOHO_NOWRITE=1`; only the panel's `x` and `g` confirmations run a
  write (a release or `gc --yes`), never without the on-screen
  confirmation.
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

The plugin supports Herdr 0.9.1+ on Linux, macOS and Windows. Its
actions and picker run the `herdr-soho` binary (the Go CLI), which must
be on the Herdr server's `PATH` — install it with `install.sh` or
`install.ps1`. If the binary was installed after the server started,
restart the server in a shell that sees it.

From a checkout of this repository, link it once:

```sh
herdr plugin link "$PWD/plugin"
herdr plugin action list
```

Or install it from GitHub, for example with the tag of the installed
release:

```sh
herdr plugin install djalmajr/herdr-soho/plugin --ref <tag>
```

A checkout linked before the rename is registered as
`djalmajr.herdr-agents`; unlink it and link the checkout again so Herdr
loads `djalmajr.herdr-soho`.

The `team`, `roster` and `doctor` actions open the team panel on the
workspace currently focused in Herdr (which can differ from the invoking
shell's `HERDR_*` variables): `team` and `roster` start on the team
view, `doctor` starts on the doctor view. The panel names the resolved
workspace and cwd on its first line, and never refreshes by itself
(`r` reloads). Its only writes are `x` (release the selected worker) and
`g` (`gc --yes`), and only after their on-screen confirmation; every other
CLI call runs with `HERDR_SOHO_NOWRITE=1`.

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

### Team board

The `board` action ("Teams: every agent on every machine") opens a
board over the focused pane with every agent of every machine — local
first, the enabled machines appended as they arrive (a machine that
fails to load shows a status line, not an error). Agents are grouped
by machine and workspace, the orchestrators first within each
workspace; a line shows the state marker (`*` working, `!` blocked),
the name, the kind, the status and the task (its `<role>: ` prefix
stripped). The top line totals the agents by status and shows the time
of the last update; the board refreshes by itself every 10 s.

Type to filter (letters, digits, space and punctuation match
case-insensitively over the reference, name, kind, status, task,
workspace and tab labels, cwd and machine), `↑`/`↓` select (the
selection stays on the same session while it is still loaded), `Enter`
copies the selected session reference to the clipboard (the same text
as the picker) and closes, `Esc` or `Ctrl-C` close without copying,
`r` refreshes when the filter is empty, and `Ctrl-R` refreshes at any
time.

To bind the action to a key, add an entry to the Herdr config (any key
you like):

```toml
[[keys.command]]
key = "prefix+t" # any key
type = "plugin_action"
command = "djalmajr.herdr-soho.board"
```

### Team panel

The `team`, `roster` and `doctor` actions open the same panel over the
focused pane: `herdr-soho plugin team` (and `--view doctor` for the
doctor view). The first line is always `herdr-soho · <workspace> ·
<cwd>`; `1`–`4` (or `Tab`) switch the views, each reloads on demand
(no auto-refresh) and shows the CLI's exit code and stderr when a call
fails:

1. **team** (`equipe` on screen) — what the team is doing (`explain`, wrapped) and its
   workers: the first roster table, one line per worker, `↑`/`↓` select
   (orchestrator first as the roster gives it). `Enter` opens a
   scrollable report of the selected worker (`collect <agent> --lines
   60`, `↑`/`↓` and `PgUp`/`PgDn` scroll). `x` confirms on the panel
   (`release --close <agent> in <workspace> (<cwd>)? y/N`) and, with
   `y`, releases that worker (`--close`) and reloads the view.
2. **doctor** — the output of `herdr-soho doctor` for the focused
   workspace.
3. **resources** (`recursos` on screen) — the `gc` dry-run (pressure). `g` confirms (`gc --yes
   in <cwd>? y/N`) and, with `y`, runs `gc --yes` and reloads.
4. **friction** — `herdr-soho friction --summary`.

A confirmation runs its write only on a `y` pressed on its own after the
prompt has been on screen for 0.4 s with no other input: a paste (the
panel turns on the terminal's bracketed paste and drops pasted text) or
a key typed along with the one that opened it never confirms.

`q` closes the panel from a main view and `Ctrl-C` from any view; `Esc`
backs out of a report or a confirmation and closes a main view. Every
read call runs in the target's cwd
with the target's `HERDR_*` ids and `HERDR_SOHO_NOWRITE=1`. A focused
pane from another workspace, or whose cwd does not exist, shows the
cause on one line instead of the panel, and closes with `Esc`.

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
