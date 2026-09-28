// The `send` command (S4): deliver a message to an agent of any kind —
// claude, codex, cursor, grok, agy, pi, opencode — local or on another
// machine, without manual approval. The message is always prefixed with the
// peer header (lib/peer.mjs) that names the sender as another agent.
//
// Target: a `[machine/]<pane id>` reference (local/w12:p1, windows/w3:p1,
// w12:p1) or an agent name on the local server (soho-s1), resolved with
// `herdr [--machine m] agent get <pane id|name>`.
//
// Message: the words after the target joined with a space, or `--file
// <path>` (the file content, trailing newlines trimmed) — never both. The
// body and the sender fields are scrubbed (literalPeerText) of the bytes a
// terminal would run as keystrokes before the text is assembled.
//
// Delivery: an idle/done/unknown target gets the prompt at once; a
// working/blocked target is waited on (`agent wait --until idle --until
// done --timeout MS`, default 600000) unless `--now`. The prompt goes
// through `agent prompt --wait … --timeout 15000`; agent_prompt_stalled /
// agent_blocked / the receipt-wait timeout are not-received (a timeout is
// not a delivery) with no automatic resend.
//
// Policy: for EVERY local target the `inbound` key (auto | off) of the
// target's project is read in the target's directory with the target's
// session layer and none of the sender's HERDR_SOHO_*/HERDR_AGENTS_*
// variables; off refuses before anything is sent. When the target's cwd is
// empty the read happens in a fresh empty directory (no project): the user
// layer and the defaults still apply, and the sender's project can never
// stand in for the target's. A remote target's policy is not consulted (the
// sending machine cannot read the remote project) — the documented
// limitation.
//
// Every attempt appends one line to <state>/peer-messages.tsv (ts from to
// result chars; HERDR_SOHO_NOWRITE=1 skips it).
//
// Exit codes: 0 sent, 2 usage, 4 herdr/target unavailable, 15 the target
// did not take the message, 17 the target is still busy after the wait
// timeout (nothing sent), 18 refused by the target's inbound=off.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { die, readTextFile } from '../platform.mjs';
import { stateDirPath } from '../state.mjs';
import { LOCAL_MACHINE } from '../sessionref.mjs';
import {
  appendPeerLog, DEFAULT_SEND_TIMEOUT_MS, deliverPrompt, inboundPolicy,
  literalPeerText, peerHeader, resolveTarget, senderInfo, senderRefOf,
  waitUntilIdle,
} from '../peer.mjs';

