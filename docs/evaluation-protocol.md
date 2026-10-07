# Evaluation protocol

## Purpose

This protocol evaluates agent work from controlled, reproducible runs rather than anecdotes. It separates what was directly observed from what an evaluator inferred, and defines role-specific measures before any model is run. The first pilot covers only `implementer` and `reviewer`; definitions for the other roles are included for later use, not as a claim that they have been piloted.

The results are evidence about the tested brief, fixture, environment, model, and effort—not a general ranking of models. The observations in `skills/herdr-soho/references/agent-profiles.md` are experience reports, not comparable benchmark results.

## What a comparable run is

A run is comparable only to runs with the same role, task, fixture, and evaluation conditions:

- **Same brief:** the bytes of the dispatched brief match exactly; record its SHA-256. Do not silently amend it. If an amendment is unavoidable, count it and treat the run as amended rather than as an unamended comparison.
- **Frozen fixture:** verify every worker-delivered file against the fixture's `MANIFEST.sha256` before preparing a run. Preserve that fixture and manifest for all runs in the comparison.
- **Isolated copy:** prepare a fresh copy for each execution outside the repository. Do not let a worker inspect hidden probes or an answer key. Do not share a writable run directory between workers.
- **Equal limits:** apply the same timeout and declared effort to runs being compared. Record the actual values; a missing value is `unknown`, not an inferred default.
- **One role, one worker:** dispatch the role alone, with no edits from other workers. If an amendment or outside edit is made, count it in `amendments` and describe it. Do not merge another role's work into the run.
- **Independent evaluation:** keep probes and answer keys outside the worker copy. Run probes or adjudicate seeded findings only after the worker report is collected.
- **Run identity:** use one JSON result per execution, as specified under Recording. Do not combine runs or average unlike measures.

A run of a different role or fixture is not a direct comparison, even if the same model and effort were used.

## Roles and rubrics

Scores use the observable criteria below, on an ordinal scale: 0 is no usable evidence of role performance; 1 is limited; 2 is substantially successful with a stated shortcoming; 3 meets the stated role criteria. Record the underlying metrics as well as the score. Do not turn ordinal scores or unlike metrics into a cross-role ranking.

### implementer

**Metrics**

- **Hidden probes passed:** number of hidden probe tests passing divided by total hidden probe tests (`x/N`); obtain both integers from `herdr-soho-eval probes`. Unit: tests. A probe passes only when its native process exits successfully and its test results are consistent.
- **Scope violation:** whether any path outside the brief's `Owned files` changed, was added, or was deleted, determined by comparing the prepared manifest with the completed copy. Unit: yes/no; retain the violating path list. Changes to an owned path are not scope violations.
- **Honest report:** count of report work items explicitly marked `[done]` whose pre-mapped probe fails. Map each probe to its report item before dispatch. Unit: items. Do not count unsupported narrative claims unless they can be mapped to a listed item.
- **Time to first patch:** first observed time an owned file differs from the prepared copy minus `dispatched_at`. Record `first_patch_at`; if no patch is observed, use `null`. Unit: minutes. The observer records the timestamp when first detected; do not infer it from a final file mtime.
- **Time to report:** report file's first observed write time minus `dispatched_at`, recorded as `report_at`. If only a final report mtime is available, label that limitation in `observations`; unit: minutes.
- **Amendments:** number of amendments sent to the worker after dispatch. Unit: amendments. An outside worker edit is also described in observations and counted if it changed the brief or patch.
- **Cost:** record token/cost values only when the execution panel exposes them. Preserve the panel's measure and units; otherwise use the exact string `not observable`. Do not estimate cost from elapsed time or model pricing.

**Rubric (0–3)**

- **0:** no usable patch or report was returned, or no hidden probe passes.
- **1:** at least one hidden probe passes, but not all; or any out-of-scope path changed.
- **2:** all hidden probes pass and there is no scope violation, but one or more `[done]` items are contradicted by their mapped probes, or the report omits a material limitation visible in the probe results.
- **3:** all hidden probes pass, there is no scope violation, no `[done]` item is contradicted by its mapped probe, and the report accurately states the measured results and limitations.

When more than one condition applies, use the lowest applicable score.

### reviewer

**Metrics**

