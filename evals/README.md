# Evaluation kit

This kit is repository tooling, not part of the distributable skill. It prepares isolated worker copies and runs hidden implementer probes; it does not launch models or create result records.

## Fixtures

- `fixtures/implementer-prune/` is a no-dependency Node fixture. Workers receive `brief.md` and `src/prune.mjs`; `probes/` is hidden until evaluation. Only `src/prune.mjs` is owned.
- `fixtures/reviewer-seeded/` is a static seeded review fixture. Workers receive `brief.md`, `change.diff`, and `base/transfer.mjs`; `ANSWER-KEY.md` is withheld. It has exactly three seeded defects.
- Each `MANIFEST.sha256` lists every worker-delivered file and its SHA-256, with paths relative to that fixture. The manifest itself, probes, answer key, and generated `.eval-run.json` are not worker-delivered files.

Use the same frozen fixture and exact brief bytes for every run being compared. Follow [`../docs/evaluation-protocol.md`](../docs/evaluation-protocol.md) for limits, dispatch, measurement, recording, rubrics, and reporting. Prepare a new destination for every worker, outside this repository. The destination must not already exist.

## Prepare an isolated copy

From the repository root, run:

```sh
node evals/prepare.mjs evals/fixtures/implementer-prune /tmp/prune-run-a
```

Or prepare the reviewer fixture similarly:

```sh
node evals/prepare.mjs evals/fixtures/reviewer-seeded /tmp/reviewer-run-a
```

The command validates the fixture manifest, refuses a destination inside the repository, copies only manifest-listed files, and writes `.eval-run.json` containing the fixture name, preparation timestamp, and `manifest_ok: true`. The destination must be fresh and outside the repository (including through symlinks). Do not place hidden probes or answer keys in the worker copy.

The worker's brief and source are now at the prepared destination. Dispatch exactly the role/brief in that copy, and do not share the writable destination with another run.

## Run hidden probes

After collecting the implementer's report, run:

```sh
node evals/run-probes.mjs evals/fixtures/implementer-prune /tmp/prune-run-a
```

The runner copies the prepared source into its own temporary directory, adds the fixture's hidden probes there, runs Node's test runner against that isolated temporary copy, and removes the temporary directory. It never runs tests in the worker destination. It also compares the prepared manifest to the final worker copy and reports changed, added, or deleted paths outside `Owned files` as scope violations.

Standard output is one JSON object:

```json
{"fixture":"implementer-prune","probes":{"passed":0,"total":6,"failed":["probe name"]},"scope_violations":[],"manifest_ok":true}
```

Counts and failed names are the test runner's observed result; the example is illustrative, not a pilot result. Exit status is `0` if measurement completed, including when probes fail or scope violations exist. Exit status is `2` for invalid usage, fixture/manifest errors, or inability to measure. Diagnostics for invalid use go to stderr; do not treat an error as a measured failure. The reviewer fixture has no executable hidden probes; adjudicate its report against `ANSWER-KEY.md` only after the report is fixed.

## Reproducibility and records

The `prepare` and `run-probes` commands use only Node built-ins. No dependencies need installation. Keep each original fixture and manifest unchanged during a comparison. A fixture/brief correction requires a new fixture version and fresh comparison, not a silent patch to an in-flight run. The protocol defines the result JSON at `evals/results/<YYYY-MM-DD>-<fixture>-<label>.json`; this starter kit creates no results and the pilot belongs to the orchestrator.
