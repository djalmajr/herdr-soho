// `lint <brief.md> [--role <role>]`: run the dispatch brief diagnostics
// without creating a dispatch, friction entry, or state directory.
import fs from 'node:fs';
import { DieError } from '../config.mjs';
import { roleFile, roleIsEdit } from '../roles.mjs';
import { briefLintFindings } from '../dispatch.mjs';

const USAGE = 'usage: lint <brief.md> [--role <role>]';

export function cmdLint(argv, ctx, env = process.env, cwd = process.cwd()) {
  const args = (argv ?? []).map(String);
  const brief = args[0] ?? '';
  if (brief === '' || brief.startsWith('--')) throw new DieError(USAGE, 2);
  let role = 'implementer';
  if (args.length === 3 && args[1] === '--role' && args[2] !== '' && !args[2].startsWith('--')) {
    role = args[2];
  } else if (args.length !== 1) {
    throw new DieError(USAGE, 2);
  }
  let isFile = false;
  try { isFile = fs.statSync(brief).isFile(); } catch { /* missing brief */ }
  if (!isFile) throw new DieError(`lint: brief not found: ${brief}`, 2);
  if (!roleFile(role, env, cwd)) throw new DieError(`lint: unknown role '${role}'`, 3);

  const findings = briefLintFindings(brief, ctx, env, { readOnly: !roleIsEdit(role, env, cwd) });
  if (findings.mode === 'off') {
    process.stdout.write(`brief ${brief}: lint off (brief_lint=off)\n`);
    return 0;
  }
  for (const warning of findings.warnings) process.stderr.write(`herdr-soho: warning: ${warning}\n`);
  if (findings.missingMessage !== '' && findings.mode === 'strict') {
    process.stderr.write(`herdr-soho: ${findings.missingMessage} (brief_lint=strict)\n`);
    return 2;
  }
  if (findings.missingMessage !== '') process.stderr.write(`herdr-soho: warning: ${findings.missingMessage}\n`);
  if (findings.warnings.length || findings.missingMessage !== '') return 1;
  process.stdout.write(`brief ${brief}: ok\n`);
  return 0;
}