- **Seeded-defect recall:** seeded defects correctly identified divided by seeded defects in the answer key (`found/seeded`). Unit: defects. A finding counts once when its location and failure mechanism match the key; paraphrases are acceptable.
- **Precision / false positives:** true seeded findings divided by all findings (`found / (found + false_positives)`); report the raw `false_positives` count too. Unit: fraction and findings. A non-seeded finding counts as false positive only after adjudication against the fixture and stated scope.
- **Severity calibration:** seeded findings assigned the exact expected severity divided by seeded findings found; record the exact-match numerator as `severity_ok`. Unit: fraction and findings. Missing or ambiguous severity is not an exact match.
- **Evidence executed vs inferred:** count separately the finding claims supported by a command/test actually run with recorded result and the claims based only on inspection or reasoning. Unit: findings. An attempted command without a captured result is not executed evidence. Keep the records in `observations` and `inferences` respectively.
- **Time to report:** report's first observed write time minus `dispatched_at`, recorded as `report_at`; unit: minutes. If only final mtime is available, state this in `observations`.

**Rubric (0–3)**

- **0:** no actionable seeded defect is found, or no usable review report is returned.
- **1:** at least one seeded defect is found, but fewer than half of seeded defects are found, or false positives outnumber true findings.
- **2:** at least half of the seeded defects are found and false positives do not outnumber the hits, but a seeded defect is missing, a false positive remains, a severity does not match, or a claimed execution is not supported.
- **3:** all seeded defects are found, there are no false positives, every found severity matches the key, and execution/inference claims are accurately separated.

For a zero-defect fixture, recall and severity calibration are undefined; do not score it with this rubric. When conditions conflict, use the lowest applicable score.

### scouter/researcher

**Metrics**

- **Fact accuracy:** correct, answerable factual claims divided by all answerable claims in the prewritten answer key. Unit: claims and fraction. A claim is correct only when it matches the frozen source evidence; unsupported claims are incorrect. Mark unanswerable claims separately and exclude them from the denominator.
- **Lookup-function citation:** whether the report cites the specific lookup function/command required by the applicable skill rule, with a source location or captured invocation that can be checked against that rule. Unit: yes/no. Do not substitute a plausible or similarly named lookup.
- **Time:** first observed report write minus dispatch time. Unit: minutes; use the same timestamp rule as the other roles.

**Rubric (0–3)**

- **0:** no usable report, or no answer-key facts are correct.
- **1:** some answer-key facts are correct, but fewer than half; or a required lookup is omitted or misidentified.
- **2:** at least half of answer-key facts are correct and the lookup is correctly cited, but at least one factual claim lacks a checkable source or is incorrect.
- **3:** every answerable fact matches the key, the required lookup is correctly cited, and claims are traceable to source evidence.

Use the lowest applicable score.

### documenter

**Metrics**

- **Verified-claim rate:** factual assertions checked against the frozen code/source and found correct divided by all checkable factual assertions in the document. Unit: claims and fraction. Count an assertion once; stylistic statements are excluded. Report the numerator and denominator.
- **Invented flags or names:** count of flags, commands, APIs, configuration keys, or public names asserted as real that do not exist in the frozen source or authorized brief. Unit: items. Do not count a clearly labelled proposal as an existing feature.
- **Time:** first observed report/document write minus dispatch time. Unit: minutes.

**Rubric (0–3)**

- **0:** no usable document, or fewer than half of checkable factual claims are correct.
- **1:** at least half are correct, but at least one invented flag/name materially misleads a reader, or the document does not distinguish implemented from unimplemented behavior.
- **2:** all checkable claims are correct and no materially misleading invented name appears, but one or more claims are not traceable or an implemented/unimplemented distinction is missing.
- **3:** all checkable claims are correct and traceable, no invented existing flag/name is present, and implemented behavior is clearly distinguished from proposals or limitations.

Use the lowest applicable score.

### specialist

This category includes specialist variants such as `security-reviewer` and `designer`; evaluate only against a role-specific frozen brief and seeded answer key. Do not compare those runs to one another or to implementer/reviewer runs.

**Metrics**

- **Seeded vulnerability recall:** vulnerabilities correctly identified divided by vulnerabilities seeded in the key (`found/seeded`). Unit: vulnerabilities. A finding must match the vulnerability's location and exploit/failure mechanism.
- **Precision / false positives:** true seeded findings divided by all findings, with false-positive count recorded after adjudication. Unit: fraction and findings.
- **Time:** first observed report write minus dispatch time. Unit: minutes.

**Rubric (0–3)**

- **0:** no usable report or no seeded vulnerability found.
- **1:** at least one vulnerability found, but fewer than half are found, or false positives outnumber true findings.
- **2:** at least half are found and false positives do not outnumber the hits, but a seeded vulnerability is missing or a false positive remains.
- **3:** all seeded vulnerabilities are found and there are no false positives.

Use the lowest applicable score. This rubric measures only the seeded fixture, not general security or design quality.

## Procedure

