#!/usr/bin/env node
// herdr-soho plugin — clipboard copy with native tools and an OSC 52
// fallback (slice S3, used by picker.mjs).
//
// Native tool per platform, in the same order as Herdr's own fallback
// chain (herdr v0.9.1 sources, src/platform/{macos,linux,windows}.rs):
//   darwin:  pbcopy
//   win32:   powershell (ReadToEnd on the stdin; clip.exe is not used:
//             it mangles UTF-8). The command itself reads the stdin —
//             [Console]::In.ReadToEnd() — with UTF-8 input encoding,
//             so the text never appears on the command line (no quote
//             or $ mangling): powershell -NoProfile -Command
//             "[Console]::InputEncoding=[Text.Encoding]::UTF8;
//              Set-Clipboard -Value ([Console]::In.ReadToEnd())"
//   linux:   wl-copy, then xclip -selection clipboard,
//            then xsel --clipboard --input
// A tool that is missing or fails yields the next tool; when no tool can
// copy, the text is written as OSC 52 (ESC ] 52 ; c ; <base64> BEL) to the
// picker's own terminal, which Herdr forwards to the user's terminal
// (the server decodes OSC 52 from pane output; herdr v0.9.1
// src/ghostty/mod.rs + src/selection.rs). The base64 encoding is
// UTF-8-safe in every case.
//
// copyText is synchronous (the picker copies once, on Enter) and returns
// { path } — the tool that wrote the text ('pbcopy', 'powershell',
// 'wl-copy', 'xclip', 'xsel') or 'osc52'.
import fs from 'node:fs';
import path from 'node:path';
import { spawnSync } from 'node:child_process';
import { cmdInvocation } from '../skills/herdr-soho/scripts/lib/platform.mjs';

// Ceiling per clipboard tool attempt. The tools are local and fast; a
// stuck tool must not hold the picker's exit, so a failure (including
// the timeout) moves on to the next tool / the OSC 52 fallback.
export const COPY_TIMEOUT_MS = 10_000;

// The candidate invocations per platform, in fallback order. Every
// candidate receives the text on stdin.
export function toolCandidates(platform) {
  switch (platform) {
    case 'darwin':
      return [['pbcopy']];
    case 'win32':
      return [['powershell', '-NoProfile', '-Command',
        '[Console]::InputEncoding=[Text.Encoding]::UTF8; Set-Clipboard -Value ([Console]::In.ReadToEnd())']];
    default:
      return [
        ['wl-copy'],
        ['xclip', '-selection', 'clipboard'],
        ['xsel', '--clipboard', '--input'],
      ];
  }
}

// Executable suffixes that turn a bare tool name into a file on the
// platform (Windows PATHEXT semantics); tried per PATH directory.
function exeSuffixes(platform) {
  return platform === 'win32' ? ['', '.exe', '.cmd', '.bat'] : [''];
}

// First candidate whose executable exists on the given env's PATH.
// Returns { args, full } (full = resolved path) or null.
export function findTool(args, { env = process.env, platform = process.platform } = {}) {
  const pathKey = Object.keys(env).find((k) => k === 'PATH' || k === 'Path') ?? 'PATH';
  const dirs = String(env[pathKey] ?? '').split(path.delimiter).filter((d) => d !== '');
  for (const dir of dirs) {
    for (const suffix of exeSuffixes(platform)) {
      const full = path.join(dir, args[0] + suffix);
      let st;
      try { st = fs.statSync(full); } catch { continue; }
      if (!st.isFile()) continue;
      if (platform !== 'win32' && !(st.mode & 0o111)) continue;
      return { args, full };
    }
  }
  return null;
}

// The exact OSC 52 sequence: ESC ] 52 ; c ; <base64> BEL (UTF-8 encoded).
export function osc52Sequence(text) {
  const b64 = Buffer.from(String(text), 'utf8').toString('base64');
  return `\x1b]52;c;${b64}\x07`;
}

// Copy text to the clipboard. Tries the platform's tools in order
// (existence on PATH first; a spawned failure moves to the next tool);
// when none succeeds, writes the OSC 52 sequence to `out` (the picker's
// terminal). Returns { path, bytes? }.
export function copyText(text, {
  platform = process.platform,
  env = process.env,
  out = null, // stream to receive the OSC 52 fallback (default: process.stdout)
  timeoutMs = COPY_TIMEOUT_MS,
} = {}) {
  for (const args of toolCandidates(platform)) {
    const tool = findTool(args, { env, platform });
    if (tool === null) continue;
    // A .cmd/.bat on Windows needs the cmd.exe wrapper (the skill CLI's
    // own rule), never a bare spawn or shell:true.
    const inv = platform === 'win32' && /\.(bat|cmd)$/i.test(tool.full)
      ? cmdInvocation(tool.full, args.slice(1), env)
      : { command: tool.full, args: args.slice(1), windowsVerbatimArguments: false };
    let r;
    try {
      r = spawnSync(inv.command, inv.args, {
        input: String(text),
        env,
        encoding: 'utf8',
        timeout: timeoutMs,
        killSignal: 'SIGTERM',
        stdio: ['pipe', 'ignore', 'pipe'],
        windowsVerbatimArguments: inv.windowsVerbatimArguments,
      });
    } catch {
      continue; // spawn itself failed: next tool
    }
    if (r.error) continue; // not executable / timed out: next tool
    if (r.status === 0) return { path: args[0] };
    // Non-zero exit: the tool failed; next tool.
  }
  const seq = osc52Sequence(text);
  const stream = out ?? process.stdout;
  if (stream && typeof stream.write === 'function') stream.write(seq);
  return { path: 'osc52', bytes: Buffer.byteLength(seq, 'utf8') };
}
