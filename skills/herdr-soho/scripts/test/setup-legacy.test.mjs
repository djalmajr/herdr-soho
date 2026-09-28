// Legacy (herdr-agents) migration, pure text/merge layer (lib/setuptext.mjs):
// the pinned hashes of the two legacy hook literals, in-place replacement of
// the pre-rename block by setupBlockResult, and the settings merge removing
// the exact legacy hook commands while keeping user commands that only
// mention herdr-agents. No files are written here, so no fixture root is
// needed (the sibling setup.test.mjs scaffolding is for the file-level
// helpers).
import test from 'node:test';
import assert from 'node:assert/strict';
import { createHash } from 'node:crypto';
import {
  SETUP_START,
  SETUP_END,
  LEGACY_SETUP_START,
  LEGACY_SETUP_END,
  setupBlock,
  setupHookReminder,
  setupHookDoctor,
  legacyHookReminder,
  legacyHookDoctor,
  setupBlockResult,
  settingsHooksResult,
} from '../lib/setuptext.mjs';

const BLOCK = setupBlock();
const OLD_LEGACY_BLOCK = `${LEGACY_SETUP_START}\nold legacy line\n${LEGACY_SETUP_END}\n`;
const sha256 = (s) => createHash('sha256').update(s, 'utf8').digest('hex');

// Mutation captured: editing either literal (or deriving it from the
// current hooks at run time) changes the hash/length pair below.
test('legacy hooks: pinned sha256 and length of the exact pre-rename commands', () => {
  assert.equal(sha256(legacyHookReminder()), '76ecb6d012d80693694393dbd61fe423132cbe1cad4e805984ca59a479e57f7e');
  assert.equal(legacyHookReminder().length, 220);
  assert.equal(sha256(legacyHookDoctor()), 'aeefff35cf1dded4818f047ede0bbd5c3cc2aa0812c378e913dc694a5ae59535');
  assert.equal(legacyHookDoctor().length, 526);
});

// Mutation captured: dropping LEGACY_SETUP_START from the entry condition
// (or the line loop) sends a legacy-block file down the append path.
// Mutation captured: regenerating the block (instead of renaming it in
// place) drops the project's own line; renaming outside the block rewrites
// text that merely mentions the old name.
test('setupBlockResult: a legacy block is renamed in place and keeps its own text', () => {
  const own = `before herdr-agents\n${LEGACY_SETUP_START}\n## Multi-agent workflow (herdr-agents)\nLoad /herdr-agents; config in .agents/herdr-agents.conf; HERDR_AGENTS_DIR.\nOur own rule: name workers by job.\n${LEGACY_SETUP_END}\nafter herdr-agents\n`;
  assert.equal(setupBlockResult(own),
    `before herdr-agents\n${SETUP_START}\n## Multi-agent workflow (herdr-soho)\nLoad /herdr-soho; config in .agents/herdr-soho.conf; HERDR_SOHO_DIR.\nOur own rule: name workers by job.\n${SETUP_END}\nafter herdr-agents\n`);
  // no trailing newline is kept as is
  assert.equal(setupBlockResult(`${LEGACY_SETUP_START}\nold\n${LEGACY_SETUP_END}`), `${SETUP_START}\nold\n${SETUP_END}`);
  // an unterminated legacy block is refused (null), like an incomplete result
  assert.equal(setupBlockResult(`a\n${LEGACY_SETUP_START}\nno end\n`), null);
  // with both kinds present, the legacy markers read as the current ones (replace)
  assert.equal(setupBlockResult(`${SETUP_START}\nx\n${SETUP_END}\n${LEGACY_SETUP_START}\ny\n${LEGACY_SETUP_END}\n`), `${BLOCK}${BLOCK}`);
});

// Mutation captured: matching the word herdr-agents (instead of the full
// legacy markers) rewrites content that merely mentions the old name.
test('setupBlockResult: a file without the legacy markers is byte-identical to today', () => {
  // the word herdr-agents in the text is not a marker: append, as today
  const plain = 'this project used to run herdr-agents panes\n';
  assert.equal(setupBlockResult(plain), `${plain}\n${BLOCK}`);
  // the current markers keep their exact behavior
  assert.equal(setupBlockResult(`x\n${SETUP_START}\ny\n${SETUP_END}\n`), `x\n${BLOCK}`);
  // absent and empty files are unchanged by the migration
  assert.equal(setupBlockResult(null), BLOCK);
  assert.equal(setupBlockResult(''), `\n${BLOCK}`);
});

// Mutation captured: dropping the legacy comparison in the merge keeps the
// legacy entries stacked beside the new ones.
test('settingsHooksResult: the exact legacy commands are removed like the current ones', () => {
  const seed = JSON.stringify({
    hooks: {
      UserPromptSubmit: [
        { hooks: [{ type: 'command', command: legacyHookReminder() }] },
        { hooks: [{ type: 'command', command: setupHookReminder() }] },
      ],
      SessionStart: [
        { hooks: [{ type: 'command', command: legacyHookDoctor() }] },
        { hooks: [{ type: 'command', command: setupHookDoctor() }] },
      ],
    },
  });
  const merged = settingsHooksResult(seed);
  const doc = JSON.parse(merged);
  assert.equal(doc.hooks.UserPromptSubmit.length, 1, 'legacy + current collapse into the one new entry');
  assert.equal(doc.hooks.UserPromptSubmit[0].hooks[0].command, setupHookReminder());
  assert.equal(doc.hooks.SessionStart.length, 1);
  assert.equal(doc.hooks.SessionStart[0].hooks[0].command, setupHookDoctor());
  const again = settingsHooksResult(merged);
  assert.equal(again, merged, 'a re-run is stable: no duplicates stack');
});

// Mutation captured: substring matching on herdr-agents drops the user
// commands below.
test('settingsHooksResult: a user command that only contains herdr-agents is kept', () => {
  const userReminder = "sh -c 'my-herdr-agents-thing run'";
  const userDoctor = 'echo herdr-agents legacy cleanup';
  const seed = JSON.stringify({
    hooks: {
      UserPromptSubmit: [{ hooks: [{ type: 'command', command: userReminder }] }],
      SessionStart: [{ hooks: [{ type: 'command', command: userDoctor }] }],
    },
  });
  const doc = JSON.parse(settingsHooksResult(seed));
  assert.equal(doc.hooks.UserPromptSubmit.length, 2);
  assert.equal(doc.hooks.UserPromptSubmit[0].hooks[0].command, userReminder, 'user command kept');
  assert.equal(doc.hooks.UserPromptSubmit[1].hooks[0].command, setupHookReminder(), 'new entry appended at the end');
  assert.equal(doc.hooks.SessionStart.length, 2);
  assert.equal(doc.hooks.SessionStart[0].hooks[0].command, userDoctor, 'user command kept');
  assert.equal(doc.hooks.SessionStart[1].hooks[0].command, setupHookDoctor());
});