1. Select a role and a frozen fixture. Define the brief, timeout, effort, owned paths, expected probe-to-report mapping (for implementer), and answer key (for reviewer) before dispatch. Hash the exact dispatched brief.
2. Verify the fixture manifest, then use `go run ./cmd/herdr-soho-eval prepare <fixture> <fresh-dest>` from the repository root for a new destination outside the repository. Prepare a distinct copy per run. Confirm that hidden probes and answer keys are absent.
3. Dispatch one worker for the selected role with the exact brief bytes. Record `dispatched_at`, model, effort, timeout, and the run label. Observe owned files for the first patch and record the first observed timestamp; do not reconstruct it later from a changed file's mtime.
4. Collect the worker's report. Record its first observed write as `report_at` (or record the mtime limitation). Count amendments and capture only panel-visible cost/token data.
5. For implementer, run `go run ./cmd/herdr-soho-eval probes <fixture> <dest>` after report collection. For reviewer, adjudicate each finding against the hidden answer key only after the report is fixed. Record false positives and exact severity matches.
6. Write one result JSON per execution. Keep observations (directly seen facts and executed outputs) separate from inferences. Retain the original fixture, manifest, worker copy, report, and result together under the run's archival controls.
7. Repeat with a clean copy and unchanged conditions for each planned execution. Do not alter a rubric or probe after seeing a model result; if a correction is necessary, version the fixture and start a new comparison.

## Recording

Store one JSON document per execution at `evals/results/<YYYY-MM-DD>-<fixture>-<label>.json`. Do not create result files for the starter kit or invent pilot results. The object has these fields:

```json
{
  "fixture": "fixture directory name",
  "role": "implementer or reviewer",
  "kind": "execution kind or unknown",
  "model": "model identifier or unknown",
  "effort": "declared effort or unknown",
  "brief_sha256": "64 lowercase hexadecimal characters",
  "dispatched_at": "timestamp with timezone",
  "first_patch_at": null,
  "report_at": "timestamp with timezone or null",
  "amendments": 0,
  "probes": null,
  "review": null,
  "cost": "not observable",
  "observations": [],
  "inferences": []
}
```

For implementer, `probes` is the `probes` object from `herdr-soho-eval probes` (`passed`, `total`, and `failed`); `review` is `null`. For reviewer, `probes` is `null` and `review` is `{ "seeded": N, "found": N, "false_positives": N, "severity_ok": N }`. `severity_ok` counts exact expected-severity matches among found seeded defects. `observations` and `inferences` are arrays of objects, each with a `claim` string; an observation also has an `evidence` string identifying a captured output/source, while an inference has a `basis` string. Record the applied timeout and any condition deviations as observations, because the fixed top-level schema has no timeout field. Keep the arrays separate; never label an inference as executed evidence. Use explicit JSON `null` for unavailable timestamps or inapplicable role-specific fields. `cost` is exactly `not observable` unless a panel provides values; if so, store an object preserving the panel's units and values without estimating or converting them.

Timestamps are ISO-8601 with timezone. `dispatched_at` is the dispatch event time. `first_patch_at` is the first observed change to an owned file (implementer only). `report_at` is first observed report write time when available; otherwise null and explain a final-mtime proxy in `observations`. Counts are non-negative integers. The filename date is the dispatch date in the dispatch timezone; fixture and label use filesystem-safe names.

## Reporting rules

- Label direct observations and inferences separately. Paste command output as observed; distinguish a failed/unavailable command from a successful probe.
- State the sample size `n` for every summary and specify which role, fixture, brief hash, model/effort condition, and time window it covers. With `n=1`, describe only that run; do not imply population performance.
- State limitations: fixture coverage, hidden-test coverage, environment/tooling, timestamp precision, missing panel data, and any deviations or amendments.
- Report raw counts and denominators alongside rates. Undefined rates (for example, precision with no findings) are `not applicable`, not zero.
- The `stats` command aggregates task counts, duration, partial counts, and review verdict/severity headers. Those fields are not role-normalized benchmark outcomes. **Do not rank models from heterogeneous aggregates produced by `stats`**, or combine role-specific metrics into a single score.
- Do not claim causality or general capability from a single fixture, one run, or a heterogeneous sample. Recommendations must state the sample size and limitations and remain scoped to the tested conditions.

## Pilot scope

The first pilot includes only `implementer` and `reviewer`, using the reproducible fixtures in `evals/fixtures/implementer-prune/` and `evals/fixtures/reviewer-seeded/`. The other role definitions above are protocol definitions only; do not dispatch or report pilot results for `scouter/researcher`, `documenter`, or `specialist` in this cycle. No model is run by the kit implementation itself.
