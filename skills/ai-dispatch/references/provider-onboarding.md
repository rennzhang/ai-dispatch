# Provider And Model Onboarding

Prefer changing model routing data before adding code.

1. Add or edit a model entry in the registry.
2. Verify routing:

```bash
~/.ai-dispatch/bin/ai-dispatch models resolve <alias> --format json
```

3. Add provider code only when a new CLI, auth behavior, process protocol, output format, or session protocol is required.
4. Add provider tests before real provider smoke.
5. Run:

```bash
go test ./...
~/.ai-dispatch/bin/ai-dispatch doctor --format json
```

Keep provider adapters thin. ai-dispatch owns command construction, process timeout, result parsing, runstore, and route metadata; it should not become a general agent framework.
