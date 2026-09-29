// wait/collect bookkeeping (port slice 6a): the per-agent completion probe
// (report-size stability, blocked double-probe, auto-approve, quota,
// provider error / capacity double-probe with the bounded continue
// prompts, settled screen, gone / unavailable), the error-rank order for a
// multi-agent wait (4 > 11 > 14 > 15 > 7 > 6), the synchronous poll loop
// and the `wait` command. Port of the original bash implementation
// :3616-3810
// (kind_approve_keys :3616, try_auto_approve :3625, probe_agent :3644,
// notify_done :3690, wait_rank :3733, wait_raise :3742, wait_for :3751,
// cmd_wait :3802).
//
// A dispatch that ended `not-received` records the moment in
// <state>/wait/<agent>.not-received (epoch seconds); a prompt accepted while
// the target is working records <agent>.queued. The probe that finds
// that marker with the agent not working/blocked continues what the
// dispatch left: while the prompt is still visible in the agent's input
// box it retries one Enter per prompt_check_seconds window, up to three
// (the constant below); the agent starting to work clears the markers and
// the probe goes on as usual; otherwise the wait ends `not-received`
// (rank between 14 and 7).
//
// Faithful-port notes:
//   - one compact JSON line per agent, same keys in the same order as the
//     bash `jq -n -c` literals (insertion order in JSON.stringify);
//   - the screen hash is the first field of the local `cksum` (the
//     ISO 8802-3 32-bit CRC over the bytes + the least-significant-first
//     length octets, complemented — the BSD/macOS default `cksum`,
//     algorithm 3). It is only ever compared against itself across probes.
//   - the `.approvals` counter is read-then-written without a lock, as in
//     bash (spec open question 6: ported as-is, documented);
//   - the poll sleep is synchronous (Atomics.wait) and defaults to 3 s
//     like `sleep 3`; HERDR_SOHO_WAIT_POLL_MS shortens it for tests.
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { readTextFile } from './platform.mjs';
import { cfg, DieError } from './config.mjs';
import { stateDir, rosterLine, lastReport, warn, dieFriction, nowStamp, sleepSync, workspaceId } from './state.mjs';
import { agentState, agentRead, agentSendKeys, notificationShow, agentPrompt } from './herdr.mjs';
import { promptSitsInInput, queuedPromptPath, queuedPromptSitsInInput, markerHasPromptPath, markerSeqChanged } from './arrival.mjs';
import { sanitizeCause, hasWord } from './text.mjs';
import { dialogKind, questionText } from './dialog.mjs';
import { partialCount, reviewHeader } from './reportscan.mjs';
import { REVIEW_ROLES_ALL } from './roles.mjs';
import { quotaDetect } from './quota.mjs';
import { providerDetect } from './provider.mjs';
import { laneOfRole } from './lanes.mjs';
import { roleTimeoutMs } from './resolve.mjs';
import { markTaskDone } from './tasks.mjs';
import { readTaskReportPointer, syncTaskReport } from './taskreport.mjs';

// ---------- kind_approve_keys (:3616) ----------

// Logical key that accepts the highlighted default of that CLI's approval
// dialog (herdr agent send-keys syntax).
export function kindApproveKeys(kind) {
  switch (kind) {
    case 'claude':
    case 'grok':
    case 'agy':
    case 'cursor': return 'enter'; // option 1 "Yes" is preselected
    case 'codex': return 'y'; // Codex approval: y = yes
    default: return 'enter';
  }
}

// ---------- try_auto_approve (:3625) ----------

// Returns true when a key was sent and the wait may continue. The counter
// is read then written without a lock (as in bash; spec open question 6).
export function tryAutoApprove(sd, agent, ctx, env = process.env) {
  if (cfg(ctx, 'auto_approve', 'off', env) !== 'on') return false;
  const maxRaw = cfg(ctx, 'max_auto_approvals', '20', env);
  const max = Number(maxRaw);
  // Absent counter = 0. A counter that exists but is not an integer (empty,
  // truncated by an interrupted write) fails closed like bash
  // `[ "$n" -lt "$max" ]`: warn-and-return, no keypress.
  let n = 0;
  let nRaw = null;
  try { nRaw = readTextFile(path.join(sd, 'wait', `${agent}.approvals`)); } catch { /* absent */ }
  if (nRaw !== null) n = /^\s*[0-9]+\s*$/.test(nRaw) ? Number(nRaw.trim()) : NaN;
  if (!(n < max)) {
    warn(`auto_approve: ${agent} reached max_auto_approvals=${maxRaw}; leaving it blocked`);
    return false;
  }
  const kind = rosterLine(sd, agent).split('\t')[2] ?? '';
  if (!agentSendKeys(agent, kindApproveKeys(kind), env)) return false;
  const next = n + 1;
  fs.writeFileSync(path.join(sd, 'wait', `${agent}.approvals`), `${next}\n`);
  fs.appendFileSync(path.join(sd, 'wait', `${agent}.approvals.log`), `${nowStamp()} auto-approved dialog #${next}\n`);
  warn(`auto_approve: answered dialog #${next} for '${agent}' with its default option`);
  fs.rmSync(path.join(sd, 'wait', `${agent}.blocked`), { force: true });
  return true;
}

// ---------- probe_agent (:3644) ----------

function reportNonEmpty(p) {
  if (!p) return false;
  try { return fs.statSync(p).size > 0; } catch { return false; }
}

// The first field of `cksum` for `text`: ISO 8802-3 32-bit CRC (poly
// 0x04C11DB7, init 0, MSB-first) over the UTF-8 bytes followed by the
// smallest little-endian octet count of the byte length, complemented —
// the BSD/macOS `cksum` default (algorithm 3; verified against the local
// binary). Used only to compare screen stability across probes.
export function cksumField(text) {
  const buf = Buffer.from(String(text), 'utf8');
  const extra = [];
  let n = buf.length;
  do { extra.push(n & 0xff); n >>>= 8; } while (n > 0);
  const data = Buffer.concat([buf, Buffer.from(extra)]);
  let crc = 0;
  for (const b of data) {
    crc ^= (b << 24);
    for (let j = 0; j < 8; j++) {
      crc = (crc & 0x80000000) ? ((crc << 1) ^ 0x04c11db7) & 0xffffffff : (crc << 1) & 0xffffffff;
      crc >>>= 0;
    }
  }
  return (~crc) >>> 0;
}

