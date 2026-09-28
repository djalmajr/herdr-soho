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

// Parse key names only from a TOML inline table { ... }.
// Returns null on duplicate keys or malformed syntax.
// Never extracts or retains values.
// True once the inline table that opens the text has closed: its `{`
// balanced by a `}` outside quotes (a `}` inside a string value does not
// count; `\` escapes only inside double quotes, as in TOML).
function inlineTableClosed(tableText) {
  let depth = 0;
  let quote = '';
  for (let i = 0; i < tableText.length; i += 1) {
    const ch = tableText[i];
    if (quote !== '') {
      if (quote === '"' && ch === '\\') { i += 1; continue; }
      if (ch === quote) quote = '';
      continue;
    }
    if (ch === '"' || ch === "'") { quote = ch; continue; }
    if (ch === '{') depth += 1;
    else if (ch === '}') {
      depth -= 1;
      if (depth === 0) return true;
    }
  }
  return false;
}

function parseInlineTableKeys(tableText) {
  const trimmed = tableText.trim();
  if (!trimmed.startsWith('{') || !trimmed.endsWith('}')) return null;
  const inner = trimmed.slice(1, -1).trim();
  if (!inner) return [];

  const keys = [];
  const seen = new Set();
  let pos = 0;
  const len = inner.length;

  while (pos < len) {
    while (pos < len && /[\s,]/.test(inner[pos])) pos++;
    if (pos >= len) break;

    let key = '';
    if (inner[pos] === '"' || inner[pos] === "'") {
      const quote = inner[pos++];
      while (pos < len && inner[pos] !== quote) {
        if (inner[pos] === '\\' && pos + 1 < len) pos++;
        key += inner[pos++];
      }
      if (pos >= len) return null; // unclosed quote
      pos++; // skip closing quote
    } else {
      while (pos < len && /[a-zA-Z0-9_-]/.test(inner[pos])) {
        key += inner[pos++];
      }
    }
    key = key.trim();
    if (!key) return null;

    if (seen.has(key)) return null; // duplicate key in inline table
    seen.add(key);
    keys.push(key);

    while (pos < len && /\s/.test(inner[pos])) pos++;
    if (pos >= len || inner[pos] !== '=') return null;
    pos++; // skip '='

    // Skip value until ',' or end of table, respecting quotes and nested brackets/braces
    let inDQuote = false;
    let inSQuote = false;
    let braceDepth = 0;
    let bracketDepth = 0;

    while (pos < len) {
      const ch = inner[pos];
      if (ch === '"' && !inSQuote && (pos === 0 || inner[pos - 1] !== '\\')) {
        inDQuote = !inDQuote;
      } else if (ch === "'" && !inDQuote) {
        inSQuote = !inSQuote;
      } else if (!inDQuote && !inSQuote) {
        if (ch === '{') braceDepth++;
        else if (ch === '}') braceDepth--;
        else if (ch === '[') bracketDepth++;
        else if (ch === ']') bracketDepth--;
        else if (ch === ',' && braceDepth === 0 && bracketDepth === 0) {
          pos++; // skip comma
          break;
        }
      }
      pos++;
    }
  }

  return keys;
}

