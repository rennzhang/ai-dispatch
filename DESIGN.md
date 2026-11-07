# ai-dispatch Architecture

`ai-dispatch` is a local provider CLI runtime. One request resolves one execution path and returns one structured result. It is not an agent framework, provider installer, or automatic fallback scheduler.

## Public entry

```bash
ai-dispatch send --prompt-file /tmp/task.md --json-result
ai-dispatch send sol "prompt" --json-result
ai-dispatch send codex "prompt" --surface app --model sol --json-result
ai-dispatch send grok "prompt" --model grok-4.7 --json-result
ai-dispatch resume --session-id <id> "delta" --json-result
```

Defaults are OMP / CLI / `gpt-6.1-sol`. The configured model must have a discovered or manual OMP/CLI target before implicit default dispatch. Missing defaults fail with an actionable scan/config error; no model ID is guessed.

The stable installed entry and the Agent-facing skill wrapper execute the same CLI contract. A development build does not alter an installed release unless explicitly installed. Build identity comes from `version --format json`, not a filename alone.

## Package boundaries

```text
cmd/ai-dispatch/                 # binary main
internal/cli/                    # parsing, config migration, discovery commands
internal/config/                 # strict v2 schema, validation, atomic save
internal/providerinfo/           # supported names and shared executable lookup
internal/setup/                  # executable/catalog discovery and merge
internal/routing/                # one provider/surface/native-model resolution
internal/dispatch/               # one request, capabilities, execution/result
internal/contract/               # request/result/progress contract
internal/runtime/                # subprocess, timeouts, owned process cleanup
internal/providers/<provider>/   # native provider protocol adapters
internal/providers/codex/app*    # Desktop discovery, preparation, delivery, receipt
internal/output/                 # stdout, JSON, files, user-facing summary
internal/runstore/               # persisted invocation and session recovery
```

## Configuration and ownership

One executable configuration file, `~/.ai-dispatch/config.json`, contains:

- `defaults`: default provider, surface and logical model.
- `providers`: discovered absolute executables and supported surfaces.
- `models`: logical names and aliases, with many-to-many `targets.<provider>.<surface>` records containing native model IDs and per-entry capabilities.

A model is not assigned one mandatory provider. Target records describe known support, not candidate order or retry policy. Magpie is one possible OMP source, not a global dependency. CLI versions are diagnostic data and are not persisted; the top-level schema `version` remains for migration.

Each target has `origin: scan|manual`. A successful catalog refresh replaces only scanned targets of that provider/surface. Failed or unsupported discovery preserves prior model entries and reports the limitation. Manual targets, aliases and defaults survive scans. All config writers serialize reload/merge/save and atomically replace the file; no second catalog or dual-reading compatibility layer exists.

v1 migration is explicit and previewable. Applying preserves the original file. Multiple legacy candidates for the same provider/surface with different native IDs are conflicts; ordering cannot silently choose a winner. Legacy provider-name model aliases are renamed so they cannot shadow explicit execution entries.

`preferences.md` remains optional human-readable Agent guidance. It does not execute routing, and cannot override explicit user choices.

## Request flow

```text
CLI input
  -> ensure minimum state (no implicit provider/model scan)
  -> load one config snapshot
  -> resolve explicit provider or defaults
  -> resolve surface and per-entry native model ID
  -> resolve effort/fast from that selected target
  -> execute one native adapter
  -> classify result and preserve actual session/surface
  -> output + runstore
```

Explicit provider names always mean that provider. An unknown native ID with explicit `provider --model` passes through as unlisted; a logical alias lacking a target does not become a guessed native ID. Model-only selection uses the default provider and does not switch to an arbitrary available target.

Resume uses stored session provider/surface/model. An explicit conflicting provider or surface is rejected. Unknown sessions require an explicit enough destination instead of falling back to current defaults.

Capability fallback (effort auto or standard speed) is reported independently; it never starts another model request. Provider failures remain failures. Result fields retained for historical compatibility do not reintroduce candidate iteration.

## Discovery flow

```text
providers scan [--dry-run]
  -> supported adapter registry
  -> shared executable lookup
  -> each entry's native model-list interface
  -> validate/normalize native IDs and capability intersection
  -> merge scanned targets without replacing manual state
  -> preview report or atomic save of the same config.json
```

Absent providers appear in the report, not as fabricated unavailable config entries. Existing configured paths that fail are reported. Discovery does not install, authorize, change another CLI's settings, or invoke model inference. CLI caches may be read/refreshed through their own documented catalog interfaces.

Codex CLI and Codex App catalogs come from their respective binaries. App discovery locates the registered installation independently of running GUI instances and starts only its bundled temporary backend for `model/list`, then closes it; it does not launch/focus the GUI, create threads, or change presets. A catalog does not certify GUI selection or account quota.

## Codex App execution

1. An explicit `AI_DISPATCH_CODEX_APP_PID` selects a target. Default routing excludes custom `--user-data-dir` profiles, then resolves a matching ancestor, a unique active instance or a unique instance with GUI windows. Reject remaining ambiguity. Delivery may start the registered installation when none is running. URL routes use PID-addressed GURL AppleEvents, not bundle-addressed `open -a`.
2. For a new thread, create/name metadata through the bundled temporary backend and close it before GUI execution.
3. Acquire the instance preparation lock. Open the exact named thread, access windows and controls through native PID-bound AXUIElement handles, select/verify its GUI model/effort/speed through native menu actions and PID-targeted single-step strength keys, checking each reported level within the menu-provided bound, and persist input through a cold queue-only backend. Check that backend owns no loaded threads before/after queueing, close it, then wake the GUI.
4. Release the instance preparation lock before waiting. Keep the per-session lock so another dispatch cannot concurrently submit to the same session.
5. Match the unique dispatch receipt to its thread/turn. Require a successful terminal event and matching `turn_context`; old completions, partial output and queued acknowledgements are not success.

The body retains `[ai-dispatch]` as its first line and includes a unique `dispatch-id` for App correlation. An unconfirmed submission retains a UUID-only pending record across cancellation/restart so retry cannot blindly enqueue again. Active or unconfirmed sessions require checking the original thread.

Cancellation ends dispatch waiting and owned temporary processes; it does not claim to interrupt a GUI-owned model turn. Preserve the session and explicit warning. No independent daemon or CLI inference fallback may substitute for the requested App channel.

## State

```text
~/.ai-dispatch/
  config.json
  preferences.md
  bin/
  runs/
  logs/
  desktop-locks/   # coordination and UUID-only pending delivery state
```

Migration backups and lock files are recovery/coordination artifacts, not additional active configs. `CODEX_HOME` remains the caller's selected Desktop state; delivery never invents an isolated profile.

## Validation

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go test ./...
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go vet ./...
git diff --check
scripts/go_active_caller_check.sh
```

Real provider acceptance separately verifies selected entry/model, response, session recovery, cancellation and task-specific effects. Unit tests, scan success and a zero process exit do not establish real-chain acceptance. Release/build/install checks do not publish changes without explicit authorization.