// The `.size` bookkeeping uses `wc -c < report`: the BSD/macOS `wc` prints
// the count right-justified in an 8-character field (9 digits in a
// 10-character field, wider counts unpadded), so the stored value keeps
// that padding to stay byte-identical with the bash write.
export function wcSize(size) {
  const s = String(size);
  const w = s.length < 8 ? 8 : s.length === 9 ? 10 : s.length;
  return s.padStart(w, ' ');
}

function readWaitFile(sd, agent, name) {
  try { return readTextFile(path.join(sd, 'wait', name)).replace(/\n+$/, ''); } catch { return null; }
}

// Age (seconds) of the last observed visible-screen change, or null when
// nothing proves one. Reading a screen never proves activity: only a wait
// that sees a previously recorded hash move writes <agent>.activity-at,
// dated at the probe before the move (<agent>.probe-at, else
// <agent>.stuck-since) — the oldest moment the change could have happened,
// so a long gap between two waits never reads as a fresh change. The
// current screen differing from <agent>.stuck-hash is a change since the
// last probe: its age is nowS minus that probe. An equal hash is
// nowS - activity-at. A failed (empty) read, a recorded empty-screen hash,
// or a missing or invalid marker is null. Same hash as the stuck detection
// (cksumField over normalizeScreen — counters and progress glyphs do not
// count); status calls this read-only.
export function activityAgeSeconds(sd, agent, screenText, nowS) {
  const hashRaw = readWaitFile(sd, agent, `${agent}.stuck-hash`);
  if (hashRaw === null || hashRaw.trim() === '') return null;
  // A failed read comes back empty: it proves nothing, and neither does a
  // recorded hash of an empty screen.
  if (String(screenText ?? '').trim() === '' || hashRaw.trim() === EMPTY_SCREEN_HASH()) return null;
  const h = String(cksumField(normalizeScreen(screenText)));
  if (h !== String(hashRaw.trim())) {
    const since = positiveInt(readWaitFile(sd, agent, `${agent}.probe-at`))
      ?? positiveInt(readWaitFile(sd, agent, `${agent}.stuck-since`));
    return since === null ? null : nowS - since;
  }
  const at = positiveInt(readWaitFile(sd, agent, `${agent}.activity-at`));
  return at === null ? null : nowS - at;
}

// A marker's positive integer, or null (missing, empty, non-numeric, 0).
function positiveInt(raw) {
  const t = raw === null || raw === undefined ? '' : String(raw).trim();
  return /^[0-9]+$/.test(t) && Number(t) > 0 ? Number(t) : null;
}

// The hash of an empty screen (what a failed `agent read` returns): moving
// to or from it is not a real change.
const EMPTY_SCREEN_HASH = () => String(cksumField(normalizeScreen('')));

// The screen normalization shared by the stuck-worker detection and the
// auto-approve dialog repetition: CRLF → LF, digit runs → '#', and the
// Braille and spinner/progress glyphs (\u2800-\u28ff, \u25d0-\u25d3,
// \u2588\u258c\u2590\u2591) → '*' — they rotate between polls of the same
// screen, so leaving them in would reset the repetition counter.
export function normalizeScreen(text) {
  return String(text)
    .replace(/\r\n/g, '\n')
    .replace(/[0-9]+/g, '#')
    .replace(/[\u2800-\u28ff\u25d0-\u25d3\u2588\u258c\u2590\u2591]/g, '*');
}

// The same auto-approve dialog is not re-sent forever. After each key
// sent, the hash of the visible screen normalized like the stuck-worker
// detection (normalizeScreen: CRLF → LF, digit runs → '#', Braille and
// spinner/progress glyphs → '*') and a repetition count are kept in
// wait/<agent>.approve-screen ("<count>\t<hash>"): an equal hash
// increments the counter on the next block, a different one resets it to
// 1, and from the third consecutive repetition the key is not sent — the
// dialog is not advancing and the agent stays blocked. The record is
// written only after a key was actually sent (a failed keypress leaves it
// behind, like the .approvals counter), so max_auto_approvals stays the
// overall ceiling.
export const normalizeApproveScreen = normalizeScreen;

// The repetition count the next block would record for `hash`: count+1
// when the screen normalized to the same value as the previous record,
// else 1 (a different dialog resets the counter; no record is 1).
function approveRepeatOf(sd, agent, hash) {
  const raw = readWaitFile(sd, agent, `${agent}.approve-screen`);
  if (raw === null) return 1;
  const parts = raw.split('\t');
  const prevHash = parts[1] ?? '';
  const count = Number.parseInt(parts[0] ?? '', 10);
  if (prevHash === '' || prevHash !== String(hash)) return 1;
  return (Number.isFinite(count) && count > 0 ? count : 0) + 1;
}

function approveRepeatSet(sd, agent, count, hash) {
  fs.writeFileSync(path.join(sd, 'wait', `${agent}.approve-screen`), `${count}\t${hash}\n`);
}

// The last 20 non-empty lines of the agent's visible screen, joined with
// newlines; '' when the screen cannot be read. (The blocked JSON's
// `dialog` field — the dialog the agent is sitting on.)
function visibleDialog(env, agent) {
  const screen = agentRead(env, agent, { source: 'visible' });
  const lines = String(screen).split('\n').map((l) => l.trim()).filter((l) => l !== '');
  return lines.slice(-20).join('\n');
}

// The wait retries the Enter at most this many times (one per
// prompt_check_seconds window): a CLI that is still opening swallows the
// first Enter(s), and three windows cover a slow opening. Not a config key
// on purpose.
const ENTER_RETRY_LIMIT = 3;

