// Codex environment policy and ancestry diagnostics (Decision 1 & Decision 2).
// When HERDR_ENV != 1, distinguishes a command run by Codex inside Herdr
// where shell_environment_policy dropped HERDR_*, from truly running outside Herdr.
import fs from 'node:fs';
import path from 'node:path';
import { findExecutable, homeDir, runCli } from './platform.mjs';

// Strip inline TOML comment from a line, respecting single and double quotes.
export function stripTomlComment(line) {
  let inDQuote = false;
  let inSQuote = false;
  for (let i = 0; i < line.length; i++) {
    const ch = line[i];
    if (ch === '"' && !inSQuote && (i === 0 || line[i - 1] !== '\\')) {
      inDQuote = !inDQuote;
    } else if (ch === "'" && !inDQuote) {
      inSQuote = !inSQuote;
    } else if (ch === '#' && !inDQuote && !inSQuote) {
      return line.slice(0, i);
    }
  }
  return line;
}

// Minimal TOML section parser for [shell_environment_policy] only.
// Returns null if the section is absent or content is invalid.
// Never extracts or retains content from any other section (may contain tokens).
export function parseCodexPolicy(content) {
  if (typeof content !== 'string') return null;
  const lines = content.split('\n');
  let insidePolicy = false;
  const rawSectionLines = [];

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i];
    const headerMatch = rawLine.match(/^\s*\[([^\]]+)\]/);
    if (headerMatch) {
      const sectionName = headerMatch[1].trim();
      if (sectionName.toLowerCase() === 'shell_environment_policy') {
        insidePolicy = true;
        rawSectionLines.length = 0;
        continue;
      } else if (insidePolicy) {
        break;
      }
    }
    if (insidePolicy) {
      rawSectionLines.push(rawLine);
    }
  }

  if (!insidePolicy && rawSectionLines.length === 0) {
    return null;
  }

  let inherit = null;
  let include_only = null;
  let exclude = null;

  for (let i = 0; i < rawSectionLines.length; i++) {
    const line = stripTomlComment(rawSectionLines[i]).trim();
    if (!line) continue;
    const kvMatch = line.match(/^([a-zA-Z0-9_-]+)\s*=\s*(.*)$/);
    if (!kvMatch) continue;
    const key = kvMatch[1].toLowerCase();
    const valPart = kvMatch[2].trim();

    if (key === 'inherit') {
      const strMatch = valPart.match(/^(?:"([^"]*)"|'([^']*)')/);
      if (strMatch) {
        inherit = strMatch[1] ?? strMatch[2];
      } else {
        inherit = valPart.split(/\s+/)[0];
      }
    } else if (key === 'include_only' || key === 'exclude') {
      if (valPart.startsWith('[')) {
        let arrayText = valPart;
        while (!arrayText.includes(']') && i + 1 < rawSectionLines.length) {
          i++;
          const nextLine = stripTomlComment(rawSectionLines[i]);
          arrayText += '\n' + nextLine;
        }
        const strings = [];
        const strRegex = /(?:"([^"\\]*(?:\\.[^"\\]*)*)"|'([^'\\]*(?:\\.[^'\\]*)*)')/g;
        let m;
        while ((m = strRegex.exec(arrayText)) !== null) {
          strings.push(m[1] ?? m[2]);
        }
        if (key === 'include_only') include_only = strings;
        else exclude = strings;
      } else {
        const strMatch = valPart.match(/^(?:"([^"]*)"|'([^']*)')/);
        if (strMatch) {
          const val = strMatch[1] ?? strMatch[2];
          if (key === 'include_only') include_only = [val];
          else exclude = [val];
        }
      }
    }
  }

  return { inherit, include_only, exclude };
}

// Case-insensitive glob matching with `*` (matching Codex behavior).
export function globMatch(pattern, text) {
  if (typeof pattern !== 'string' || typeof text !== 'string') return false;
  const esc = pattern.replace(/[.+^${}()|[\]\\]/g, '\\$&').replace(/\*/g, '.*');
  return new RegExp(`^${esc}$`, 'i').test(text);
}

// Evaluate whether [shell_environment_policy] drops HERDR_ENV.
// Returns { drops: boolean, reason: string }.
export function evaluateCodexPolicy(policy) {
  if (!policy) return { drops: false, reason: '' };

  const { inherit, include_only, exclude } = policy;

  if (Array.isArray(exclude) && exclude.some((pat) => globMatch(pat, 'HERDR_ENV'))) {
    return { drops: true, reason: 'exclude matches HERDR_*' };
  }

  const hasInclude = Array.isArray(include_only) && include_only.length > 0;
  const includeMatches = hasInclude && include_only.some((pat) => globMatch(pat, 'HERDR_ENV'));
  if (hasInclude && !includeMatches) {
    return { drops: true, reason: 'include_only without HERDR_*' };
  }

  const inheritNorm = (inherit ?? '').trim().toLowerCase();
  if ((inheritNorm === 'core' || inheritNorm === 'none') && !includeMatches) {
    return { drops: true, reason: `inherit="${inheritNorm}"` };
  }

  return { drops: false, reason: '' };
}

// Reads Codex configuration from ${CODEX_HOME:-~/.codex}/config.toml.
// Returns parsed policy or null if absent or invalid.
export function readCodexPolicy(env = process.env, platform = process.platform) {
  const confPath = env.CODEX_HOME
    ? path.join(env.CODEX_HOME, 'config.toml')
    : path.join(homeDir(platform, env), '.codex', 'config.toml');
  try {
    const content = fs.readFileSync(confPath, 'utf8');
    return parseCodexPolicy(content);
  } catch {
    return null;
  }
}

// Decision 1: doctor check warning for Codex shell_environment_policy.
export function doctorCodexPolicyWarnings(env = process.env, say, platform = process.platform) {
  const policy = readCodexPolicy(env, platform);
  if (!policy) return;
  const evaluation = evaluateCodexPolicy(policy);
  if (evaluation.drops) {
    say.warn(`codex: shell_environment_policy drops HERDR_* (${evaluation.reason}): commands Codex runs cannot see Herdr; see the Codex section of docs/guide.md`);
  }
}

// Climb process ancestry up to 64 levels via ps (or /proc on Linux).
// Returns an array of { pid, name } for ancestors.
export function getProcessAncestors(startPid = process.pid, env = process.env, platform = process.platform) {
  const ancestors = [];
  let curr = startPid;
  for (let depth = 0; depth < 64; depth++) {
    if (!curr || curr <= 1) break;
    let ppid = null;
    let comm = null;

    if (findExecutable('ps', env, platform)) {
      const res = runCli('ps', ['-o', 'ppid=,comm=', '-p', String(curr)], { env, platform, timeoutMs: 3000 });
      if (res.status === 0 && res.stdout) {
        const line = res.stdout.trim().split('\n')[0]?.trim();
        if (line) {
          const match = line.match(/^([0-9]+)\s+(.+)$/);
          if (match) {
            ppid = Number(match[1]);
            comm = match[2].trim();
          }
        }
      }
    }

    if (ppid === null && platform === 'linux') {
      try {
        const stat = fs.readFileSync(`/proc/${curr}/stat`, 'utf8');
        const match = stat.match(/^([0-9]+)\s+\((.+)\)\s+\S+\s+([0-9]+)/);
        if (match) {
          comm = match[2];
          ppid = Number(match[3]);
        }
      } catch {
        // ignore
      }
    }

    if (ppid === null || comm === null) break;
    const name = path.basename(comm).replace(/^-/, '');
    if (curr !== startPid) {
      ancestors.push({ pid: curr, name });
    }
    if (ppid <= 1 || ppid === curr) {
      if (ppid === 1 && curr !== 1) {
        ancestors.push({ pid: 1, name: 'init' });
      }
      break;
    }
    curr = ppid;
  }
  return ancestors;
}

// Decision 2: Runtime ancestry check when HERDR_ENV != 1.
// Outside Windows and with herdr in PATH:
// - climbs ancestors; if no codex ancestor, returns baseMessage without calling Herdr.
// - with codex ancestor, queries herdr api snapshot and herdr pane process-info --pane <id>.
// - exactly one match returns the specific message with local/<pane>.
// - zero or multiple matches returns baseMessage with the suffix.
// - any failure of ps or herdr falls back to baseMessage.
export function diagnoseOutsideHerdr(baseMessage, env = process.env, platform = process.platform, opts = {}) {
  if (platform === 'win32' || !findExecutable('herdr', env, platform)) {
    return baseMessage;
  }

  const ancestors = typeof opts.getAncestors === 'function'
    ? opts.getAncestors()
    : getProcessAncestors(opts.pid ?? process.pid, env, platform);

  const hasCodexAncestor = ancestors.some((a) => a && typeof a.name === 'string' && a.name.toLowerCase() === 'codex');
  if (!hasCodexAncestor) {
    return baseMessage;
  }

  const snapRes = runCli('herdr', ['api', 'snapshot'], { env, platform, timeoutMs: 10000 });
  if (snapRes.status !== 0 || !snapRes.stdout) {
    return baseMessage;
  }

  let snapData;
  try {
    snapData = JSON.parse(snapRes.stdout);
  } catch {
    return baseMessage;
  }

  const panes = snapData?.result?.snapshot?.panes ?? snapData?.result?.panes;
  if (!Array.isArray(panes)) {
    return baseMessage;
  }

  const codexPanes = panes.filter((p) => p && (p.agent ?? '').toLowerCase() === 'codex' && !p.remote && !p.machine);

  const ancestorPids = new Set(ancestors.map((a) => a.pid));
  if (opts.pid != null) ancestorPids.add(opts.pid);
  else ancestorPids.add(process.pid);

  const matchedPanes = [];
  for (const pane of codexPanes) {
    if (!pane.pane_id) continue;
    const infoRes = runCli('herdr', ['pane', 'process-info', '--pane', String(pane.pane_id)], { env, platform, timeoutMs: 10000 });
    if (infoRes.status !== 0 || !infoRes.stdout) continue;
    let infoData;
    try {
      infoData = JSON.parse(infoRes.stdout);
    } catch {
      continue;
    }
    const fg = infoData?.result?.process_info?.foreground_processes;
    if (Array.isArray(fg) && fg.some((proc) => proc && ancestorPids.has(proc.pid))) {
      matchedPanes.push(pane);
    }
  }

  if (matchedPanes.length === 1) {
    const pane = matchedPanes[0];
    const paneId = String(pane.pane_id);
    const paneLabel = paneId.startsWith('local/') ? paneId : `local/${paneId}`;
    return `this command runs under Codex in Herdr pane ${paneLabel} (matched by process ancestry), but Codex's shell_environment_policy does not pass HERDR_*; allow them (see herdr-soho doctor) and restart Codex`;
  }

  return `${baseMessage} (a Codex ancestor was found, but no single Herdr pane matched it)`;
}
