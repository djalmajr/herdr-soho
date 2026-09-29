import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { setupBlock, setupHookReminder, setupHookDoctor, legacyHookReminder, legacyHookDoctor, setupBlockResult, settingsHooksResult } from '../../../skills/herdr-soho/scripts/lib/setuptext.mjs';
const inputs=[null,'','hello','hello\n',`before\n<!-- herdr-soho:start -->\nold\n<!-- herdr-soho:end -->\nafter\n`,`<!-- herdr-agents:start -->\nold\n<!-- herdr-agents:end -->\n`,`<!-- herdr-agents:start -->\nno end\n`,' \ufeff','{}','{"other":{"x":1},"hooks":{"PreToolUse":[{"hooks":[{"type":"command","command":"keep"}]}]}}','NOT-JSON','42','[1,2]','{"hooks":[]}','{"hooks":{"UserPromptSubmit":{}}}'];
const rows=[{kind:'block',expected:setupBlock()},{kind:'reminder',expected:setupHookReminder()},{kind:'doctor',expected:setupHookDoctor()},{kind:'legacy-reminder',expected:legacyHookReminder()},{kind:'legacy-doctor',expected:legacyHookDoctor()}];
inputs.push('{"hooks":{"SessionStart":[{"hooks":false}]}}');
for(const input of inputs){rows.push({kind:'block-result',input,expected:setupBlockResult(input)});rows.push({kind:'hooks-result',input,expected:settingsHooksResult(input)});}
fs.writeFileSync(path.join(path.dirname(fileURLToPath(import.meta.url)),'setuptext.json'),JSON.stringify(rows,null,2)+'\n');