// done | pending | blocked | question | working | settled | quota |
// provider-error | capacity | gone | not-received |
// `unavailable\t<cause>` — per-agent screen/settled bookkeeping under
// <state>/wait/ (the .size/.screen/.since/.blocked/.question/.stuck-hash/
// .stuck-since/.stuck-warned/.activity-at/.probe-at/.quota/.provider/.provider-cause/
// .capacity-retries/.capacity-at files; a not-received dispatch adds
// .not-received, a prompt queued behind a working turn adds the .queued
// marker, and the wait's Enter retries use .enter-retry.
// S5 items 2 and 11a: a confirmed blocked screen that matches the kind's
// question marker (lib/dialog.mjs) returns `question` with the text saved
// in <agent>.question (no key sent, same rank as blocked); a working
// agent whose visible screen only changes in its counters (digits, progress
// glyphs) for stuck_warn_minutes gets one friction line (nothing sent, the
// status stays working).
export function probeAgent(sd, agent, report, ctx, env = process.env) {
  const grace = Number(cfg(ctx, 'settled_grace', '45', env));
  if (reportNonEmpty(report)) {
    fs.rmSync(path.join(sd, 'wait', `${agent}.queued`), { force: true });
    fs.rmSync(path.join(sd, 'wait', `${agent}.not-received`), { force: true });
    fs.rmSync(path.join(sd, 'wait', `${agent}.enter-retry`), { force: true });
    // Wait for the file size to stop changing (the worker may still be
    // writing); `.size` is cleared by waitFor, so a fresh report is only
    // `done` on the second probe.
    const size = fs.statSync(report).size;
    const prev = readWaitFile(sd, agent, `${agent}.size`) ?? '-1';
    fs.writeFileSync(path.join(sd, 'wait', `${agent}.size`), `${wcSize(size)}\n`);
    if (String(size) === prev.trim()) return 'done';
    return 'pending';
  }
  const st = agentState(agent, env);
  const nrFile = path.join(sd, 'wait', `${agent}.not-received`);
  const queuedFile = path.join(sd, 'wait', `${agent}.queued`);
  const retryFile = path.join(sd, 'wait', `${agent}.enter-retry`);
  if (st.state === 'gone') {
    fs.rmSync(queuedFile, { force: true });
    fs.rmSync(retryFile, { force: true });
    return 'gone';
  }
  if (st.state === 'unavailable') {
    return `unavailable\t${st.cause}`;
  }
  // A dispatch that ended not-received recorded the moment and the
  // agent's state_change_seq in .not-received ("<epoch> <seq>"; an
  // epoch-only marker is an older one and stays valid). Working or blocked
  // means the prompt arrived late, and a seq that moved means the agent
  // changed state in the meantime: in both cases drop the markers and go on
  // with the normal probe (no key in the seq case). Otherwise the wait
  // continues the dispatch with a bounded Enter retry (see below).
  let queuedPending = false;
  let queuedTerminal = false;
  let queuedText = '';
  if (fs.existsSync(queuedFile)) {
    queuedText = readWaitFile(sd, agent, `${agent}.queued`) ?? '';
    if (st.state === 'working') {
      if (markerSeqChanged(queuedText, st.seq)) {
        fs.rmSync(queuedFile, { force: true });
        fs.rmSync(retryFile, { force: true });
      }
    } else if (st.state === 'blocked' || st.state === 'gone' || st.state === 'unavailable') {
      queuedTerminal = true;
    } else {
      queuedPending = true;
    }
  }
  if (!queuedPending && fs.existsSync(nrFile)) {
    const markText = readWaitFile(sd, agent, `${agent}.not-received`);
    // True when the marker holds a seq, the current one is known, and the
    // two differ: the agent did something since the dispatch gave up, so a
    // prompt echo in the last lines is no proof the input box is stuck.
    const seqChanged = markerSeqChanged(markText, st.seq);
    if (st.state === 'working' || st.state === 'blocked' || seqChanged) {
      fs.rmSync(nrFile, { force: true });
      fs.rmSync(retryFile, { force: true });
    } else {
      // The same prompt_check_seconds read and validation as the dispatch
      // arrival check: a positive integer; the marker only exists when the
      // check ran with one, so an absent or invalid window just has no
      // grace between retries.
      const rawWin = String(cfg(ctx, 'prompt_check_seconds', '15', env));
      const winS = /^[0-9]+$/.test(rawWin) && Number(rawWin) > 0 ? Number(rawWin) : 0;
      let lastEpoch = 0;
      const markEpoch = Number(String(markText ?? '').trim().split(/\s+/)[0]);
      if (Number.isFinite(markEpoch)) lastEpoch = markEpoch;
      // .enter-retry: "<attempts> <epoch of the last Enter>". While it is
      // absent the last attempt is the dispatch's not-received moment; a
      // file that is not two integers fails closed (no more Enters), like
      // the .approvals counter.
      let attempts = 0;
      const retryRaw = readWaitFile(sd, agent, `${agent}.enter-retry`);
      if (retryRaw !== null) {
        const parts = retryRaw.trim().split(/\s+/);
        const a = Number(parts[0]);
        const e = Number(parts[1]);
        if (!Number.isFinite(a) || !Number.isFinite(e) || parts.length !== 2) return 'not-received';
        attempts = a;
        lastEpoch = e;
      }
      const nowS = Math.floor(Date.now() / 1000);
      const retryScreen = agentRead(env, agent, { source: 'recent-unwrapped', lines: 40 });
      const pathInInput = markerHasPromptPath(markText)
        ? queuedPromptSitsInInput(markText, retryScreen, sd, agent)
        : null;
      // A queued prompt that was already outside its exact input-box area
      // must stay not-received on every later wait, without a new retry grace.
      if (pathInInput === false) return 'not-received';
      // Inside the window since the last attempt: nothing to do, the agent
      // keeps working.
      if (nowS - lastEpoch < winS) return 'working';
      const markerInInput = pathInInput ?? promptSitsInInput(retryScreen);
      if (attempts < ENTER_RETRY_LIMIT && markerInInput) {
        const n = attempts + 1;
        agentSendKeys(agent, 'enter', env);
        fs.writeFileSync(retryFile, `${n} ${nowS}\n`);
        warn(`prompt to '${agent}' was still in its input box; sent Enter again (${n} of ${ENTER_RETRY_LIMIT})`);
        return 'working';
      }
      // The three retries are spent, or the prompt is no longer in the
      // input box with the agent not working: the wait ends not-received.
      return 'not-received';
    }
  }
  if (st.state === 'blocked') {
    fs.rmSync(retryFile, { force: true });
    const clearQueuedAfterBlockedProbe = () => {
      if (!queuedTerminal) return;
      fs.rmSync(queuedFile, { force: true });
      fs.rmSync(retryFile, { force: true });
    };
    // Detection can flag a transient approval UI; require two consecutive
    // blocked probes before acting. With auto_approve=on the default
    // option is sent and the wait continues (bounded by
    // max_auto_approvals). A decision question is never auto-answered: on
    // the confirmed probe the visible screen is read, and when it matches
    // the kind's question marker the wait reports `question` with the text
    // and sends no key — with or without auto_approve.
    const bfile = path.join(sd, 'wait', `${agent}.blocked`);
    if (fs.existsSync(bfile)) {
      const kind = rosterLine(sd, agent).split('\t')[2] ?? '';
      const visible = agentRead(env, agent, { source: 'visible', lines: 40 });
      if (dialogKind(kind, visible) === 'question') {
        fs.writeFileSync(path.join(sd, 'wait', `${agent}.question`), `${questionText(visible)}\n`);
        clearQueuedAfterBlockedProbe();
        return 'question';
      }
      const h = cksumField(normalizeApproveScreen(visible));
      const n = approveRepeatOf(sd, agent, h);
      if (n >= 3) {
        warn(`auto_approve: the same dialog came back 3 times for '${agent}'; leaving it blocked`);
        clearQueuedAfterBlockedProbe();
        return 'blocked';
      }
      if (!tryAutoApprove(sd, agent, ctx, env)) {
        clearQueuedAfterBlockedProbe();
        return 'blocked';
      }
      approveRepeatSet(sd, agent, n, h);
      clearQueuedAfterBlockedProbe();
      return 'working';
    }
    fs.writeFileSync(bfile, '');
    return 'working';
  }
  fs.rmSync(path.join(sd, 'wait', `${agent}.blocked`), { force: true });
  // The provider double-confirm and the capacity delay only hold across
  // consecutive probes that keep seeing the stop: a working agent or a
  // screen without it clears both (the continue counter stays per dispatch).
  const pfile = path.join(sd, 'wait', `${agent}.provider`);
  const atFile = path.join(sd, 'wait', `${agent}.capacity-at`);
  const clearProviderMarks = () => {
    fs.rmSync(pfile, { force: true });
    fs.rmSync(atFile, { force: true });
  };
  if (st.state === 'working') clearProviderMarks();
  // The visible screen, read at most once per probe: the stuck check and the
  // settled check below share it.
  let screen = null;
  const visibleScreen = () => (screen ??= agentRead(env, agent, { source: 'visible' }));
  // A working agent whose visible screen only changes in its counters
  // (digits and progress glyphs) for stuck_warn_minutes is probably stuck in
  // one tool call: one friction line, once, nothing is sent and the status
  // stays working. 0 disables the check.
  const limitMin = Number(cfg(ctx, 'stuck_warn_minutes', '20', env));
  // The hash markers follow every probe of a working agent, whatever
  // stuck_warn_minutes says (0 only turns the stuck warning off): the
  // activity age of the checkpoint and of `status` reads them.
  if (st.state === 'working') {
    const text = String(visibleScreen());
    const h = String(cksumField(normalizeScreen(text)));
    const nowS = Math.floor(Date.now() / 1000);
    const prevHash = readWaitFile(sd, agent, `${agent}.stuck-hash`);
    if (prevHash !== h) {
      // A real observed change (a previous non-empty hash moved to a
      // non-empty screen) is dated at the probe before it — the oldest
      // moment it could have happened. The first observation of a screen
      // only reads it, and a failed (empty) read is not a change.
      const prevProbe = positiveInt(readWaitFile(sd, agent, `${agent}.probe-at`))
        ?? positiveInt(readWaitFile(sd, agent, `${agent}.stuck-since`));
      if (prevHash !== null && prevHash.trim() !== '' && prevHash.trim() !== EMPTY_SCREEN_HASH()
        && text.trim() !== '' && prevProbe !== null) {
        fs.writeFileSync(path.join(sd, 'wait', `${agent}.activity-at`), `${prevProbe}\n`);
      }
      fs.writeFileSync(path.join(sd, 'wait', `${agent}.stuck-hash`), `${h}\n`);
      fs.writeFileSync(path.join(sd, 'wait', `${agent}.stuck-since`), `${nowS}\n`);
      fs.rmSync(path.join(sd, 'wait', `${agent}.stuck-warned`), { force: true });
    } else if (limitMin > 0 && !fs.existsSync(path.join(sd, 'wait', `${agent}.stuck-warned`))) {
      const rawSince = readWaitFile(sd, agent, `${agent}.stuck-since`);
      // A .stuck-since that is missing, empty or non-numeric is treated as
      // now (and rewritten) — never as epoch 0, which would age the screen
      // to the Unix epoch (a "29839405 min" friction line).
      const trimmed = rawSince === null ? '' : String(rawSince).trim();
      const sinceS = /^[0-9]+$/.test(trimmed) ? Number(trimmed) : NaN;
      if (!Number.isFinite(sinceS) || sinceS <= 0) {
        fs.writeFileSync(path.join(sd, 'wait', `${agent}.stuck-since`), `${nowS}\n`);
      } else if ((nowS - sinceS) >= limitMin * 60) {
        warn(`agent '${agent}' has shown the same screen (apart from counters) for ${Math.floor((nowS - sinceS) / 60)} min while working; it may be stuck in one tool call. Inspect: herdr agent read ${agent} --source recent-unwrapped --lines 60`);
        fs.writeFileSync(path.join(sd, 'wait', `${agent}.stuck-warned`), '');
      }
    }
    // Only a read that returned a screen counts as a probe of it.
    if (text.trim() !== '') fs.writeFileSync(path.join(sd, 'wait', `${agent}.probe-at`), `${nowS}\n`);
  }
  if (st.state !== 'working') {
    const qtext = agentRead(env, agent, { source: 'visible', lines: 20 });
    const q = quotaDetect(st.state, qtext);
    if (q) {
      fs.rmSync(queuedFile, { force: true });
      fs.rmSync(retryFile, { force: true });
      // `printf '%s\n' "$(quota_detect …)"`: the command substitution strips
      // the trailing newlines, so an empty renewal leaves a one-line file.
      fs.writeFileSync(path.join(sd, 'wait', `${agent}.quota`), q[1] !== '' ? `${q[0]}\n${q[1]}\n` : `${q[0]}\n`);
      clearProviderMarks(); // a quota screen interrupts any provider confirmation
      return 'quota';
    }
    // Provider stop (after the quota, which always wins): the same screen,
    // read as recent-unwrapped (long lines arrive unbroken).
    const ptext = agentRead(env, agent, { source: 'recent-unwrapped', lines: 40 });
    const p = providerDetect(st.state, ptext);
    if (p) {
      // Terminal authentication failure (R11/D58): no retry or second
      // probe will help, so report it on the first probe instead of
      // double-confirming or settling without a report. The quota check
      // above still wins over this on the same screen.
      if (p.status === 'provider-error' && p.auth === true) {
        fs.rmSync(queuedFile, { force: true });
        fs.rmSync(retryFile, { force: true });
        clearProviderMarks();
        fs.writeFileSync(path.join(sd, 'wait', `${agent}.provider-cause`), `${p.cause}\n`);
        return 'provider-error';
      }
      // Double confirm, like blocked: the first detection records the
      // screen hash and the detected status in <agent>.provider and keeps
      // working; it acts only on the next probe when the hash AND the
      // status are the same. Any difference re-records the new detection.
      const target = `${String(cksumField(ptext))}\n${p.status}`;
      const prev = readWaitFile(sd, agent, `${agent}.provider`);
      if (prev !== target) {
        fs.rmSync(pfile, { force: true });
        fs.writeFileSync(pfile, `${target}\n`);
        return 'working';
      }
      // Confirmed: the same screen hash and status as the previous probe.
      fs.writeFileSync(path.join(sd, 'wait', `${agent}.provider-cause`), `${p.cause}\n`);
      if (p.status === 'provider-error') {
        fs.rmSync(queuedFile, { force: true });
        fs.rmSync(retryFile, { force: true });
        return 'provider-error';
      }
      // Capacity is transient: at most provider_retries continue prompts,
      // each at least provider_retry_delay apart from the first
      // confirmation (the .capacity-at epoch-s marker).
      let used = 0;
      const usedRaw = readWaitFile(sd, agent, `${agent}.capacity-retries`);
      if (usedRaw !== null) {
        // A counter that exists but is not an integer fails closed, like
        // the .approvals counter: treat it as exhausted.
        if (!/^\s*[0-9]+\s*$/.test(usedRaw)) {
          fs.rmSync(queuedFile, { force: true });
          fs.rmSync(retryFile, { force: true });
          return 'capacity';
        }
        used = Number(usedRaw.trim());
      }
      const limit = Number(cfg(ctx, 'provider_retries', '3', env));
      if (!Number.isFinite(limit) || used >= limit) {
        fs.rmSync(queuedFile, { force: true });
        fs.rmSync(retryFile, { force: true });
        return 'capacity';
      }
      const atRaw = readWaitFile(sd, agent, `${agent}.capacity-at`);
      if (atRaw === null) {
        fs.writeFileSync(atFile, `${Math.floor(Date.now() / 1000)}\n`);
        return 'working';
      }
      const delay = Number(cfg(ctx, 'provider_retry_delay', '60', env));
      const at = Number(atRaw);
      const nowS = Math.floor(Date.now() / 1000);
      // Bash `[ $((now_s - since)) -ge "$grace" ]`: a non-numeric operand
      // fails the test and the agent keeps working.
      if (!(Number.isFinite(at) && Number.isFinite(delay) && (nowS - at) >= delay)) return 'working';
      const report = lastReport(sd, agent);
      const sent = agentPrompt(agent,
        `The model provider was at capacity and your last request failed. Continue the task from where you stopped; do not redo finished steps. When finished, write your report to ${report} and reply with only that path.`,
        env);
      if (!sent.ok) {
        // The continue never left: warn and act as if exhausted.
        warn(`provider capacity: failed to send the continue to '${agent}': ${sanitizeCause(sent.raw) || 'unknown error'}; acting as exhausted`);
        fs.rmSync(queuedFile, { force: true });
        fs.rmSync(retryFile, { force: true });
        return 'capacity';
      }
      fs.writeFileSync(path.join(sd, 'wait', `${agent}.capacity-retries`), `${used + 1}\n`);
      fs.rmSync(atFile, { force: true });
      fs.rmSync(pfile, { force: true });
      warn(`provider capacity: sent continue #${used + 1} of ${limit} to '${agent}': ${p.cause}`);
      fs.rmSync(queuedFile, { force: true });
      fs.rmSync(retryFile, { force: true });
      return 'working';
    }
    clearProviderMarks();
  }
  if (queuedPending) {
    const screen = agentRead(env, agent, { source: 'recent-unwrapped', lines: 40 });
    const inInput = queuedPromptSitsInInput(queuedText, screen, sd, agent);
    if (!inInput) {
      const promptPath = queuedPromptPath(queuedText, sd, agent) || '-';
      const seqField = st.seq !== '' ? st.seq : '-';
      fs.writeFileSync(nrFile, `${Math.floor(Date.now() / 1000)} ${seqField} ${promptPath}\n`);
      fs.rmSync(queuedFile, { force: true });
      fs.rmSync(retryFile, { force: true });
      return 'not-received';
    }
    const first = String(queuedText).trim().split(/\s+/)[0];
    const epoch = /^[0-9]+$/.test(first) ? first : `${Math.floor(Date.now() / 1000)}`;
    const promptPath = queuedPromptPath(queuedText, sd, agent) || '-';
    // An absent seq is written as '-', the field .queued uses, so the
    // marker keeps three fields and markerHasPromptPath still sees the path.
    const seqField = st.seq !== '' ? st.seq : '-';
    fs.writeFileSync(nrFile, `${epoch} ${seqField} ${promptPath}\n`);
    fs.rmSync(queuedFile, { force: true });
    const nowS = Math.floor(Date.now() / 1000);
    const winRaw = String(cfg(ctx, 'prompt_check_seconds', '15', env));
    const winS = /^[0-9]+$/.test(winRaw) && Number(winRaw) > 0 ? Number(winRaw) : 0;
    const retryRaw = readWaitFile(sd, agent, `${agent}.enter-retry`);
    let attempts = 0;
    let lastEpoch = epoch;
    if (retryRaw !== null) {
      const parts = retryRaw.trim().split(/\s+/);
      const a = Number(parts[0]);
      const e = Number(parts[1]);
      if (!Number.isFinite(a) || !Number.isFinite(e) || parts.length !== 2) return 'not-received';
      attempts = a;
      lastEpoch = e;
    }
    if (nowS - Number(lastEpoch) < winS) return 'working';
    if (attempts < ENTER_RETRY_LIMIT && inInput) {
      const n = attempts + 1;
      agentSendKeys(agent, 'enter', env);
      fs.writeFileSync(retryFile, `${n} ${nowS}\n`);
      warn(`prompt to '${agent}' was still in its input box; sent Enter again (${n} of ${ENTER_RETRY_LIMIT})`);
      return 'working';
    }
    return 'not-received';
  }
  const hash = String(cksumField(visibleScreen()));
  const lastScreen = readWaitFile(sd, agent, `${agent}.screen`) ?? '';
  const nowS = Math.floor(Date.now() / 1000);
  if (st.state === 'working' || hash !== lastScreen) {
    fs.writeFileSync(path.join(sd, 'wait', `${agent}.screen`), `${hash}\n`);
    fs.writeFileSync(path.join(sd, 'wait', `${agent}.since`), `${nowS}\n`);
    return 'working';
  }
  const sinceRaw = readWaitFile(sd, agent, `${agent}.since`) ?? String(nowS);
  const since = Number(sinceRaw);
  // Bash `[ $((now_s - since)) -ge "$grace" ]`: a non-numeric operand
  // fails the test and the agent keeps working.
  if (Number.isFinite(since) && Number.isFinite(grace) && (nowS - since) >= grace) return 'settled';
  return 'working';
}

