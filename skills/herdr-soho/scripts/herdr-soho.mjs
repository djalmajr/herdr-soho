#!/usr/bin/env node
// herdr-soho — JavaScript command entry.
//
// Dispatches every command of the bash script: `config [set <key> <value>
// [--project|--user]]`, `session [set <key> <value> | clear [key] | show]`,
// `roles`, `role <name>`, `kinds`, `models <kind>`, `model <kind> <spec>
// [effort]`, `spawn <role> …`, `dispatch <agent> <brief.md> …`,
// `run <role> <brief.md> …`, `lint <brief.md> [--role <role>]`,
// `find [search words]... [--machine <label>]... [--all] [--json]`,
// `status <agent>…`, `roster`, `friction`,
// `feedback send <report.md> "<one-line summary>"`, `tab-label`, `layout-plan`, `wait <agent>…`, `collect <agent>`,
// `release <agent>`, `clean`, `doctor [--fix] [--panes 2|3|4] [--user]`,
// `explain`, `init`, `title` (the orchestrator pane's current objective),
// `setup [--target FILE] [--no-hooks]
// [--dry-run] [--panes 2|3|4] [--lane name=kind[:model[:effort]]]` — its
// `--probe [--kind K --model M --timeout S]` form runs the per-kind
// probes, exclusive with `--plan` — plus `env` (the environment block for
// a feedback issue) and the help (`help`, `-h`, `--help`, or no command:
// the constant usage text of lib/usage.mjs, exit 0, no Herdr needed).
// An unknown command dies 2 like bash `main`'s `*` arm. Load the config
// layers before dispatching, like bash `main`. The living commands need
// the Herdr environment (`require_env`) and log their warnings/errors to
// <state>/friction.log, like bash `main` (layout-plan is not living, and
// requires the Herdr environment only in live mode). Decision 6:
// DieError with a message → die (friction for living commands); empty
// message → exit with the code only (bash passes herdr's own output
// through and exits with herdr's code).
import path from 'node:path';
import { loadConfig, cmdConfig, cmdConfigSet, nowrite, DieError } from './lib/config.mjs';
import { applyLegacyEnv } from './lib/legacy.mjs';
import { cmdSession } from './lib/session.mjs';
import { cmdRoles, cmdRole } from './lib/roles.mjs';
import { cmdKinds } from './lib/kinds.mjs';
import { cmdModels, cmdModel } from './lib/models.mjs';
import { requireEnv } from './lib/herdr.mjs';
import { dieFriction, setFrictionLog, stateDir } from './lib/state.mjs';
import { cmdStatus } from './lib/commands/status.mjs';
import { cmdRoster } from './lib/commands/roster.mjs';
import { cmdFriction } from './lib/commands/friction.mjs';
import { cmdFeedback } from './lib/commands/feedback.mjs';
import { cmdEnv } from './lib/commands/env.mjs';
import { printUsage } from './lib/usage.mjs';
import { cmdLayoutPlan } from './lib/layout.mjs';
import { cmdRegrid } from './lib/regrid.mjs';
import { cmdTabLabel } from './lib/herdtabs.mjs';
import { cmdSpawn } from './lib/spawn.mjs';
import { cmdWait } from './lib/wait.mjs';
import { cmdDispatch } from './lib/dispatch.mjs';
import { cmdLint } from './lib/commands/lint.mjs';
import { cmdRun } from './lib/commands/run.mjs';
import { cmdFind } from './lib/commands/find.mjs';
import { cmdCollect } from './lib/commands/collect.mjs';
import { cmdStats } from './lib/commands/stats.mjs';
import { cmdRelease } from './lib/commands/release.mjs';
import { cmdClean } from './lib/commands/clean.mjs';
import { cmdSetup } from './lib/commands/setup.mjs';
import { cmdDoctor } from './lib/commands/doctor.mjs';
import { cmdExplain } from './lib/commands/explain.mjs';
import { cmdInit } from './lib/commands/init.mjs';
import { cmdTitle } from './lib/commands/title.mjs';
import { cmdMutationGuard } from './lib/commands/mutation-guard.mjs';
import { die, findExecutable } from './lib/platform.mjs';

// The commands that log to friction when running inside Herdr (bash main's
// living-command list).
const LIVING = new Set(['spawn', 'dispatch', 'run', 'status', 'roster', 'friction', 'feedback', 'tab-label', 'regrid', 'wait', 'collect', 'release', 'clean', 'init', 'title']);

const argv = process.argv.slice(2);
const cmd = argv[0] ?? '';

const env = process.env;
// The legacy (herdr-agents) HERDR_AGENTS_* variables are read as
// HERDR_SOHO_* before the config layers load (the old variables stay).
applyLegacyEnv(env);
const ctx = loadConfig();

