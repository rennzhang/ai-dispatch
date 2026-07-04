# Contributing

Keep ai-dispatch small.

Preferred change order:

1. Change model registry data when routing data is enough.
2. Change provider command construction only when a CLI contract changed.
3. Add a provider adapter only for a new CLI, auth flow, process protocol, output format, or session protocol.

Run before sending changes:

```bash
AI_DISPATCH_GO_PROVIDER_EXECUTION=off go test ./...
go vet ./...
git diff --check
scripts/go_active_caller_check.sh
```

Real provider smoke is required for provider behavior changes.

Installer or release changes also need:

```bash
bash -n scripts/install-remote.sh scripts/release.sh skills/ai-dispatch/scripts/ai-dispatch
scripts/release.sh
```