// ---------- notify_done (:3690) ----------

// notify=on: one notification per finished agent; best effort.
export function notifyDone(agent, report, ctx, env = process.env) {
  if (cfg(ctx, 'notify', 'off', env) === 'on') notificationShow(`herdr-soho: ${agent} finished`, report, env);
}

// ---------- D24: the $TMPDIR-routed report is mirrored into the state dir ----------

// The tmp routing dir dispatch writes to when the worker's cwd is outside
// the repo root: <TMPDIR>/herdr-soho/<ws>/reports (the report and its
// composed prompt, <name>.brief.md next to it).
function tmpReportsDir(ctx, env, cwd) {
  return path.join(env.TMPDIR || os.tmpdir(), 'herdr-soho', workspaceId(ctx, env, cwd), 'reports');
}

// One best-effort copy: a file already at the destination with the same
// content is not rewritten (the mtime stands); an absent or different one
// is written over.
function copyOnce(src, dst) {
  const buf = fs.readFileSync(src);
  if (fs.existsSync(dst)) {
    let same = false;
    try { same = fs.readFileSync(dst).equals(buf); } catch { same = false; }
    // An existing file with other content is never overwritten: it may be
    // the only copy of an earlier report.
    if (!same) warn(`kept ${dst}: it differs from ${src}, which was not copied over it`);
    return;
  }
  fs.mkdirSync(path.dirname(dst), { recursive: true });
  fs.writeFileSync(dst, buf);
}

