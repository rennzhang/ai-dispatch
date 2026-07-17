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
RELEASE_REVISION="${GITHUB_SHA:-$(git -C "$ROOT" rev-parse HEAD)}"

worktree_dirty=0
if ! git -C "$ROOT" diff --quiet --ignore-submodules -- || \
  ! git -C "$ROOT" diff --cached --quiet --ignore-submodules -- || \
  [ -n "$(git -C "$ROOT" ls-files --others --exclude-standard)" ]; then
  worktree_dirty=1
fi
if [ "$worktree_dirty" = "1" ]; then
  if [ "${AI_DISPATCH_ALLOW_DIRTY_LOCAL:-0}" != "1" ]; then
    echo "ai-dispatch: refusing to build release packages from a dirty worktree" >&2
    echo "Commit the complete release, or set AI_DISPATCH_ALLOW_DIRTY_LOCAL=1 for explicit local acceptance only." >&2
    exit 1
  fi
  echo "ai-dispatch: WARNING: building dirty local acceptance artifacts; do not publish them" >&2
fi

assert_release_identity() {
  local binary="$1"
  local metadata binary_module binary_revision binary_modified
  metadata="$(go version -m "$binary")"
  binary_module="$(printf '%s\n' "$metadata" | awk '$1 == "mod" { print $2; exit }')"
  binary_revision="$(printf '%s\n' "$metadata" | awk '$1 == "build" && $2 ~ /^vcs.revision=/ { sub(/^vcs.revision=/, "", $2); print $2; exit }')"
  binary_modified="$(printf '%s\n' "$metadata" | awk '$1 == "build" && $2 ~ /^vcs.modified=/ { sub(/^vcs.modified=/, "", $2); print $2; exit }')"
  if [ "$binary_module" != "github.com/rennzhang/ai-dispatch" ] || \
    [ "$binary_revision" != "$RELEASE_REVISION" ] || [ "$binary_modified" != "false" ]; then
    echo "ai-dispatch: release binary identity mismatch: $binary" >&2
    echo "  module=$binary_module revision=$binary_revision modified=$binary_modified" >&2
    exit 1
  fi
}
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
    release_identity=""
    if [ -n "$EXPECTED_VERSION" ]; then
      release_identity=" -X github.com/rennzhang/ai-dispatch/internal/buildinfo.releaseIdentity=true"
    fi
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -trimpath \
      -ldflags="-buildid= -X github.com/rennzhang/ai-dispatch/internal/buildinfo.versionOverride=$VERSION$release_identity" \
      -o "$bin" ./cmd/ai-dispatch
  )
  chmod +x "$bin"
  if [ -n "$EXPECTED_VERSION" ]; then
    assert_release_identity "$bin"
  fi

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

HOST_OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
HOST_ARCH="$(uname -m)"
case "$HOST_ARCH" in
  x86_64|amd64) HOST_ARCH=amd64 ;;
  aarch64|arm64) HOST_ARCH=arm64 ;;
  *) echo "ai-dispatch: unsupported host architecture for acceptance: $HOST_ARCH" >&2; exit 1 ;;
esac
case "$HOST_OS" in
  darwin|linux) ;;
  *) echo "ai-dispatch: unsupported host OS for acceptance: $HOST_OS" >&2; exit 1 ;;
esac
echo ""
echo "==> Verifying host release artifact"
if [ -n "$EXPECTED_VERSION" ]; then
  AI_DISPATCH_REQUIRE_RELEASE_IDENTITY=1 \
    AI_DISPATCH_CANDIDATE_BIN="$DIST/ai-dispatch-$HOST_OS-$HOST_ARCH/scripts/ai-dispatch-go" \
    AI_DISPATCH_PACKAGE_TARBALL="$DIST/ai-dispatch-$HOST_OS-$HOST_ARCH.tar.gz" \
    scripts/distribution_acceptance.sh
else
  AI_DISPATCH_CANDIDATE_BIN="$DIST/ai-dispatch-$HOST_OS-$HOST_ARCH/scripts/ai-dispatch-go" \
    AI_DISPATCH_PACKAGE_TARBALL="$DIST/ai-dispatch-$HOST_OS-$HOST_ARCH.tar.gz" \
    scripts/distribution_acceptance.sh
fi

ls -lh "$DIST"/*.tar.gz "$DIST"/*.bin
echo ""
echo "Release packages built for $VERSION."
echo ""
echo "Checksums: $DIST/SHA256SUMS"
