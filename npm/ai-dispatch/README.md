# ai-dispatch

The npm distribution for the `ai-dispatch` CLI.

```bash
npm install -g ai-dispatch
ai-dispatch doctor
```

For a one-off command:

```bash
npx --yes ai-dispatch doctor
```

At install time, this package downloads the matching `ai-dispatch` binary from
the GitHub Release for this package version and verifies its SHA-256 checksum
against that release's `SHA256SUMS`. It supports macOS and Linux on x64 and
arm64, and requires Node.js 18 or newer.

For provider configuration, skills, and source code, see the
[ai-dispatch repository](https://github.com/rennzhang/ai-dispatch).
