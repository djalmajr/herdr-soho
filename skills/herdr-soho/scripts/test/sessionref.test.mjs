// Session references (`[machine/]<pane id>`): parse, canonical format and
// the herdr arguments that target the machine.
import test from 'node:test';
import assert from 'node:assert/strict';
import { LOCAL_MACHINE, formatRef, herdrMachineArgs, parseRef } from '../lib/sessionref.mjs';

// Mutation captured: defaulting a bare pane id to anything but the local
// machine, or keeping the surrounding whitespace, changes these values.
test('parseRef: a bare pane id is local; a machine prefix is kept', () => {
  assert.deepEqual(parseRef('w12:p1'), { machine: LOCAL_MACHINE, paneId: 'w12:p1' });
  assert.deepEqual(parseRef('  windows/w3:p1\n'), { machine: 'windows', paneId: 'w3:p1' });
  assert.deepEqual(parseRef('local/w14:pR'), { machine: 'local', paneId: 'w14:pR' });
  assert.deepEqual(parseRef('hetzner.eu-1/w3:p2'), { machine: 'hetzner.eu-1', paneId: 'w3:p2' });
});

// Mutation captured: a looser pane pattern (or no machine check) accepts
// names, paths and extra segments as references.
test('parseRef: anything that is not a reference is null', () => {
  for (const bad of ['', 'soho-s1', 'w12', 'w12:', ':p1', 'p1', 'w12:p1:extra', 'a/b/w12:p1',
    '/w12:p1', 'win dows/w3:p1', '-x/w3:p1', 'windows/', 'C:/Users/x', undefined, null, 42]) {
    assert.equal(parseRef(bad), null, String(bad));
  }
});

test('formatRef: always names the machine; round-trips with parseRef', () => {
  assert.equal(formatRef({ machine: 'windows', paneId: 'w3:p1' }), 'windows/w3:p1');
  assert.equal(formatRef({ machine: '', paneId: 'w12:p1' }), 'local/w12:p1');
  for (const ref of ['local/w12:p1', 'windows/w3:p1']) assert.equal(formatRef(parseRef(ref)), ref);
});

test('herdrMachineArgs: nothing for the local server, --machine otherwise', () => {
  assert.deepEqual(herdrMachineArgs(LOCAL_MACHINE), []);
  assert.deepEqual(herdrMachineArgs(''), []);
  assert.deepEqual(herdrMachineArgs('windows'), ['--machine', 'windows']);
});
