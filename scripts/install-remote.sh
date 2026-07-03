#!/usr/bin/env bash
set -euo pipefail

# Remote installer for ai-dispatch skill.
# Downloads a precompiled tarball from GitHub Releases, verifies its SHA256
# checksum, and extracts it into ~/.claude/skills/ and/or ~/.codex/skills/.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh | bash
#
# Environment variables:
#   AI_DISPATCH_SKILL_TARGET  claude | codex | all  (default: all)
#   AI_DISPATCH_VERSION       release tag or "latest" (default: latest)

REPO="rennzhang/ai-dispatch"
VERSION="${AI_DISPATCH_VERSION:-latest}"
TARGET="${AI_DISPATCH_SKILL_TARGET:-all}"

# --- detect OS and ARCH -----------------------------------------------------

OS="$(uname -s | tr '[:upper:]' '[:lower:]')"
ARCH="$(uname -m)"

case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  aarch64|arm64) ARCH="arm64" ;;
  *) echo "ai-dispatch: unsupported architecture: $ARCH" >&2; exit 1 ;;
esac

case "$OS" in
  darwin|linux) ;;
  *) echo "ai-dispatch: unsupported OS: $OS" >&2; exit 1 ;;
esac

# --- download ---------------------------------------------------------------

TARBALL="ai-dispatch-${OS}-${ARCH}.tar.gz"
BASE_URL="https://github.com/${REPO}/releases"
if [ "$VERSION" = "latest" ]; then
  TARBALL_URL="${BASE_URL}/latest/download/${TARBALL}"
  CHECKSUMS_URL="${BASE_URL}/latest/download/SHA256SUMS"
else
  TARBALL_URL="${BASE_URL}/download/${VERSION}/${TARBALL}"
  CHECKSUMS_URL="${BASE_URL}/download/${VERSION}/SHA256SUMS"
fi

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT
installed_bins=()

sha256_file() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "ai-dispatch: no SHA256 tool found (need sha256sum or shasum)" >&2
    return 1
  fi
}

echo "==> Downloading ai-dispatch ${OS}/${ARCH} (${VERSION})..."
if ! curl -fsSL "$TARBALL_URL" -o "$tmpdir/$TARBALL"; then
  echo "ai-dispatch: failed to download $TARBALL_URL" >&2
  echo "Check your network connection or specify AI_DISPATCH_VERSION." >&2
  exit 1
fi

# --- verify checksum --------------------------------------------------------

echo "==> Verifying checksum..."
if ! curl -fsSL "$CHECKSUMS_URL" -o "$tmpdir/SHA256SUMS"; then
  echo "ai-dispatch: SHA256SUMS not found at $CHECKSUMS_URL" >&2
  exit 1
fi
expected="$(grep " ${TARBALL}\$" "$tmpdir/SHA256SUMS" | awk '{print $1}')"
if [ -z "$expected" ]; then
  echo "ai-dispatch: ${TARBALL} not found in SHA256SUMS" >&2
  exit 1
fi
actual="$(sha256_file "$tmpdir/$TARBALL")"
if [ "$expected" != "$actual" ]; then
  echo "ai-dispatch: checksum mismatch for ${TARBALL}" >&2
  echo "  expected: $expected" >&2
  echo "  actual:   $actual" >&2
  exit 1
fi
echo "    checksum OK"

# --- extract ----------------------------------------------------------------

tar -xzf "$tmpdir/$TARBALL" -C "$tmpdir"
src_dir="$tmpdir/ai-dispatch-${OS}-${ARCH}"
if [ ! -d "$src_dir" ]; then
  echo "ai-dispatch: unexpected tarball structure — $src_dir not found" >&2
  exit 1
fi

# --- install ----------------------------------------------------------------

install_to() {
  local skill_root="$1"
  local dest="$skill_root/ai-dispatch"
  local tmp_dest="$skill_root/.ai-dispatch.install.$$"
  local backup="$skill_root/.ai-dispatch.backup.$$"

  echo "==> Installing to $dest/"
  mkdir -p "$skill_root"
  rm -rf "$tmp_dest" "$backup"
  cp -R "$src_dir" "$tmp_dest"
  chmod +x "$tmp_dest/scripts/ai-dispatch" "$tmp_dest/scripts/ai-dispatch-go"
  if [ -d "$dest" ]; then
    mv "$dest" "$backup"
  fi
  if ! mv "$tmp_dest" "$dest"; then
    if [ -d "$backup" ]; then
      mv "$backup" "$dest"
    fi
    echo "ai-dispatch: failed to install to $dest; previous install preserved" >&2
    exit 1
  fi
  rm -rf "$backup"
  installed_bins+=("$dest/scripts/ai-dispatch")
}

case "$TARGET" in
  all)
    install_to "$HOME/.claude/skills"
    install_to "$HOME/.codex/skills"
    ;;
  claude)
    install_to "$HOME/.claude/skills"
    ;;
  codex)
    install_to "$HOME/.codex/skills"
    ;;
  *)
    echo "ai-dispatch: invalid AI_DISPATCH_SKILL_TARGET='$TARGET' (expected claude, codex, or all)" >&2
    exit 1
    ;;
esac

# --- verify -----------------------------------------------------------------

echo ""
echo "==> Verifying installation..."
verify_home="$tmpdir/verify-home"
for bin in "${installed_bins[@]}"; do
  AI_DISPATCH_HOME="$verify_home" "$bin" doctor --format json >/dev/null
done
echo "    verified ${#installed_bins[@]} skill install(s)"
echo ""

echo "Done. ai-dispatch installed successfully."
echo ""
echo "On first send/resume, ~/.ai-dispatch/ will be auto-initialized"
echo "with config and preferences. Run 'ai-dispatch providers scan' when needed."
