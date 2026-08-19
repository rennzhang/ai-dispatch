# Changelog

## v0.6.2

- Reject legacy-looking `provider:model` and `model:provider` colon inputs instead of inferring them as raw model ids; use `send <provider> --model <model>`.

## v0.6.1

- Continue ordered `config.models` fallback after config, quota, network, timeout, runtime, and unknown provider failures; cancellation, resume, input, and interactive permission failures still stop immediately.
- Raise the default wall-clock safety limit from 1800 seconds to 7200 seconds (120 minutes) for every provider; `--timeout 0` still disables it.
- Switch Grok and Cursor from completion-only JSON to incremental NDJSON so shared provider-activity timeouts reset while either CLI is still producing events.
- Parse Grok text/end events into the final result and expose safe Grok/Cursor progress without echoing user prompts or hidden reasoning.

## v0.6.0

- Add native Cursor provider (`cursor-agent --print --output-format json`) with subscription model routing: `cursor-fable5`, `cursor-opus5`, `cursor-sonnet5`, `cursor-composer`, plus Fable/Opus/Sonnet aliases and `cursor` bare target defaulting to Composer.
- Add `cursor.approval=always|default` provider option; default trusts the workspace without auto-approving tool/file operations.
- Preserve Cursor JSON error body as provider stderr; classify Node network errors (`ENOTFOUND`, `ECONNREFUSED`, `ETIMEDOUT`, `fetch failed`, `network error`) as `network` so candidate fallback works.
- Sanitize prompt-file and workspace paths from CLI errors, Cursor stderr, JSON error text, and run errors; keep `CURSOR_API_KEY` through the sanitized provider environment.
- Add routing, diagnostics, probe, CLI whitelist, env, and provider unit tests for the Cursor adapter.

## v0.5.0

- Add GPT-5.6 Luna as a built-in Codex target with exact `low|medium|high|xhigh|max` effort support.
- Add cross-provider `--fast`. Codex GPT-5.5/5.6 targets apply the priority service tier; unsupported providers continue once at standard speed and expose `requested_fast`, `applied_fast`, and `fast_fallback_reason` without setting `degraded`.

## v0.4.0

- Add cross-provider top-level `--effort` (`auto|none|minimal|low|medium|high|xhigh|max`). Omitted/`auto` preserves each CLI's default; unsupported explicit levels fall back to `auto` without adjacent-level mapping or provider re-runs.
- Surface `requested_effort`, `applied_effort`, and `effort_fallback_reason` on results and route steps. Effort fallback does not set `degraded`.
- Codex no longer hardcodes `model_reasoning_effort=high` when effort is omitted; CLI/model defaults apply.
- Expose only GPT-5.6 Sol and Terra as built-in/local Codex choices; allow `low|medium|high|xhigh|max`, while `none`, `minimal`, Luna, and unverified models fall back to `auto`.
- Claude Code does not accept `--effort` today: explicit Claude effort falls back to `auto` with a clear reason, and Build never adds the flag (print or PTY).
- Remove `grok.effort` provider option in favor of `--effort` (clear migration error).
- Antigravity only sends `--model` when a model override is resolved; empty model uses the agy default.
- Disable the inactivity timeout by default while retaining the 1800-second wall-clock safety limit; `--activity-timeout` remains available as an explicit opt-in.
- Make dispatch cancellation and timeout process-tree aware, preserve provider diagnostics, bound captured output, and surface incomplete cleanup or truncation as warnings.
- Preserve real elapsed time for provider lock waits and execution-preparation timeouts instead of recording zero-duration failures.
- Persist send/resume results atomically, add safer run-history filtering and failure summaries, and skip corrupt records with explicit diagnostics.
- Add bounded, deduplicated progress parsing with terminal done/error semantics that provider output cannot forge.
- Add machine-readable `version --format json` identity and make installed wrappers reject binaries that do not match their packaged version.
- Forward interrupt/termination signals through the npm wrapper and strengthen distribution acceptance across direct, skill, npm, and Homebrew entrypoints.
- Refuse dirty release builds by default; dirty artifacts require the explicit local-only `AI_DISPATCH_ALLOW_DIRTY_LOCAL=1` override and must not be published.

## v0.3.0

- Add the `ai-dispatch` npm package, so the CLI can be installed with npm or run once through npx.
- Add a generated Homebrew formula for the `rennzhang/tap` tap.
- Keep GitHub Release binaries as the only executable source: the npm installer downloads the matching platform binary and verifies it against release SHA-256 checksums.
- Add npm package tests and release-version checks; release assets now include standalone darwin/linux amd64/arm64 binaries for the npm installer.

## v0.2.0

- Promote the Grok provider release to a minor version because it adds a first-class provider and reusable provider acceptance gate.
- Keep the `send grok` route as native Grok Build first, with OpenCode/OpenRouter fallback support.
- Keep the `grok-fast` native Grok Composer fast target.
- Document the same-name `config.json models` routing behavior, Grok approval behavior, and the `grok-build-0.1` migration path.

## v0.1.7

- Superseded by `v0.2.0`; kept as a published patch-tag release for continuity.
- Add native Grok Build CLI support through the `grok` provider.
- Route `send grok` through the configured native Grok candidate first, with OpenCode/OpenRouter fallback support.
- Add `grok-fast` for the native Grok Composer fast model.
- Add reusable provider acceptance documentation and harness scripts for provider onboarding and regression.
- Document Grok provider options, approval behavior, and the `grok-build-0.1` target migration path.

## v0.1.6

- Previous public release.