// When a done report sits under the tmp routing dir, the report, its
// composed prompt (next to it) and its attempt sidecar (.dispatch.json)
// are copied into the state dir — the report to <state>/reports/<name>,
// the prompt to <state>/briefs/<name> minus the .brief, and the sidecar to
// <state>/briefs/<name>.dispatch.json (the same layout dispatch writes
// when the report stays in the state dir). Best effort: each failure warns
// and the done stands, and last-report-<agent> and the JSON line keep
// pointing at the original. A pair predating the sidecar has none: a
// missing sidecar source is normal and is skipped without a warn (the
// conservative copyOnce policy still applies when it is there: a
// different existing file is never overwritten).
function mirrorReport(sd, agent, report, ctx, env, cwd) {
  const dir = tmpReportsDir(ctx, env, cwd);
  if (!report.startsWith(dir + path.sep)) return;
  const base = path.basename(report);
  const stem = base.slice(0, -path.extname(base).length);
  const jobs = [
    [report, path.join(sd, 'reports', base), false],
    [path.join(path.dirname(report), `${stem}.brief.md`), path.join(sd, 'briefs', `${stem}.md`), false],
    [path.join(path.dirname(report), `${stem}.dispatch.json`), path.join(sd, 'briefs', `${stem}.dispatch.json`), true],
  ];
  for (const [src, dst, optional] of jobs) {
    if (optional && !fs.existsSync(src)) continue;
    try {
      copyOnce(src, dst);
    } catch (e) {
      warn(`mirror: could not copy ${path.basename(src)} of '${agent}' to the state dir: ${sanitizeCause(e && e.message ? e.message : e) || 'unknown error'}`);
    }
  }
}

