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
  agentGet, agentReadScreen, agentSendKey, appendPeerLog, arrivalPollMs, arrivalWindowMs,
  checkIdInScreen, DEFAULT_SEND_TIMEOUT_MS, deliverPrompt, inboundPolicy,
  isDialogScreen, literalPeerText, peerEndLine, peerHeader, quotePeerBody, randomPeerId,
  resolveTarget, senderInfo, senderRefOf, sleepMs, tailLines, waitUntilIdle,
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

  // The message id: 8 random hex characters generated for this send attempt.
  const msgId = randomPeerId();

  // The attempt log lives in the sender's state dir (its workspace).
  const sd = stateDirPath(ctx, env, cwd);
  const log = (fromRef, toRef, result) => appendPeerLog(sd, fromRef, toRef, result, body.length, msgId, env);

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
  let currentStatus = t.status;
  if (!now && (t.status === 'working' || t.status === 'blocked')) {
    const w = waitUntilIdle(t.machine, t.targetArg, timeoutMs, env);
    if (w.settled) {
      currentStatus = 'idle';
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

  // Resolve sender identity for the peer header.
  const sender = senderInfo(ctx, env, cwd);

  // 4. Dialog check: before sending, inspect the target's visible screen.
  // If reading fails, exit 4 unreadable without sending (Decision 7).
  // If it matches a question dialog (blocked) or trust prompt (any status),
  // wait up to timeoutMs for it to clear. If still showing, exit 17 dialog.
  let vScreen = agentReadScreen(t.machine, t.targetArg, { source: 'visible' }, env);
  if (!vScreen.ok) {
    log(sender.ref, ref, 'unreadable');
    die(`send: could not read ${ref}'s screen (${vScreen.cause || 'read failed'}); nothing was sent`, 4);
  }
  if (isDialogScreen(vScreen.text, t.kind, currentStatus)) {
    const deadline = Date.now() + timeoutMs;
    const pollMs = arrivalPollMs(env);
    while (Date.now() < deadline && isDialogScreen(vScreen.text, t.kind, currentStatus)) {
      const remaining = deadline - Date.now();
      if (remaining <= 0) break;
      sleepMs(Math.min(pollMs, remaining));
      vScreen = agentReadScreen(t.machine, t.targetArg, { source: 'visible' }, env);
      if (!vScreen.ok) {
        log(sender.ref, ref, 'unreadable');
        die(`send: could not read ${ref}'s screen (${vScreen.cause || 'read failed'}); nothing was sent`, 4);
      }
    }
    if (isDialogScreen(vScreen.text, t.kind, currentStatus)) {
      log(sender.ref, ref, 'dialog');
      die(`send: ${ref} is showing a dialog; nothing was sent`, 17);
    }
  }

  // Pre-prompt reads (Decision 3): state_change_seq and visible screen right before prompt.
  const preGet = agentGet(t.machine, t.targetArg, env);
  const preSeq = (preGet.ok && preGet.seq) ? preGet.seq : '';
  const preScreen = vScreen.text;

  // 5. Deliver: header + blank line + quoted body + end line, through agent prompt --wait.
  // The first line starts with [herdr-soho:peer] #<id>, each body line quoted with "> ",
  // and the final line is [herdr-soho:peer] #<id> end of message (Decision 1).
  const bodyText = quotePeerBody(literalPeerText(body));
  const endLine = peerEndLine(msgId);
  const text = `${peerHeader(literalPeerText(sender.ref), literalPeerText(sender.name), literalPeerText(sender.kind), literalPeerText(sender.role), msgId)}\n\n${bodyText}\n${endLine}`;
  const msgLines = text.split('\n').length;
  const recentLines = msgLines + 60;

  const p = deliverPrompt(t.machine, t.targetArg, text, env);
  if (!p.ok) {
    if (p.code === 'agent_prompt_stalled' || p.code === 'agent_blocked' || p.code === 'timeout') {
      const result = p.code === 'agent_prompt_stalled' ? 'stalled' : p.code === 'agent_blocked' ? 'blocked' : 'timeout';
      log(sender.ref, ref, result);
      die(`send: ${ref} did not take the message (${p.code === 'timeout' ? 'timeout' : p.cause}); read its pane before sending again`, 15);
    }
    log(sender.ref, ref, 'error');
    die(`send: ${ref} unavailable: ${p.cause}`, 4);
  }

  // 6. Arrival proof (Decisions 2, 3, 4, 5):
  // Poll in a 15 s window (poll ~1 s). Arrival is proven when either:
  // - (a) agent get returns non-empty state_change_seq !== non-empty preSeq;
  // - (b) #<id> is in recent-unwrapped (--lines msgLines + 60), visible screen !== preScreen,
  //   and the end line is not in the last 15 non-empty visible lines.
  // Never re-prompt (Decision 2).
  const windowMs = arrivalWindowMs(env);
  const pollMs = arrivalPollMs(env);

  const pollWindow = () => {
    const deadline = Date.now() + windowMs;
    let recentReadSucceeded = false;
    let lastCause = '';

    while (true) {
      // (a) agent get state_change_seq moved
      const curGet = agentGet(t.machine, t.targetArg, env);
      if (curGet.ok) {
        const curSeq = curGet.seq ?? '';
        if (preSeq !== '' && curSeq !== '' && curSeq !== preSeq) {
          return { proven: true };
        }
      } else if (curGet.cause) {
        lastCause = curGet.cause;
      }

      // (b) recent transcript contains #<id>, visible changed, end line not in last 15 lines
      const recentRes = agentReadScreen(t.machine, t.targetArg, { source: 'recent-unwrapped', lines: recentLines }, env);
      if (recentRes.ok) {
        recentReadSucceeded = true;
        if (recentRes.text.includes(`#${msgId}`)) {
          const visRes = agentReadScreen(t.machine, t.targetArg, { source: 'visible' }, env);
          if (visRes.ok) {
            const visText = visRes.text;
            if (visText !== preScreen) {
              const last15 = tailLines(visText, 15);
              const hasEndInLast15 = last15.some((l) => l.includes(endLine) || (l.includes(`#${msgId}`) && l.includes('end of message')));
              if (!hasEndInLast15) {
                return { proven: true };
              }
            }
          } else if (visRes.cause) {
            lastCause = visRes.cause;
          }
        }
      } else if (recentRes.cause) {
        lastCause = recentRes.cause;
      }

      const nowMs = Date.now();
      if (nowMs >= deadline) break;
      const sleepTime = Math.min(pollMs, deadline - nowMs);
      if (sleepTime <= 0) break;
      sleepMs(sleepTime);
    }

    return { proven: false, recentReadSucceeded, lastCause };
  };

  let res = pollWindow();
  if (res.proven) {
    log(sender.ref, ref, 'sent');
    process.stdout.write(`sent to ${ref}\n`);
    return 0;
  }

  // If every read failed in the window, do not send Enter: exit 15 unverified (Decision 5).
  if (!res.recentReadSucceeded) {
    log(sender.ref, ref, 'unverified');
    die(`send: could not confirm that ${ref} took the message (${res.lastCause || 'read failed'}); read its pane before sending again`, 15);
  }

  // No proof at window end: one Enter and a second window (Decision 4).
  agentSendKey(t.machine, t.targetArg, 'enter', env);

  res = pollWindow();
  if (res.proven) {
    log(sender.ref, ref, 'sent');
    process.stdout.write(`sent to ${ref}\n`);
    return 0;
  }

  if (!res.recentReadSucceeded) {
    log(sender.ref, ref, 'unverified');
    die(`send: could not confirm that ${ref} took the message (${res.lastCause || 'read failed'}); read its pane before sending again`, 15);
  }

  // Still no proof: exit 15 lost (Decision 4).
  log(sender.ref, ref, 'lost');
  die(`send: ${ref} did not take the message (no sign of it in its state or transcript); read its pane before sending again`, 15);
}
