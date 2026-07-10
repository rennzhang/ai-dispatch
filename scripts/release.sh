#!/usr/bin/env bash
set -euo pipefail

# Cross-compile ai-dispatch for darwin/linux × amd64/arm64 and package
# each platform as a self-contained tarball that users can extract directly
# into ~/.claude/skills/ or ~/.codex/skills/.

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST="$ROOT/dist"
SRC_SKILL="$ROOT/skills/ai-dispatch"
VERSION_FILE="$SRC_SKILL/VERSION"
NPM_PACKAGE="$ROOT/npm/ai-dispatch"

if [ "${2:-}" != "" ]; then
  echo "Usage: scripts/release.sh [expected-version-tag]" >&2
  exit 2
fi
if [ ! -f "$VERSION_FILE" ]; then
  echo "ai-dispatch: missing $VERSION_FILE" >&2
  exit 1
fi
VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
if [ -z "$VERSION" ] || [ "$VERSION" = "dev" ]; then
  echo "ai-dispatch: release VERSION must be a tag, got '$VERSION'" >&2
  exit 1
fi
EXPECTED_VERSION="${1:-}"
if [ -n "$EXPECTED_VERSION" ] && [ "$VERSION" != "$EXPECTED_VERSION" ]; then
  echo "ai-dispatch: VERSION mismatch: skill VERSION is $VERSION, release tag is $EXPECTED_VERSION" >&2
  exit 1
fi
if ! command -v node >/dev/null 2>&1; then
  echo "ai-dispatch: node is required to verify the npm package version" >&2
  exit 1
fi
node "$NPM_PACKAGE/scripts/verify-package.js"

PLATFORMS=(
  "darwin amd64"
  "darwin arm64"
  "linux amd64"
  "linux arm64"
)

rm -rf "$DIST"
mkdir -p "$DIST"

sha256_files() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$@"
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$@"
  else
    echo "ai-dispatch: no SHA256 tool found (need sha256sum or shasum)" >&2
    return 1
  fi
}

for plat in "${PLATFORMS[@]}"; do
  set -- $plat
  GOOS="$1"
  GOARCH="$2"
  pkgname="ai-dispatch-${GOOS}-${GOARCH}"
  pkgdir="$DIST/$pkgname"
  bin="$pkgdir/scripts/ai-dispatch-go"

  echo "==> Building $pkgname"

  mkdir -p "$pkgdir/scripts" "$pkgdir/references" "$pkgdir/agents"

  # Copy skill assets (SKILL.md, references, agents, wrapper)
  cp "$SRC_SKILL/SKILL.md" "$pkgdir/"
  cp "$SRC_SKILL/VERSION" "$pkgdir/"
  cp "$ROOT/LICENSE" "$pkgdir/"
  cp "$SRC_SKILL/references/"*.md "$pkgdir/references/"
  cp "$SRC_SKILL/agents/"*.yaml "$pkgdir/agents/"
  cp "$SRC_SKILL/scripts/ai-dispatch" "$pkgdir/scripts/"
  chmod +x "$pkgdir/scripts/ai-dispatch"

  # Cross-compile the Go binary
  (
    cd "$ROOT"
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath -ldflags="-buildid=" -o "$bin" ./cmd/ai-dispatch
  )
  chmod +x "$bin"

  # Keep a checksum-verified standalone binary for the npm installer.
  cp "$bin" "$DIST/$pkgname.bin"
  chmod +x "$DIST/$pkgname.bin"

  # Create tarball
  tar -C "$DIST" -czf "$DIST/$pkgname.tar.gz" "$pkgname"
  echo "    -> $DIST/$pkgname.tar.gz"
done

echo ""
echo "==> Generating SHA256SUMS"
cd "$DIST"
sha256_files *.tar.gz *.bin > SHA256SUMS
cd "$ROOT"
scripts/render-homebrew-formula.sh "$VERSION" "$DIST/ai-dispatch.rb"
ls -lh "$DIST"/*.tar.gz "$DIST"/*.bin
echo ""
echo "Release packages built for $VERSION."
echo ""
echo "Checksums: $DIST/SHA256SUMS"
