import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { writeTaskReportPointer } from '../../../skills/herdr-soho/scripts/lib/taskreport.mjs';

const cases = [
  { version: 1, task_report: '/state/reports/build.md', current: '/tmp/task report.md', history: [] },
  { version: 1, task_report: '/state/relatório-🦌.md', current: '/tmp/時刻.md', history: ['/state/old.md', '/state/old-2.md'] },
];
const out = [];
for (const pointer of cases) {
  const root = fs.mkdtempSync(path.join(os.tmpdir(), 'taskreport-gen-'));
  try {
    await writeTaskReportPointer(root, 'build', pointer);
    out.push({ pointer, serialized: fs.readFileSync(path.join(root, 'task-report-build.json'), 'utf8') });
  } finally { fs.rmSync(root, { recursive: true, force: true }); }
}
const dir = path.dirname(fileURLToPath(import.meta.url));
fs.writeFileSync(path.join(dir, 'taskreport.json'), `${JSON.stringify(out, null, 2)}\n`);
