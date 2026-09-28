// Legacy (herdr-agents) reads: the old env prefix, the old user/project
// config paths, the old state dir name and the doctor lines that name what
// is still legacy. A machine or project that only has the old names keeps
// working with no data moved: HERDR_AGENTS_* is read as HERDR_SOHO_*
// (applyLegacyEnv; the old variables stay, the doctor names them), a config
// layer reads the old file when the new one is absent (effectiveConfigFile)
// and the first write copies it to the new path (migrateLegacyConfigFile;
// the old file stays untouched), and the state dir defaults to .herdr-agents
// while .herdr-soho is absent. Nothing here moves or deletes legacy state.
// node:fs/node:path only (portability rule); the config import is used
// at call time, so the legacy<->config cycle is safe under Node and Bun
// (like config<->lanes).
import crypto from 'node:crypto';
import fs from 'node:fs';
import path from 'node:path';
import { homeDir, projectRoot, userConfigPath } from './platform.mjs';
import { loadConfig } from './config.mjs';

// `fs.statSync(p).isFile()` / `.isDirectory()` that swallow errors: a
// regular file or directory (symlinks followed) is true; an absent path,
// a broken symlink or the other kind is false.
function isRegularFile(p) {
  try { return fs.statSync(p).isFile(); } catch { return false; }
}
function isDirectory(p) {
  try { return fs.statSync(p).isDirectory(); } catch { return false; }
}

// The (sorted) old names copied by the last applyLegacyEnv call; the
// doctor's source for its "rename it" lines (the module is imported once
// per process, so the last call is the entry's).
let legacyCopied = [];
export function legacyEnvCopied() {
  return legacyCopied;
}

// apply_legacy_env: for every HERDR_AGENTS_<R> with a non-empty value,
// HERDR_SOHO_<R> takes it when unset or empty (the non-empty new value
// wins and nothing is copied); the old variable is never removed. Returns
// the sorted list of the old names copied (legacyEnvCopied() keeps it).
// The exact prefix HERDR_AGENTS_ is required: HERDR_AGENTS or a name with
// no underscore after AGENTS (HERDR_AGENTSX) is left alone.
export function applyLegacyEnv(env = process.env) {
  const copied = [];
  for (const name of Object.keys(env)) {
    if (!name.startsWith('HERDR_AGENTS_')) continue;
    const old = env[name];
    if (old === undefined || old === '') continue;
    const fresh = `HERDR_SOHO_${name.slice('HERDR_AGENTS_'.length)}`;
    if (env[fresh] === undefined || env[fresh] === '') {
      env[fresh] = old;
      copied.push(name);
    }
  }
  copied.sort();
  legacyCopied = copied;
  return copied;
}

// The old user config file: userConfigPath with the old directory —
// $XDG_CONFIG_HOME/herdr-agents/config when set (any platform), otherwise
// %APPDATA%\herdr-agents\config or
// %USERPROFILE%\AppData\Roaming\herdr-agents\config on Windows, else
// ~/.config/herdr-agents/config.
export function legacyUserConfigPath(platform = process.platform, env = process.env) {
  if (env.XDG_CONFIG_HOME) return path.join(env.XDG_CONFIG_HOME, 'herdr-agents', 'config');
  if (platform === 'win32') {
    if (env.APPDATA) return path.join(env.APPDATA, 'herdr-agents', 'config');
    return path.join(homeDir(platform, env), 'AppData', 'Roaming', 'herdr-agents', 'config');
  }
  return path.join(homeDir(platform, env), '.config', 'herdr-agents', 'config');
}

// The old project config file.
export function legacyProjectConfigPath(root) {
  return path.join(root, '.agents', 'herdr-agents.conf');
}

export function legacyStatePath(root, pathApi = path) {
  return pathApi.join(root, '.herdr-agents');
}

// The config file a layer actually reads: the new file when it is a
// regular file, else the legacy file when it is, else the new one (a
// write target for a layer that reads nothing yet).
export function effectiveConfigFile(newPath, legacyPath) {
  if (isRegularFile(newPath)) return newPath;
  if (isRegularFile(legacyPath)) return legacyPath;
  return newPath;
}