export function cmdSend(argv, ctx, env = process.env, cwd = process.cwd()) {
  // Argument parse: the first positional is the target; flags may sit
  // anywhere; the remaining positionals are the message words.
  let target = '';
  let now = false;
  let file = '';
  let timeoutMs = DEFAULT_SEND_TIMEOUT_MS;
  const words = [];
  for (let i = 0; i < argv.length; i += 1) {
    const a = argv[i];
    if (a === '--now') { now = true; continue; }
    if (a === '--file' || a === '--timeout') {
      const v = argv[i + 1];
      if (v === undefined || v === '' || v.startsWith('--')) die(`send: ${a} expects a value`, 2);
      if (a === '--file') file = v;
      else {
        const n = Number(v);
        if (!Number.isInteger(n) || n <= 0) die('send: --timeout expects the wait in milliseconds (a positive integer)', 2);
        timeoutMs = n;
      }
      i += 1;
      continue;
    }
    if (a.startsWith('--')) die(`send: unknown option '${a}'`, 2);
    if (target === '') { target = a; continue; }
    words.push(a);
  }
  if (target === '') die('usage: send <ref|name> <message…> | send <ref|name> --file <path> [--now] [--timeout MS]', 2);
  // The message body: the words joined with a space, or the file content
  // (trailing newlines trimmed). Empty (neither, or an empty file) → 2.
  let body = '';
  if (file !== '') {
    if (words.length > 0) die('send: use either the message words or --file, not both', 2);
    let raw;
    try { raw = readTextFile(file); } catch { die(`send: cannot read --file '${file}'`, 2); }
    body = raw.replace(/\n+$/, '');
  } else {
    body = words.join(' ');
  }
  if (body === '') die('send: empty message (pass the message words or --file <path>)', 2);

  // The attempt log lives in the sender's state dir (its workspace).
  const sd = stateDirPath(ctx, env, cwd);
  const log = (fromRef, toRef, result) => appendPeerLog(sd, fromRef, toRef, result, body.length, env);

  // 1. Resolve the target (herdr agent get; the machine from the ref).
  const t = resolveTarget(target, env);
  const ref = t.refShown;
  if (!t.ok) {
    log(senderRefOf(env), ref, t.code === 'agent_not_found' ? 'no-agent' : 'error');
    if (t.code === 'agent_not_found') die(`send: no agent in ${ref}`, 4);
    die(`send: ${ref} unavailable: ${t.cause}`, 4);
  }

  // 2. The inbound policy: the target project's rule, consulted BEFORE any
  // wait or send — for EVERY local target (a remote target's policy is not
  // consulted; the sending machine cannot read the remote project). When
  // the target's cwd is empty (agent get gave no usable string), the read
  // happens in a fresh empty directory (no project): the user layer and
  // the defaults still apply, and the sender's project can never stand in
  // for the target's.
  if (t.machine === LOCAL_MACHINE) {
    let policyCwd = t.cwd;
    let policyTmp = '';
    if (policyCwd === '') {
      policyTmp = fs.mkdtempSync(path.join(os.tmpdir(), 'ha-send-policy-'));
      policyCwd = policyTmp;
    }
    let policy;
    try {
      policy = inboundPolicy(policyCwd, t.workspaceId, env);
    } finally {
      if (policyTmp !== '') fs.rmSync(policyTmp, { recursive: true, force: true });
    }
    if (policy === 'off') {
      log(senderRefOf(env), ref, 'refused');
      die(`send: ${ref} does not accept peer messages (inbound=off)`, 18);
    }
  }

  // 3. A busy target settles first (idle or done), unless --now skips the
  // wait (the target's own CLI decides queue vs mix).
  if (!now && (t.status === 'working' || t.status === 'blocked')) {
    const w = waitUntilIdle(t.machine, t.targetArg, timeoutMs, env);
    if (w.settled) {
      // The wait matched idle or done: proceed to the prompt.
    } else if (w.code === 'timeout') {
      // Re-read the state for the message (a failed re-read keeps the
      // status that put the wait in motion); nothing was sent.
      const again = resolveTarget(target, env);
      log(senderRefOf(env), ref, 'busy');
      die(`send: ${ref} is still ${again.ok ? again.status : t.status} after ${timeoutMs / 1000}s; nothing was sent`, 17);
    } else {
      log(senderRefOf(env), ref, 'error');
      die(`send: ${ref} unavailable: ${w.cause}`, 4);
    }
  }

  // 4. Deliver: header + blank line + body, through agent prompt --wait.
  // The body and the sender fields pass through literalPeerText so no byte
  // the target's terminal would run as a keystroke (CR, ESC, DEL, the
  // bracketed-paste markers, the other control characters) reaches it; \n
  // and \t stay. The header always comes first.
  const sender = senderInfo(ctx, env, cwd);
  const text = `${peerHeader(literalPeerText(sender.ref), literalPeerText(sender.name), literalPeerText(sender.kind), literalPeerText(sender.role))}\n\n${literalPeerText(body)}`;
  const p = deliverPrompt(t.machine, t.targetArg, text, env);
  if (p.ok) {
    log(sender.ref, ref, 'sent');
    process.stdout.write(`sent to ${ref}\n`);
    return 0;
  }
  if (p.code === 'agent_prompt_stalled' || p.code === 'agent_blocked' || p.code === 'timeout') {
    // No automatic resend: a second shot may duplicate the first, and the
    // receipt timeout does not prove the text never reached the pane.
    const result = p.code === 'agent_prompt_stalled' ? 'stalled' : p.code === 'agent_blocked' ? 'blocked' : 'timeout';
    log(sender.ref, ref, result);
    die(`send: ${ref} did not take the message (${p.code === 'timeout' ? 'timeout' : p.cause}); read its pane before sending again`, 15);
  }
  log(sender.ref, ref, 'error');
  die(`send: ${ref} unavailable: ${p.cause}`, 4);
}
