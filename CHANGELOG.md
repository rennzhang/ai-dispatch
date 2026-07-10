# Changelog

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