// Minimal TOML section parser for [shell_environment_policy] only.
// Returns null if the section is absent, content is invalid, or any key is duplicated.
// Never extracts or retains content from any other section (may contain tokens).
// Never retains or prints values of `set`.
export function parseCodexPolicy(content) {
  if (typeof content !== 'string') return null;
  const lines = content.split('\n');
  let currentSection = null; // 'policy' | 'set' | 'other'
  let hasPolicySection = false;
  let hasSetSection = false;
  const seenPolicyKeys = new Set();
  const seenSetKeys = new Set();
  let inherit = null;
  let include_only = null;
  let exclude = null;

  for (let i = 0; i < lines.length; i++) {
    const rawLine = lines[i];
    const lineWithoutComment = stripTomlComment(rawLine).trim();

    const headerMatch = lineWithoutComment.match(/^\s*\[([^\]]+)\]\s*$/);
    if (headerMatch) {
      let rawSection = headerMatch[1].trim();
      if ((rawSection.startsWith('"') && rawSection.endsWith('"')) || (rawSection.startsWith("'") && rawSection.endsWith("'"))) {
        rawSection = rawSection.slice(1, -1);
      }
      const sectionName = rawSection.toLowerCase();
      if (sectionName === 'shell_environment_policy') {
        if (hasPolicySection) return null; // duplicate table
        hasPolicySection = true;
        currentSection = 'policy';
      } else if (sectionName === 'shell_environment_policy.set') {
        if (hasSetSection || seenPolicyKeys.has('set')) return null; // duplicate table / key
        hasSetSection = true;
        currentSection = 'set';
      } else {
        currentSection = 'other';
      }
      continue;
    }

    if (!lineWithoutComment) continue;

    if (currentSection === 'set') {
      const kvMatch = lineWithoutComment.match(/^([a-zA-Z0-9_-]+|"[^"]+"|'[^']+')\s*=\s*(.*)$/);
      if (!kvMatch) continue;
      let rawKey = kvMatch[1].trim();
      if ((rawKey.startsWith('"') && rawKey.endsWith('"')) || (rawKey.startsWith("'") && rawKey.endsWith("'"))) {
        rawKey = rawKey.slice(1, -1);
      }
      if (seenSetKeys.has(rawKey)) return null; // duplicate key in set table
      seenSetKeys.add(rawKey);
      continue;
    }

    if (currentSection === 'policy') {
      const kvMatch = lineWithoutComment.match(/^([a-zA-Z0-9_-]+|"[^"]+"|'[^']+')\s*=\s*(.*)$/);
      if (!kvMatch) continue;
      let rawKey = kvMatch[1].trim();
      if ((rawKey.startsWith('"') && rawKey.endsWith('"')) || (rawKey.startsWith("'") && rawKey.endsWith("'"))) {
        rawKey = rawKey.slice(1, -1);
      }
      const key = rawKey.toLowerCase();
      if (seenPolicyKeys.has(key)) return null; // duplicate key in policy
      seenPolicyKeys.add(key);

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
          while (!arrayText.includes(']') && i + 1 < lines.length) {
            const nextWithoutComment = stripTomlComment(lines[i + 1]).trim();
            if (nextWithoutComment.match(/^\s*\[([^\]]+)\]\s*$/)) break;
            i++;
            arrayText += '\n' + stripTomlComment(lines[i]);
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
      } else if (key === 'set') {
        if (hasSetSection) return null; // duplicate set definition
        let tableText = valPart;
        if (valPart.startsWith('{')) {
          while (!inlineTableClosed(tableText) && i + 1 < lines.length) {
            const nextWithoutComment = stripTomlComment(lines[i + 1]).trim();
            if (nextWithoutComment.match(/^\s*\[([^\]]+)\]\s*$/)) break;
            i++;
            tableText += '\n' + stripTomlComment(lines[i]);
          }
          const keys = parseInlineTableKeys(tableText);
          if (keys === null) return null;
          for (const k of keys) {
            if (seenSetKeys.has(k)) return null;
            seenSetKeys.add(k);
          }
        }
      }
    }
  }

  if (!hasPolicySection && !hasSetSection) {
    return null;
  }

  return {
    inherit,
    include_only,
    exclude,
    set: Array.from(seenSetKeys),
  };
}

// Case-insensitive glob matching with `*` and `?` (matching Codex behavior).
export function globMatch(pattern, text) {
  if (typeof pattern !== 'string' || typeof text !== 'string') return false;
  const esc = pattern
    .replace(/[.+^${}()|[\]\\/]/g, '\\$&')
    .replace(/\*/g, '.*')
    .replace(/\?/g, '.');
  return new RegExp(`^${esc}$`, 'i').test(text);
}

// Evaluate whether [shell_environment_policy] drops HERDR_ENV, HERDR_PANE_ID, or HERDR_WORKSPACE_ID.
// Simulation follows Codex 0.158 order:
// inherit ("all" or omitted -> present; "core"/"none" -> absent)
// -> exclude removes matches
// -> set keys restore matches (only key names)
// -> include_only (if non-empty) removes non-matches.
// Returns { drops: boolean, reason: string }.
export function evaluateCodexPolicy(policy) {
  if (!policy) return { drops: false, reason: '' };

  const { inherit, include_only, exclude, set } = policy;
  const inheritNorm = (inherit ?? '').trim().toLowerCase();
  const setKeys = new Set(Array.isArray(set) ? set : []);
  const hasExclude = Array.isArray(exclude) && exclude.length > 0;
  const hasInclude = Array.isArray(include_only) && include_only.length > 0;

  const REQUIRED_VARS = ['HERDR_ENV', 'HERDR_PANE_ID', 'HERDR_WORKSPACE_ID'];

  for (const name of REQUIRED_VARS) {
    let present = true;
    let dropReason = '';

    // Step 1: inherit
    if (inheritNorm === 'core') {
      present = false;
      dropReason = 'inherit="core"';
    } else if (inheritNorm === 'none') {
      present = false;
      dropReason = 'inherit="none"';
    } else {
      present = true;
      dropReason = '';
    }

    // Step 2: exclude
    if (hasExclude && present) {
      if (exclude.some((pat) => globMatch(pat, name))) {
        present = false;
        dropReason = `exclude matches ${name}`;
      }
    }

    // Step 3: set
    if (setKeys.has(name)) {
      present = true;
      dropReason = '';
    }

    // Step 4: include_only (never restores what inherit/exclude did not pass)
    if (hasInclude) {
      const matched = include_only.some((pat) => globMatch(pat, name));
      if (!matched) {
        if (present) {
          present = false;
          dropReason = `include_only does not match ${name}`;
        }
      }
    }

    if (!present) {
      return { drops: true, reason: dropReason };
    }
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
