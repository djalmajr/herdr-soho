// The help text for `help`, `-h`, `--help` and no command (port decision 8):
// exactly what the bash `usage()` printed — the script header (lines 2 up
// to the line before `set -euo pipefail`) with the leading `#` stripped.
// It is a constant on purpose, and since the switch to JS it is the source
// of the help text (the program is `herdr-soho`, launched by
// `scripts/herdr-soho`). A change here is a
// user-visible change: update test/golden/parity-entry.json with
// HERDR_SOHO_GOLDEN=update and review the diff.

export const USAGE = `herdr-soho — role-agent layer over the \`herdr\` CLI.

Roles are markdown files with frontmatter (roles/<role>.md, overridable per
project in .agents/herdr-roles/<role>.md). This script spawns one CLI agent
per role in a Herdr pane, dispatches a composed prompt (role body + brief),
and detects completion through a file-based report contract. It never
commits, pushes, or closes panes it did not create.

Usage:
  herdr-soho init                          # doctor + name the caller \`orchestrator\`, print context
  herdr-soho title "<objective>" | title --clear   # this pane's title: orchestrator: <objective>
  herdr-soho doctor [--fix] [--panes 2|3|4] [--user|--session]
                                             # advisory check; --fix normalizes lanes in the project file
  herdr-soho explain                       # plain text for a person: what is running, or how to start
  herdr-soho setup [--local | --target FILE] [--no-hooks] [--dry-run]
                     [--detect | --probe [--kind K --model M] [--timeout S]
                      | --plan [--panes 2|3|4] [--lane name=kind[:model[:effort]]]
                        [--set K V] [--user-set K V] [--session-set K V]]
                     [--panes 2|3|4] [--lane name=kind[:model[:effort]]]
                                             # write the block + hooks; --local uses CLAUDE.local.md and Git info/exclude; --detect/--probe/--plan print and write nothing
  herdr-soho roles | kinds
  herdr-soho config [set <key> <value> [--project|--user]]
  herdr-soho session [set <key> <value> | clear [key] | show]
                                             # this-session overrides in <state>/session.conf
  herdr-soho models <kind>                 # ids the CLI lists, newest first
  herdr-soho model <kind> <spec> [effort]  # how a model spec resolves
  herdr-soho regrid                        # exact grids: caller tab (layout=split) + every herd tab
  herdr-soho tab-label [<text>] [--tab ID] [--auto]
                                             # list herd tabs / pin a tab's label / back to automatic
  herdr-soho layout-plan [--layout FILE] [--me P] [--mine "P…"]
                                             # where the next split-layout spawn would go, and why
  herdr-soho role <name>
  herdr-soho spawn <role> [--name N] [--kind K] [--direction right|down]
                      [--ratio F] [--cwd DIR] [--pane ID] [--timeout MS]
                      [--effort low|medium|high|xhigh|max] [--model M]
                      [--approvals ask|edits|full] [--reuse|--fresh]
                      [--tab-label TEXT] [-- <native agent args>]
  herdr-soho env                          # environment block for a feedback issue
  herdr-soho lint <brief.md> [--role <role>]  # check the dispatch brief diagnostics without dispatching
  herdr-soho send <ref|name> <message…> | --file <path> [--now] [--timeout MS]
                                             # peer message to an agent of any kind (a reference, or a name on the local server); waits for a busy target to settle by default; the target project's inbound=off refuses (exit 18)
  herdr-soho dispatch <agent> <brief.md> [--role R] [--timeout MS]
                      [--no-wait] [--allow-same-family] [--amend] [--for <author>[,…]]
                                             # --amend sends <file> as an amendment to the agent's current brief, with a new report that wait watches
  herdr-soho wait <agent>... [--timeout MS] [--any]
  herdr-soho status <agent>...             # gone = agent_not_found; unavailable = agent get failed (exit 4)
  herdr-soho collect <agent> [--lines N] [--verify]
                                             # --verify checks the sha256 lines of the last report (exit 16 on changed/missing)
  herdr-soho stats [--since <date>] [--by role|kind|model|agent|effort] [--json]  # tasks, times and review findings; --by groups results
  herdr-soho run <role> <brief.md> [spawn/dispatch options] [-- <agent args>]
  herdr-soho roster
  herdr-soho release <agent> [--close] [--force]
  herdr-soho clean [--older-than DAYS]
  herdr-soho friction                     # every error/warning of this workspace
  herdr-soho friction add "<text>" [--brief <path>]  # record one friction note (level note, command friction)
  herdr-soho feedback send <report.md> "<one-line summary>"
                                             # feedback=local: file the report in feedback_dir; one line to feedback_to when set
  herdr-soho mutation-guard <copy-dir> [--source <dir>] [--env NAME]...

Completion contract: a worker is finished when its report file exists. Use
\`dispatch\` (waits by default), \`wait\` (one or many agents), or \`status\`
(non-blocking). Never poll \`herdr agent get\` state alone: integrations
report idle/done mid-task.

Configuration (key=value files; later layers win, env wins over files,
flags win over env):
  <skill>/config.defaults → ~/.config/herdr-soho/config
  → <repo>/.agents/herdr-soho.conf → <state>/session.conf (session)
  → HERDR_SOHO_<KEY> → flags

Exit codes: 2 usage/env · 3 unknown role/agent · 4 Herdr failure (includes
\`herdr agent get\` transport/permission errors reported as \`unavailable\`) ·
5 same-family reviewer · 6 settled without report or agent really gone ·
7 agent blocked (startup or approval) or asked a question · 8 max_workers reached ·
9 wait timeout · 10 lane busy · 11 quota exhausted · 12 planner is the orchestrator ·
13 lane kind-mismatch (set lane.<name>.kind, or release the lane) ·
14 provider error or capacity · 15 prompt not received ·
16 collect --verify: a reported file changed or is missing ·
17 send: the target is still busy after the wait timeout; nothing was sent ·
18 send: the target project refuses peer messages (inbound=off).
`;

// Print the help text to stdout (bash `usage`; exit 0, no Herdr needed).
export function printUsage() {
  process.stdout.write(USAGE);
}
