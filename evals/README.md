# Evaluation kit

This kit is repository tooling, not part of the distributable skill. It prepares isolated worker copies and runs hidden implementer probes; it does not launch models or create result records.

## Fixtures

- `fixtures/implementer-prune/` is a standard-library Go fixture. Workers receive `brief.md`, `go.mod`, and `src/prune.go`; `probes/` is hidden until evaluation. Only `src/prune.go` is owned.
- `fixtures/reviewer-seeded/` is a static seeded Go review fixture. Workers receive `brief.md`, `go.mod`, `change.diff`, and `base/transfer.go`; `ANSWER-KEY.md` is withheld. It has exactly three seeded defects.
- Each `MANIFEST.sha256` lists every worker-delivered file and its SHA-256, with paths relative to that fixture. The manifest itself, probes, answer key, and generated `.eval-run.json` are not worker-delivered files.

Use the same frozen fixture and exact brief bytes for every run being compared. Follow [`../docs/evaluation-protocol.md`](../docs/evaluation-protocol.md) for limits, dispatch, measurement, recording, rubrics, and reporting. Prepare a new destination for every worker, outside this repository. The destination must not already exist.

## Prepare an isolated copy

From the repository root, run:

```console
go run ./cmd/herdr-soho-eval prepare evals/fixtures/implementer-prune ../prune-run-a
```

Or prepare the reviewer fixture similarly:

```console
go run ./cmd/herdr-soho-eval prepare evals/fixtures/reviewer-seeded ../reviewer-run-a
```

The command validates the fixture manifest, refuses a destination inside the repository, copies only manifest-listed files, and writes `.eval-run.json` containing the fixture name, preparation timestamp, and `manifest_ok: true`. The destination must be fresh and outside the repository (including through symlinks). Do not place hidden probes or answer keys in the worker copy.

The worker's brief and source are now at the prepared destination. Dispatch exactly the role/brief in that copy, and do not share the writable destination with another run.

The repository tooling requires Go 1.25 or later. Build `./cmd/herdr-soho-eval` to use the native executable directly. When invoking it outside the repository root, pass `--repo <repository-root>` before the subcommand and use explicit fixture and destination paths.

## Run hidden probes

After collecting the implementer's report, run:

```console
go run ./cmd/herdr-soho-eval probes evals/fixtures/implementer-prune ../prune-run-a
```

The runner copies owned worker source and verified non-owned fixture files into its own temporary directory, adds the hidden Go probes, compiles each probe package once, and runs each expected probe separately through the native Go toolchain. A pass requires a successful process exit and a consistent test result. It never runs tests in the worker destination. It also compares the prepared manifest to the final worker copy and reports changed, added, or deleted paths outside `Owned files` as scope violations. Processes and temporary resources are owned and bounded; the evaluator is not a security sandbox for arbitrary worker code.

Standard output is one JSON object:

```json
{"fixture":"implementer-prune","probes":{"passed":0,"total":6,"failed":["probe name"]},"scope_violations":[],"manifest_ok":true}
```

Counts and failed names are the test runner's observed result; the example is illustrative, not a pilot result. Exit status is `0` if measurement completed, including when probes fail or scope violations exist. Exit status is `2` for invalid usage, fixture/manifest errors, or inability to measure. Diagnostics for invalid use go to stderr; do not treat an error as a measured failure. The reviewer fixture has no executable hidden probes; adjudicate its report against `ANSWER-KEY.md` only after the report is fixed.

## Reproducibility and records

The `prepare` and `probes` commands use the Go standard library and native Go toolchain. Keep each original fixture and manifest unchanged during a comparison. A fixture/brief correction requires a new fixture version and fresh comparison, not a silent patch to an in-flight run. These Go fixtures replace the earlier JavaScript fixtures; historical results from those fixtures are not directly comparable to new runs. The protocol defines the result JSON at `evals/results/<YYYY-MM-DD>-<fixture>-<label>.json`; this kit creates no result records and model runs belong to the orchestrator.