// ---------- wait_rank / wait_raise (:3733) ----------

// One order for a multi-agent wait: 4 unavailable > 11 quota >
// 14 provider-error or capacity > 15 not-received > 7 blocked >
// 6 gone or settled. The argument order must not turn a quota into a
// blocked or a gone.
export function waitRank(code) {
  switch (String(code)) {
    case '4': return 6;
    case '11': return 5;
    case '14': return 4;
    case '15': return 3;
    case '7': return 2;
    case '6': return 1;
    default: return 0;
  }
}

// wait_raise: the candidate wins when it ranks strictly higher.
function waitRaise(rc, cand) {
  return waitRank(cand) > waitRank(rc) ? cand : rc;
}

// ---------- wait_for (:3751) ----------

// Poll interval in ms: 3 s like the bash `sleep 3`; the env override is
// test-only (undocumented for users) and only shortens it: an integer from
// 1 to 3000, anything else keeps 3000 (never a 0 ms busy loop).
export function pollIntervalMs(env) {
  const raw = env.HERDR_SOHO_WAIT_POLL_MS;
  const v = /^[0-9]+$/.test(raw ?? '') ? Number(raw) : NaN;
  return v >= 1 && v <= 3000 ? v : 3000;
}

function jsonLine(obj, sink) {
  sink(`${JSON.stringify(obj)}\n`);
}

// Reads <state>/wait/<agent>.quota: line 1 = match, line 2 = renewal
// ('' when the file is absent or short).
function readQuotaFile(sd, agent) {
  const raw = readWaitFile(sd, agent, `${agent}.quota`);
  if (raw === null) return ['', ''];
  const lines = raw.split('\n');
  return [lines[0] ?? '', lines[1] ?? ''];
}