// migrate_legacy_config_file <dest>: when `dest` is the new user or project
// config path, `dest` does not exist and the matching legacy file is a
// regular file (or a symlink to one), the legacy content is copied byte for byte to a temp file
// in `dest`'s directory and renamed onto `dest` with the legacy mode
// (the atomicWrite rule: same-directory temp + rename, nothing removed),
// a warning names the copy on stderr, and true is returned. Any other case
// is a no-op with false. The legacy file is never removed or changed — it
// is simply no longer read once the new one exists.
export function migrateLegacyConfigFile(dest, env = process.env, cwd = process.cwd()) {
  const root = projectRoot(env, cwd);
  const legacy = dest === userConfigPath(process.platform, env)
    ? legacyUserConfigPath(process.platform, env)
    : dest === path.join(root, '.agents', 'herdr-soho.conf')
      ? legacyProjectConfigPath(root)
      : '';
  if (legacy === '') return false;
  try { fs.lstatSync(dest); return false; } catch { /* absent: the copy target */ }
  // statSync follows a symlinked legacy file, like the reading side
  // (effectiveConfigFile): its target's bytes and mode are copied into a
  // regular file at the new path, and the link itself stays untouched.
  let st;
  try { st = fs.statSync(legacy); } catch { return false; }
  if (!st.isFile()) return false;
  fs.mkdirSync(path.dirname(dest), { recursive: true });
  const tmp = path.join(path.dirname(dest), `.${path.basename(dest)}.${process.pid}.${crypto.randomBytes(4).toString('hex')}.tmp`);
  try {
    fs.copyFileSync(legacy, tmp);
    fs.chmodSync(tmp, st.mode & 0o777);
    fs.renameSync(tmp, dest);
  } catch (err) {
    try { fs.rmSync(tmp, { force: true }); } catch { /* best effort */ }
    throw err;
  }
  process.stderr.write(`herdr-soho: warning: copied legacy config ${legacy} to ${dest}; the legacy file is no longer read\n`);
  return true;
}

// default_state_dir_name: the legacy state dir keeps working while the
// project has only it — `.herdr-agents` when `<root>/.herdr-soho` is absent
// and `<root>/.herdr-agents` is a directory, `.herdr-soho` otherwise.
export function defaultStateDirName(root) {
  if (!fs.existsSync(path.join(root, '.herdr-soho')) && isDirectory(path.join(root, '.herdr-agents'))) return '.herdr-agents';
  return '.herdr-soho';
}

// legacy_doctor_warnings: the doctor lines (without the `warn` prefix) for
// what is still legacy, in this order, only the ones that apply:
//   - one per copied env variable (applyLegacyEnv's remembered list);
//   - the user config: in use when the legacy file is the effective one,
//     "no longer read" when both exist;
//   - the project config: the same two phrases with `project config`,
//     absolute .agents/… paths and 'config set';
//   - the state dir: in use under the defaultStateDirName rule with no
//     state_dir/HERDR_SOHO_DIR defined, "no longer used" when both
//     directories exist.
export function legacyDoctorWarnings({ env = process.env, cwd = process.cwd(), platform = process.platform } = {}) {
  const out = [];
  for (const name of legacyEnvCopied()) {
    out.push(`legacy environment variable ${name} is read as HERDR_SOHO_${name.slice('HERDR_AGENTS_'.length)}; rename it`);
  }
  const root = projectRoot(env, cwd);
  const newUser = userConfigPath(platform, env);
  const legacyUser = legacyUserConfigPath(platform, env);
  const newProject = path.join(root, '.agents', 'herdr-soho.conf');
  const legacyProject = legacyProjectConfigPath(root);
  if (isRegularFile(legacyUser)) {
    if (isRegularFile(newUser)) out.push(`legacy user config ${legacyUser} is no longer read (${newUser} exists); remove it once you no longer need it`);
    else out.push(`legacy user config in use: ${legacyUser} (the next 'config set --user' copies it to ${newUser})`);
  }
  if (isRegularFile(legacyProject)) {
    if (isRegularFile(newProject)) out.push(`legacy project config ${legacyProject} is no longer read (${newProject} exists); remove it once you no longer need it`);
    else out.push(`legacy project config in use: ${legacyProject} (the next 'config set' copies it to ${newProject})`);
  }
  // "Defined" = a non-empty HERDR_SOHO_DIR / HERDR_SOHO_STATE_DIR env
  // var or a state_dir entry a layer other than the defaults file holds
  // (the defaults file names the new dir but is not a choice: the legacy
  // state dir stays the effective default while the project has only it).
  const ctx = loadConfig(env, cwd);
  const entry = ctx.entries.get('state_dir');
  const statePinned = Boolean(env.HERDR_SOHO_DIR) || Boolean(env.HERDR_SOHO_STATE_DIR)
    || (entry !== undefined && entry.source !== 'defaults' && entry.value !== '');
  if (!statePinned && defaultStateDirName(root) === '.herdr-agents') {
    out.push(`legacy state dir in use: ${legacyStatePath(root)} (once no worker is live, rename it to .herdr-soho and ignore .herdr-soho/ in git)`);
  }
  if (isDirectory(path.join(root, '.herdr-agents')) && isDirectory(path.join(root, '.herdr-soho'))) {
    out.push(`legacy state dir ${legacyStatePath(root)} is no longer used (${path.join(root, '.herdr-soho')} exists); clean it once its reports are no longer needed`);
  }
  return out;
}
