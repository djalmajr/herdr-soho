import test from 'node:test';
import assert from 'node:assert/strict';
import { compareCodeUnits } from '../lib/text.mjs';

// Mutation captured: localeCompare puts `a` before `B` and `é` before `f`.
test('compareCodeUnits follows default JavaScript sort order', () => {
  for (const values of [['B', 'a'], ['é', 'f'], ['\uE000', '😀']]) {
    assert.deepEqual(values.toSorted(compareCodeUnits), values.toSorted());
  }
  assert.equal(compareCodeUnits('same', 'same'), 0);
});
