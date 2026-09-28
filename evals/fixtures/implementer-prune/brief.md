# Brief — prune old backups

Role: implementer · Agent: evaluator · Run: frozen-fixture · Report language: en

## Goal

Implement `pruneBackups({ dir, keep, now })` in `src/prune.mjs`. Remove backups older than the newest `keep` backups. Never remove the newest backup. The function resolves after pruning and returns the number of backup files removed.

## Decisions already made

- A backup is a regular file directly inside `dir` named `backup-<sequence>-<timestamp>.json`, where `<sequence>` is exactly 16 decimal digits and `<timestamp>` is exactly `YYYYMMDDTHHmmssSSSZ` (UTC). Example: `backup-0000000000000012-20260927T211950000Z.json` matches; `backup-final.json` does not.
- The sequence is assigned by the publisher, is unique, and strictly increases for each successfully published backup. It does not wrap or get reused. Sequence order—not filename timestamp, mtime, directory order, or the supplied wall clock—defines recency. The largest sequence is the newest.
- Backup file contents are opaque; pruning must not open or parse them. Temporary files, malformed names, directories, and symlinks are not backups and must not be removed.
- `keep` is a safe integer greater than or equal to 1. `now` is a valid `Date`; it represents the caller's current wall clock but must not determine ordering or cause a backup to be deleted. Invalid arguments fail before any file is removed.
- If two valid backup filenames have the same sequence, fail before removing any file; do not guess which is newer.
- On a crash during pruning, any already removed files are older than every retained backup. Repeating the same call converges to the same retained set. A crash after a new backup is published but before pruning leaves both old and new files; a later call keeps the newest according to sequence.
- There are no dependencies. Do not edit paths other than `src/prune.mjs`.

## Expected result

`src/prune.mjs` exports the named function `pruneBackups`. It removes exactly `max(0, backupCount - keep)` oldest backup files, returning the removed count. If there are no backups, it returns `0`.

## Acceptance criteria

1. The function validates `dir`, `keep`, and `now` and does not mutate the directory on invalid input.
2. It retains the highest sequences regardless of timestamps or mtime, and ignores non-backup entries.
3. Repeating pruning after success removes nothing; a backwards `now` does not change which files are retained.
4. Hidden probes pass: `node evals/run-probes.mjs evals/fixtures/implementer-prune <prepared-destination>`.

## Failure matrix

- [crash] A new backup is fully published before pruning starts, then pruning is interrupted after any older backup deletion. The published highest-sequence backup and every not-yet-deleted file survive. A subsequent retry retains the newest `keep` files. Local probes cover the published-but-not-pruned boundary and a partially pruned directory representing an interruption after an old-file deletion; actual process termination/power-loss durability is operational proof and is not claimed by this fixture.
- [retry] Running the same pruning request again converges: the second call removes zero more backups, with the same retained names. The local probe verifies this.
- [clock] `now` moves backwards between calls while a higher sequence has an older filename timestamp. The highest sequences remain; no needed backup is pruned. The local probe verifies this.

## When the brief does not decide

Do not choose behavior that changes the contract. Mark it `[partial]`, record the gap and available options in the report, and continue with work that is defined.

## Owned files

- `src/prune.mjs` — implement the function

## Forbidden

- Do not modify, add, or delete any other path.
- Do not add dependencies or invoke external services.

## Report

List changed files, tests/checks and their literal results, hidden-probe status if provided after the run, any scope deviation, and limitations. Mark work `[done]`, `[partial]`, or `[skipped]` honestly.
