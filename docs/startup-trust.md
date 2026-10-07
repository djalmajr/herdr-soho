# Agent startup and workspace trust

Workspace trust and tool approvals are separate decisions. A CLI can accept `approvals=full` and still ask whether it may load configuration, hooks, extensions, or other resources from a new working directory. A disposable worker copy is a new directory and can therefore trigger a first-use prompt even when the original repository is trusted.

## Current behavior

Herdr starts the interactive CLI in the worker pane. If the CLI does not become ready, herdr-soho reports `blocked_at_startup` and exits with code 7 while keeping the pane available for inspection. This generic startup failure does not submit the trust dialog. There is a dedicated Grok trust-dialog guard; there is no equivalent verified adapter for every supported kind.

The native Go CLI also checks Cursor startup on Windows: a launch-only screen, failed or empty read, workspace trust dialog or clipped trust screen does not prove readiness. It keeps the pane available and reports the startup evidence instead of claiming that Herdr's idle status alone proves a drawn interface. This check observes startup; it does not grant workspace trust or establish an adapter for every kind.

The separate wait-loop setting `auto_approve` is not a workspace-trust policy. With it enabled, the loop may submit an unclassified blocked dialog using a default key. If a workspace or hook trust dialog reaches that path, the answer can grant and persist trust. Keep this setting off for fresh-directory startup until typed dialog handling is available; it must not be presented as support for trusting a newly created directory. This audit did not exercise that path for every native CLI.

Some current approval mappings also include project-resource decisions: Cursor edits/full adds `--trust`, and Claude full enables all project MCP servers. The proposed policy must address those couplings explicitly. Enabling Claude project MCP servers does not accept its separate workspace trust dialog.

## Agent matrix

This audit was performed on 2026-10-05. Installed help, source, binary strings, and sanitized configuration schemas were inspected. These observations do not establish interactive startup behavior for every agent or every release. No shared trust store was changed.

| Kind | Inspected version | Workspace trust mechanism | Current herdr-soho support and limits |
| --- | --- | --- | --- |
| Claude | 2.1.289 | A workspace trust dialog is separate from `--permission-mode`. The inspected client records acceptance under `projects.<directory>.hasTrustDialogAccepted` in `~/.claude.json`. | Tool approval mapping is present. A new disposable copy can still stop at the trust dialog. Do not use print mode as a replacement for the interactive worker. |
| Codex | 0.160.0 | `projects.<path>.trust_level` accepts `trusted` or `untrusted` in user configuration. Project configuration, hooks, and rules are gated by trust. Hook approval can also be a separate prompt. | Tool approval mapping is present. Workspace trust and hook trust are not equivalent to the sandbox or approval policy. The exact native prompts were not exercised in this audit. |
| Grok | 1.0.46 | A directory trust question and `~/.grok/trusted_folders.toml` were identified locally. | A dedicated startup guard recognizes the trust question and protects it from an ordinary task prompt. Do not infer a native trust flag from binary strings. |
| agy | 1.2.15 | A project trust dialog was identified in the installed binary. | The persistent store, inheritance rules, and a safe noninteractive grant remain unverified. Generic startup blocking is the available fallback. |
| Gemini | Not installed | Official documentation describes trusted folders and a session-specific `--skip-trust` option. | Local help and interactive behavior were not verified. The current approval arguments need a version-aware audit before adding an adapter. No default trust setting is asserted here. |
| Cursor | 2026.09.28-64d2043 | The inspected CLI exposes `--trust`; workspace trust metadata was identified at `.cursor/.workspace-trusted`. | The existing edits/full approval mapping includes `--trust`. Ask mode can still require a workspace decision. |
| Pi | Standalone 1.0.3 | Project resources can require trust. `--approve` grants trust for one run; `--no-approve` ignores those resources. `~/.pi/agent/trust.json` uses canonical paths and the nearest ancestor entry. | Decision inheritance and resource discovery are separate: once trusted, Pi collects ancestor `.agents/skills` up to the Git root, or the filesystem root when no Git boundary exists. A grant for an exact cwd alone does not confine that resource search. |
| OpenCode | 1.18.30 | No folder-trust dialog was identified in this inspection; the observed approval mechanism concerns tools and `--auto`. | This is not proof that every version or installation has no startup dialog. Unknown prompts remain unsupported rather than receiving an invented answer. |

The Codex configuration key is documented in the [official configuration reference](https://developers.openai.com/codex/config-reference/). Pi documents project-resource trust in its [security guide](https://pi.dev/docs/latest/security) and flags in its [CLI reference](https://pi.dev/docs/latest/cli). Gemini describes session grants in its [trusted-folders documentation](https://geminicli.com/docs/cli/trusted-folders/). Installed behavior takes precedence when a published reference describes a different release.

## Standalone executable resolution

After replacing a global package with a standalone installation, an existing shell can retain an old executable path or command cache. Check `command -v pi` and `pi --version` inside the pane that will launch the next worker. A check in the orchestrator's shell does not verify another pane.

Herdr 0.9.1 starts the canonical `pi` executable for kind `pi`; its installed `agent start` help does not offer an executable-path override. Prepare a pane that you own before starting the agent: put the standalone executable directory first in that pane's PATH and clear its shell command cache (`rehash` in zsh). `herdr pane split --env PATH=<prepared-path>` can prepare a new pane without editing global shell configuration. Existing workers should finish before their CLI is replaced. Do not rerun an installer merely to fix stale resolution.

## Proposed support

This section is a design proposal, not a configuration interface that has shipped.

1. Classify startup blocks as workspace trust, hook trust, authentication, update, or unknown, and expose the classification in structured CLI results. Keep the existing exit codes and the available pane for inspection.
2. Define workspace trust separately from tool approval. The default remains interactive. An optional allowlist matches only an explicitly named canonical launch directory, with no wildcard or automatic parent grant. Matching the cwd is not a resource-loading sandbox: each adapter must also verify the native project-resource search boundary.
3. Add an adapter only after validating the installed agent's native mechanism and effective resource scope. For Pi, require a verified Git resource boundary at the granted directory, or leave the adapter unsupported; a copy without that boundary must not receive a session grant that loads ancestor skills. Prefer a verified session grant only when its effective resource scope satisfies the policy. Do not seed vendor trust stores or submit an assumed default choice.
4. Keep unknown, authentication, update, and hook-trust dialogs outside generic auto-approval. Verify both the native working directory and any directory displayed in the dialog immediately before a trust action.
5. Move Cursor's workspace `--trust` grant out of edits/full into the explicit workspace policy, with a documented compatibility migration. Keep Claude's project MCP-server approval classified separately from workspace and hook trust. Implement policy and classification once in the shared native Go CLI rules; plugin entrypoints must delegate to them. Test fresh copies, symlinks, inherited native decisions, ancestor skill loading with and without a Git boundary, unrelated prompts, stale panes, and unsupported versions in disposable fixtures.
