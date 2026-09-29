import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { applyKey, createState, entryLine, feedChunk, filterEntries, flushEsc, render, stripControls } from '../../../plugin/picker.mjs';

const entries = [
  { ref: 'local/w12:p1', machine: 'local', workspace_id: 'w12', tab_id: 'w12:t1', pane_id: 'w12:p1', name: 'orchestrator-10', kind: 'claude', status: 'working', workspace_label: 'appliance', tab_label: '1', cwd: '/Users/dj4lm/repo' },
  { ref: 'local/w14:pW', machine: 'local', workspace_id: 'w14', tab_id: 'w14:tW', pane_id: 'w14:pW', name: null, kind: null, status: 'idle', workspace_label: 'soho', tab_label: '1', cwd: '/tmp/soho' },
  { ref: 'windows/w3:p1', machine: 'windows', workspace_id: 'w3', tab_id: 'w3:t1', pane_id: 'w3:p1', name: 'orchestrator', kind: 'codex', status: 'working', workspace_label: 'pinar', tab_label: '1', cwd: 'C:\\Users\\dj4lm\\pinar' },
  { ref: 'windows/w4:p𝄞', machine: 'windows', workspace_id: 'w4', tab_id: 'w4:t1', pane_id: 'w4:p𝄞', name: 'emoji𝄞name', kind: 'codex', status: 'working', workspace_label: 'pinar', tab_label: '2', cwd: 'C:\\𝄞\\repo' },
];
const outputs = {
  filters: ['','codex pinar','pinar codex','orchestrator idle','WINDOWS','C:\\USERS\\DJ4LM','𝄞name'].map((query) => [query, filterEntries(entries, query).map((e) => e.ref)]),
  strips: ['a\tB\nC\x00D\x01E\x7fF\u0080G', 'evil\x1b[2J\x1b[31mRED\x1b]52;c;UEFO\x07', 'a\x1b]52;c;UEFO\x1b\\b', 'a\x1b[31m', 'a\x1b]unterminated', 'olá — referência 𝄞'],
  lines: [40,80].map((width) => [width, entries.map((e) => entryLine(e,width))]),
  renders: [40,80].map((width) => { const s=createState(); s.entries=entries; s.query='pinar codex'; s.selected=0; s.loading=1; s.failures=[{label:'windows',cause:'exit 1: boom\x1b[2J'}]; return [width,render(s,width)]; }),
  keys: ['up','down','down','p','i','n','a','r','backspace','backspace','é','𝄞','enter'],
  feed: [['\x1b','B'], ['\x1b','[A'], ['\x1b[1;5B'], ['é𝄞'], ['pinar','\x7f'], ['\x03']],
};
outputs.strips = outputs.strips.map(stripControls);
const keyState=createState(); keyState.entries=entries;
outputs.keyResults=outputs.keys.map((key) => { const result=applyKey(keyState,key); return {query:keyState.query,selected:keyState.selected,exit:keyState.exit ?? '',result:result ?? '',copied:keyState.copied}; });
outputs.feedResults=outputs.feed.map((chunks) => { const feedState=createState(); feedState.entries=entries; const result=chunks.map((chunk)=>feedChunk(feedState,chunk)); return {query:feedState.query,selected:feedState.selected,exit:feedState.exit ?? '',esc:feedState._esc,csi:feedState._csi,result:result.at(-1) ?? '',flush:flushEsc(feedState) ?? ''}; });
outputs.entries=entries;
const target = path.join(path.dirname(fileURLToPath(import.meta.url)), 'picker.json');
fs.writeFileSync(target, `${JSON.stringify(outputs,null,2)}\n`);
