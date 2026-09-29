import fs from 'node:fs';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const here = path.dirname(fileURLToPath(import.meta.url));
const values = [0, -0, -0.02, -0.05, 0.02, 0.05, 0.15, -0.15, 2.25, -2.25];
const cases = values.map((value) => {
  const rounded = Number(value.toFixed(1));
  return {
    input: Object.is(value, -0) ? '-0' : String(value),
    fixed: value.toFixed(1),
    rounded,
    negativeZero: Object.is(rounded, -0),
    rendered: rounded.toFixed(1),
  };
});

fs.writeFileSync(path.join(here, 'rounding.json'), JSON.stringify(cases, null, 2) + '\n');