// wait_for <timeout_ms> <any> <agent>… → one JSON line per agent;
// rc 0 when every agent settles, 4/11/14/15/7/6 by rank otherwise, 9 on
// timeout (with a `timeout` line for each pending agent: `elapsed_ms`
// since the start of this wait and the agent's last probe state, plus one
// warn per agent suggesting the doubled `--timeout`). Synchronous: the sleep
// between probes is Atomics.wait, so the caller's event loop never turns.
// `sink` receives each JSON line (default: process.stdout). The `wait`
// command prints them; `dispatch` captures them instead of printing (bash
// `out="$(wait_for …)"`) — the behavior is otherwise unchanged.
export function waitFor(agents, opts) {
  const { sd, ctx, env, timeoutMs, any = false, sink = (l) => process.stdout.write(l), cwd = process.cwd() } = opts;
  const pollMs = pollIntervalMs(env);
  const tm = Number(timeoutMs);
  // A non-numeric timeout would make the deadline check never fire (infinite
  // poll loop); 0 = timeout right after the first probe round.
  const deadline = Number.isFinite(tm)
    ? Math.floor(Date.now() / 1000) + Math.floor(tm / 1000)
    : 0;
  const startedAt = Date.now();
  // The last probe tag per pending agent, for the timeout line's `state`.
  const lastState = new Map();
  for (const a of agents) fs.rmSync(path.join(sd, 'wait', `${a}.size`), { force: true });
  let rc = 0;
  let remaining = [...agents];
  for (;;) {
    const pending = [];
    for (const a of remaining) {
      const r = lastReport(sd, a);
      const st = probeAgent(sd, a, r, ctx, env);
      const tag = st.split('\t')[0];
      switch (tag) {
        case 'done': {
          // The report is read once for the two markers: the review
          // header (findings/severity/verdict — right after `report`) and
          // the `partial` item count (after the header fields). A done
          // report that still marks `partial` items is not a pass: the
          // JSON line carries the count (key present only when it is > 0)
          // and one warn per agent+report tells the orchestrator to read
          // the partial items before commit, push or release (the marker
          // wait/<agent>.partial-warned holds the report path already
          // warned, so a second wait on the same report stays quiet). An
          // unreadable report reads '' (count 0, no header) and the line
          // is unchanged.
          let reportText = '';
          try { reportText = readTextFile(r); } catch { reportText = ''; }
          const partial = partialCount(reportText);
          const header = reviewHeader(reportText);
          const done = { agent: a, status: 'done', report: r };
          const taskPointer = readTaskReportPointer(sd, a);
          if (taskPointer !== null && taskPointer.current === r) {
            const stable = syncTaskReport(sd, a);
            if (stable !== null) done.task_report = taskPointer.task_report;
          }
          if (header) {
            done.verdict = header.verdict;
            done.findings = header.findings;
            done.severity = header.severity;
          }
          if (partial > 0) done.partial = partial;
          jsonLine(done, sink);
          if (header) {
            // The numbers are kept as parsed; a mismatch with the P0..P3
            // sum is warned, never recomputed.
            const sum = header.severity.P0 + header.severity.P1 + header.severity.P2 + header.severity.P3;
            if (sum !== header.findings) {
              warn(`report of '${a}': findings ${header.findings} but P0..P3 add up to ${sum}`);
            }
          } else {
            // A review report without the header is suspicious: the
            // orchestrator should read the report before trusting the done
            // (the current role comes from roster column 4). The four
            // review roles all carry the fixed header line now.
            const roleNow = rosterLine(sd, a).split('\t')[3] ?? '';
            if (roleNow !== '' && hasWord(REVIEW_ROLES_ALL, roleNow)) {
              warn(`report of '${a}' has no 'findings: N (P0 a, P1 b, P2 c, P3 d) | verdict: pass|fail' first line`);
            }
          }
          if (partial > 0) {
            // Once per agent+report: the marker holds the report path
            // already warned. A second wait on the same report keeps the
            // `partial` key in the JSON but warns nothing; a new report
            // path warns again.
            if (readWaitFile(sd, a, `${a}.partial-warned`) !== r) {
              warn(`report of '${a}' marks ${partial} item(s) partial: a partial item is not a pass; read them before commit, push or release`);
              fs.writeFileSync(path.join(sd, 'wait', `${a}.partial-warned`), `${r}\n`);
            }
          }
          notifyDone(a, r, ctx, env);
          markTaskDone(sd, a, env);
          mirrorReport(sd, a, r, ctx, env, cwd);
          if (any) return 0;
          break;
        }
        case 'blocked': {
          jsonLine({ agent: a, status: 'blocked', report: r, dialog: visibleDialog(env, a) }, sink);
          rc = waitRaise(rc, 7);
          break;
        }
        case 'question': {
          const q = readWaitFile(sd, a, `${a}.question`) ?? '';
          jsonLine({ agent: a, status: 'question', report: r, question: q }, sink);
          warn(`agent '${a}' asked a question; nobody answers it automatically. Ask the user, then answer with herdr agent send-keys/prompt, or release the worker.`);
          rc = waitRaise(rc, 7);
          break;
        }
        case 'gone':
          jsonLine({ agent: a, status: 'gone', report: r }, sink);
          rc = waitRaise(rc, 6);
          break;
        case 'settled':
          jsonLine({ agent: a, status: 'settled-no-report', report: r }, sink);
          rc = waitRaise(rc, 6);
          break;
        case 'unavailable': {
          const cause = st.slice(st.indexOf('\t') + 1) || '';
          jsonLine({ agent: a, status: 'unavailable', report: r, error: cause }, sink);
          warn(`agent '${a}': herdr agent get failed: ${cause}`);
          rc = waitRaise(rc, 4);
          break;
        }
        case 'quota': {
          const [match, renewal] = readQuotaFile(sd, a);
          const f = rosterLine(sd, a).split('\t');
          const kind = f[2] ?? '';
          const model = f.length >= 9 ? (f[8] ?? '') : '';
          let lane = f.length >= 12 ? (f[11] ?? '') : '';
          const roleNow = f[3] ?? '';
          if (lane === '') lane = laneOfRole(ctx, roleNow, env);
          jsonLine({ agent: a, status: 'quota', report: r, lane, kind, model, match, renewal }, sink);
          warn(`quota: agent '${a}' lane=${lane || '?'} kind=${kind} model=${model || '?'} : ${match}${renewal ? `; renewal: ${renewal}` : ''}`);
          rc = waitRaise(rc, 11);
          break;
        }
        case 'not-received':
          jsonLine({ agent: a, status: 'not-received', report: r }, sink);
          warn(`prompt to '${a}' never reached it: read the pane (herdr agent read ${a} --source visible), then dispatch again`);
          rc = waitRaise(rc, 15);
          break;
        case 'provider-error':
        case 'capacity': {
          const cause = readWaitFile(sd, a, `${a}.provider-cause`) ?? '';
          const f = rosterLine(sd, a).split('\t');
          const kind = f[2] ?? '';
          const model = f.length >= 9 ? (f[8] ?? '') : '';
          let lane = f.length >= 12 ? (f[11] ?? '') : '';
          const roleNow = f[3] ?? '';
          if (lane === '') lane = laneOfRole(ctx, roleNow, env);
          if (tag === 'provider-error') {
            jsonLine({ agent: a, status: 'provider-error', report: r, lane, kind, model, cause }, sink);
            warn(`provider error: agent '${a}' lane=${lane || '?'} kind=${kind} model=${model || '?'} : ${cause}`);
          } else {
            const retryRaw = readWaitFile(sd, a, `${a}.capacity-retries`);
            const retries = retryRaw !== null && /^\s*[0-9]+\s*$/.test(retryRaw) ? Number(retryRaw.trim()) : 0;
            jsonLine({ agent: a, status: 'capacity', report: r, lane, kind, model, cause, retries }, sink);
            warn(`provider capacity: agent '${a}' lane=${lane || '?'} kind=${kind} model=${model || '?'} : ${cause}`);
          }
          rc = waitRaise(rc, 14);
          break;
        }
        default:
          lastState.set(a, tag);
          pending.push(a);
      }
    }
    remaining = pending;
    if (remaining.length === 0) return rc;
    if (Math.floor(Date.now() / 1000) >= deadline) {
      // Neutral checkpoint (#3 + amendment): a pending agent still working
      // with an observed screen change under the stuck window (a real hash
      // move, not a fresh read) gets one stderr line and no friction entry;
      // anything else keeps today's timeout warn (the friction line). The
      // visible screen is read once per pending agent, after the last probe.
      const nowS = Math.floor(Date.now() / 1000);
      const rawWin = cfg(ctx, 'stuck_warn_minutes', '20', env);
      const winMin = Number(rawWin);
      const windowS = (Number.isFinite(winMin) && winMin > 0 ? winMin : 20) * 60;
      for (const a of remaining) {
        const state = lastState.get(a) ?? 'working';
        const age = activityAgeSeconds(sd, a, agentRead(env, a, { source: 'visible' }), nowS);
        const active = state === 'working' && age !== null && age < windowS;
        jsonLine({ agent: a, status: 'timeout', elapsed_ms: Date.now() - startedAt, state, checkpoint: active, activity_age_s: age }, sink);
        if (active) {
          // stderr only: a neutral checkpoint writes no friction line. A
          // non-numeric timeout has no value to repeat and drops the
          // suggestion.
          const sug = Number.isFinite(tm) ? ` --timeout ${tm}` : '';
          process.stderr.write(`herdr-soho: checkpoint: '${a}' is still working (screen changed ${age}s ago); wait again: herdr-soho wait ${a}${sug}\n`);
        } else {
          // The suggestion doubles the timeout this wait used (an explicit
          // --timeout or the roles' timeouts); a non-numeric timeout has no
          // value to double and drops the suggestion.
          if (Number.isFinite(tm)) {
            warn(`timeout waiting for '${a}'; it may still be working (state: ${state}). Run: herdr-soho wait ${a} --timeout ${tm * 2}`);
          } else {
            warn(`timeout waiting for '${a}'; it may still be working (state: ${state})`);
          }
        }
      }
      return 9;
    }
    sleepSync(pollMs);
  }
}

