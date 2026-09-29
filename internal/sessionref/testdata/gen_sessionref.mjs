import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { LOCAL_MACHINE, formatRef, herdrMachineArgs, parseRef } from '../../../skills/herdr-soho/scripts/lib/sessionref.mjs';
const inputs = ['w12:p1', '  windows/w3:p1\n', 'local/w14:pR', 'hetzner.eu-1/w3:p2', '\u00a0w1:p2\u2028', '', 'soho-s1', 'w1:p1\rx', 'w1:p1\x1b', 'w1:p1😀', 'wi\u2028ndows/w3:p1', 'wi\u00a0ndows/w3:p1', 'wi😀ndows/w3:p1', null, 42];
const rows = inputs.map(input => ({kind:'parse', input, expected:parseRef(input)}));
for (const [machine,pane] of [['windows','w3:p1'],['','w12:p1'],[LOCAL_MACHINE,'w1:p9']]) rows.push({kind:'format',machine,pane,expected:formatRef({machine,paneId:pane})});
for (const machine of [LOCAL_MACHINE,'','windows']) rows.push({kind:'args',machine,expected:herdrMachineArgs(machine)});
fs.writeFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)),'sessionref.json'), JSON.stringify(rows,null,2)+'\n');
