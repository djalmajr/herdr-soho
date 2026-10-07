# evals/test/fixtures

Durable implementation controls for the `implementer-prune` fixture.

Each control is a complete `src/prune.go` source stored as data (`.go.txt`) and consumed by the native integration test `internal/eval/fixture_controls_test.go`, which prepares the real distributed fixture through `eval.Prepare` and measures it through `eval.Probes`.

The controls are never copied into a prepared worker: prepare copies only the manifest-listed files, and the test replaces the owned `src/prune.go` explicitly before each measured run.

## Controls

- `reference-prune.go.txt` — the reference implementation of the six-probe contract: sequence-ordered pruning of regular backup files, duplicate-sequence and invalid-argument refusal before any mutation, and ignored temporary files, directories and symlinks.
- `mutation-duplicate-prune.go.txt` — the reference with only the duplicate-sequence guard omitted: a repeated sequence is accepted and the older file is pruned, so the duplicate probe fails after a mutation.
- `mutation-unrelated-prune.go.txt` — the reference that additionally removes only the unrelated entries incorrectly: `.tmp` files and symlinks are deleted before pruning, so the unrelated-entries probe fails while the backup pruning itself stays correct.

## Provenance

The three controls are the Go ports of the retired historical Node controls. The retired source hashes are recorded in `internal/testdata/legacy/retired-sources.sha256`; the native tests consume only the `.go.txt` controls.

- `reference-prune.mjs` -> `reference-prune.go.txt`
- `mutation-duplicate-prune.mjs` -> `mutation-duplicate-prune.go.txt`
- `mutation-unrelated-prune.mjs` -> `mutation-unrelated-prune.go.txt`

The port keeps the observable contract one-to-one: the same backup file pattern, the same safe-integer `keep` validation mapped onto the `Keep float64` wire type, the same wall-clock validation mapped onto `Now time.Time` (the legacy invalid `Date` maps to the zero time), the same sequence ordering, and the same refusal before mutation.

The wire interface is `PruneBackups(Options{Dir string, Keep float64, Now time.Time}) (int, error)` in package `src`, matching the distributed fixture stub.