// ---------- cmd_wait (:3802) ----------

// `wait <agent>… [--timeout MS] [--any]`: rc 0 all done · 4 unavailable ·
// 11 quota · 14 provider-error|capacity · 15 not-received ·
// 7 blocked (or a question) · 6 settled-no-report|gone · 9 timeout.
export function cmdWait(argv, ctx, env = process.env, cwd = process.cwd()) {
  const agents = [];
  let timeout = '';
  let any = 0;
  for (let i = 0; i < argv.length; i += 1) {
    const a = argv[i];
    if (a === '--timeout') {
      const v = argv[i + 1];
      if (v === undefined) dieFriction('wait: --timeout expects a value', 2);
      // Bash fails on the shell arithmetic; the port says why (decision).
      if (!/^[0-9]+$/.test(v)) dieFriction(`wait: --timeout expects milliseconds, got '${v}'`, 2);
      timeout = v;
      i += 1;
    } else if (a === '--any') {
      any = 1;
    } else if (a.startsWith('--')) {
      dieFriction(`wait: unknown option ${a}`, 2);
    } else {
      agents.push(a);
    }
  }
  if (agents.length === 0) dieFriction('wait: give at least one agent name', 2);
  const sd = stateDir(ctx, env, cwd);
  for (const a of agents) {
    if (rosterLine(sd, a) === '') dieFriction(`agent '${a}' is not in the roster`, 3);
  }
  if (timeout === '') {
    // Without --timeout the wait allows each agent its role's timeout
    // (the frontmatter `timeout` scaled by the role's effective effort,
    // else `dispatch_timeout`, roster column 4); with several agents the
    // largest one wins.
    let maxMs = 0;
    for (const a of agents) {
      const role = rosterLine(sd, a).split('\t')[3] ?? '';
      const t = roleTimeoutMs(role, ctx, env, cwd);
      if (t > maxMs) maxMs = t;
    }
    timeout = String(maxMs);
  }
  try {
    return waitFor(agents, { sd, ctx, env, timeoutMs: Number(timeout), any, cwd });
  } catch (e) {
    if (e instanceof DieError) dieFriction(e.message, e.code);
    throw e;
  }
}
