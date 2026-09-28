# Review brief — seeded transfer change

Role: reviewer · Agent: evaluator · Run: frozen-fixture · Report language: en

## Goal

Review `change.diff` against the supplied `base/` file. Report concrete defects introduced by the diff, with the affected file and line in the post-change file, severity (`P0`–`P3`), failure scenario, and evidence. Separate behavior actually executed from behavior inferred by inspection. The input is intentionally small and the answer key is withheld.

## Decisions already made

- Treat `change.diff` as a unified diff against `base/transfer.mjs`; the changed file path is `base/transfer.mjs` and post-change line numbers are unchanged by this patch.
- `amount` is a finite number expressed in whole-or-fractional transfer units; valid transfers require an amount greater than zero.
- Report findings individually. A finding matches a seeded defect only when its location and failure mechanism identify the same defect; paraphrase is acceptable.
- Use severity where P0 is catastrophic, P1 is high impact, P2 is moderate impact, and P3 is low impact. Do not claim execution without a captured command/test result.
- Do not edit the fixture. No external service, credentials, or additional files are needed.

## Expected result

A review report containing each finding's `file:line`, severity, impact/failure scenario, and whether evidence is executed or inferred. State commands and literal results for checks actually run; otherwise say the review is static. Include any additional finding separately.

## Report

List the changed file reviewed, findings, evidence and checks, limitations, and overall verdict. Mark each finding with the applicable severity. Do not claim a clean review without describing the checks performed.