try {
  // HERDR_SOHO_NOWRITE=1: read-only inspection (the optional plugin's
  // doctor/roster actions). Only the exact `doctor` and `roster`
  // invocations — no extra arguments, so `doctor --fix` cannot write —
  // are allowed; everything else is rejected before any state write.
  // stateRoot skips the .gitignore entry and stateDir skips the state tree
  // (config.mjs / state.mjs), and the friction log is skipped below;
  // normal runs (without the env) are untouched.
  if (nowrite(env)) {
    const exact = (name) => cmd === name && argv.length === 1;
    if (!exact('doctor') && !exact('roster')) {
      // Name the command and count the extra arguments — never echo the
      // argument values: caller-supplied text does not enter the diagnostic.
      const shown = cmd === '' ? '(none)' : cmd;
      const extra = cmd !== '' && argv.length > 1 ? ` (${argv.length - 1} extra argument${argv.length > 2 ? 's' : ''} not allowed)` : '';
      die(`herdr-soho: HERDR_SOHO_NOWRITE=1 is read-only: only the exact 'doctor' and 'roster' invocations run (the plugin's actions); rejected: ${shown}${extra} — unset HERDR_SOHO_NOWRITE to write`, 2);
    }
  }
  if (LIVING.has(cmd)) {
    requireEnv(env);
    // FRICTION_LOG=<state>/friction.log when HERDR_ENV=1 and herdr is on
    // PATH (the jq requirement is gone, orchestrator decision 5).
    // nowrite: no friction log (a write), no stateDir call (a write).
    if (!nowrite(env) && findExecutable('herdr', env)) {
      setFrictionLog(path.join(stateDir(ctx, env), 'friction.log'), cmd);
    }
  }

  switch (cmd) {
    case 'config':
      if (argv[1] === 'set') cmdConfigSet(argv.slice(2), ctx);
      else cmdConfig(ctx);
      break;
    case 'session':
      cmdSession(argv.slice(1), ctx);
      break;
    case 'roles':
      cmdRoles();
      break;
    case 'role':
      cmdRole(argv.slice(1));
      break;
    case 'kinds':
      cmdKinds();
      break;
    case 'models':
      cmdModels(argv.slice(1));
      break;
    case 'model':
      cmdModel(argv.slice(1));
      break;
    case 'spawn':
      cmdSpawn(argv.slice(1), ctx, env);
      break;
    case 'dispatch': {
      const rc = cmdDispatch(argv.slice(1), ctx, env);
      if (rc) process.exitCode = rc;
      break;
    }
    case 'lint': {
      const rc = cmdLint(argv.slice(1), ctx, env);
      if (rc) process.exitCode = rc;
      break;
    }
    case 'find': {
      const rc = cmdFind(argv.slice(1), ctx, env);
      if (rc) process.exitCode = rc;
      break;
    }
    case 'run': {
      const rc = cmdRun(argv.slice(1), ctx, env);
      if (rc) process.exitCode = rc;
      break;
    }
    case 'status': {
      const rc = cmdStatus(argv.slice(1), ctx, env);
      if (rc) process.exitCode = rc;
      break;
    }
    case 'wait': {
      const rc = cmdWait(argv.slice(1), ctx, env);
      if (rc) process.exitCode = rc;
      break;
    }
    case 'collect': {
      const rc = cmdCollect(argv.slice(1), ctx, env);
      if (rc) process.exitCode = rc;
      break;
    }
    case 'stats': {
      const rc = cmdStats(argv.slice(1), ctx, env);
      if (rc) process.exitCode = rc;
      break;
    }
    case 'release':
      cmdRelease(argv.slice(1), ctx, env);
      break;
    case 'clean':
      cmdClean(argv.slice(1), ctx, env);
      break;
    case 'setup':
      cmdSetup(argv.slice(1), ctx, env);
      break;
    case 'doctor':
      cmdDoctor(argv.slice(1), ctx, env);
      break;
    case 'explain':
      cmdExplain(argv.slice(1), ctx, env);
      break;
    case 'init':
      cmdInit(ctx, env);
      break;
    case 'title':
      cmdTitle(argv.slice(1), ctx, env);
      break;
    case 'regrid':
      cmdRegrid(argv.slice(1), ctx, env);
      break;
    case 'roster':
      cmdRoster(ctx, env);
      break;
    case 'friction':
      cmdFriction(argv.slice(1), ctx, env);
      break;
    case 'feedback': {
      const rc = cmdFeedback(argv.slice(1), ctx, env);
      if (rc) process.exitCode = rc;
      break;
    }
    case 'tab-label':
      requireEnv(env);
      cmdTabLabel(argv.slice(1), ctx, env);
      break;
    case 'layout-plan':
      cmdLayoutPlan(argv.slice(1), ctx, env);
      break;
    case 'env':
      cmdEnv(ctx, env);
      break;
    case 'mutation-guard':
      cmdMutationGuard(argv.slice(1), env);
      break;
    // bash main: `-h|--help|help|""` → usage, exit 0, no Herdr needed.
    case 'help':
    case '-h':
    case '--help':
    case '':
      printUsage();
      break;
    default:
      // bash main: `*) die "unknown command '$cmd'" 2`.
      die(`unknown command '${cmd}'`, 2);
      break;
  }
} catch (e) {
  // Decision 6: DieError with a message dies like bash `die` (friction is
  // logged by the living commands); an empty message only exits with the
  // code — bash passed herdr's own output through already.
  if (e instanceof DieError) {
    if (e.message !== '') dieFriction(e.message, e.code);
    process.exit(e.code ?? 1);
  }
  throw e;
}
