#!/usr/bin/env bash
set -euo pipefail

# Remote installer for ai-dispatch CLI and optional skills.
# Downloads a precompiled tarball from GitHub Releases, verifies its SHA256
# checksum, installs a stable CLI entrypoint, and optionally extracts the skill
# into ~/.claude/skills/ and/or ~/.codex/skills/.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/rennzhang/ai-dispatch/main/scripts/install-remote.sh | bash
#
# Environment variables:
#   AI_DISPATCH_SKILL_TARGET  claude | codex | all | none  (default: all)
#   AI_DISPATCH_VERSION       release tag or "latest" (default: latest)
#   AI_DISPATCH_RELEASE_BASE_URL
#                             release root override for mirrors/testing
#   AI_DISPATCH_LINK_DIR      directory for ai-dispatch PATH symlink, or "none"
#                             (default: ~/.local/bin)

REPO="rennzhang/ai-dispatch"
VERSION="${AI_DISPATCH_VERSION:-latest}"
TARGET="${AI_DISPATCH_SKILL_TARGET:-all}"
LINK_DIR="${AI_DISPATCH_LINK_DIR:-$HOME/.local/bin}"

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
BASE_URL="${AI_DISPATCH_RELEASE_BASE_URL:-https://github.com/${REPO}/releases}"
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

curl_download() {
  curl -fsSL \
    --retry 3 \
    --retry-delay 1 \
    --retry-connrefused \
    "$1" -o "$2"
}

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
if ! curl_download "$TARBALL_URL" "$tmpdir/$TARBALL"; then
  echo "ai-dispatch: failed to download $TARBALL_URL" >&2
  echo "Check your network connection or specify AI_DISPATCH_VERSION." >&2
  exit 1
fi

# --- verify checksum --------------------------------------------------------

echo "==> Verifying checksum..."
if ! curl_download "$CHECKSUMS_URL" "$tmpdir/SHA256SUMS"; then
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
RELEASE_VERSION="$(tr -d '[:space:]' < "$src_dir/VERSION")"
if [ -z "$RELEASE_VERSION" ] || [ "$RELEASE_VERSION" = "dev" ]; then
  echo "ai-dispatch: invalid release VERSION in tarball: '$RELEASE_VERSION'" >&2
  exit 1
fi
binary_identity="$("$src_dir/scripts/ai-dispatch-go" version --format json 2>/dev/null || true)"
binary_version="$(printf '%s' "$binary_identity" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')"
binary_revision="$(printf '%s' "$binary_identity" | sed -n 's/.*"revision":"\([^"]*\)".*/\1/p')"
binary_modified="$(printf '%s' "$binary_identity" | sed -n 's/.*"modified":\([^,}]*\).*/\1/p')"
identity_ok=0
if [ "$binary_version" = "$RELEASE_VERSION" ] && [ "$binary_modified" = "false" ] && [ -n "$binary_revision" ]; then
  identity_ok=1
elif [ "${AI_DISPATCH_ALLOW_DIRTY_LOCAL:-0}" = "1" ] && \
  [ "$binary_version" = "$RELEASE_VERSION+dirty" ] && [ "$binary_modified" = "true" ] && [ -n "$binary_revision" ]; then
  identity_ok=1
elif [ "${AI_DISPATCH_ALLOW_DIRTY_LOCAL:-0}" = "1" ] && \
  [[ "$binary_version" = "$RELEASE_VERSION+dev."* ]] && [ "$binary_modified" = "false" ] && [ -n "$binary_revision" ]; then
  identity_ok=1
fi
if [ "$identity_ok" != "1" ]; then
  echo "ai-dispatch: binary identity does not match tarball VERSION $RELEASE_VERSION" >&2
  exit 1
fi

# --- install ----------------------------------------------------------------

install_cli() {
  local home_dir="${AI_DISPATCH_HOME:-$HOME/.ai-dispatch}"
  local bin_dir="$home_dir/bin"
  local versioned_bin="$bin_dir/ai-dispatch-go-${RELEASE_VERSION}-${OS}-${ARCH}"
  local wrapper="$bin_dir/ai-dispatch"
  local tmp_bin="$versioned_bin.tmp.$$"
  local tmp_wrapper="$wrapper.tmp.$$"

  echo "==> Installing CLI to $wrapper"
  mkdir -p "$bin_dir"
  cp "$src_dir/scripts/ai-dispatch-go" "$tmp_bin"
  chmod +x "$tmp_bin"
  mv "$tmp_bin" "$versioned_bin"
  cat > "$tmp_wrapper" <<EOF
#!/usr/bin/env bash
set -euo pipefail

SOURCE="\${BASH_SOURCE[0]}"
while [ -L "\$SOURCE" ]; do
  DIR="\$(cd -P "\$(dirname "\$SOURCE")" && pwd)"
  SOURCE="\$(readlink "\$SOURCE")"
  [[ \$SOURCE != /* ]] && SOURCE="\$DIR/\$SOURCE"
done
HERE="\$(cd -P "\$(dirname "\$SOURCE")" && pwd)"
exec "\$HERE/ai-dispatch-go-${RELEASE_VERSION}-${OS}-${ARCH}" "\$@"
EOF
  chmod +x "$tmp_wrapper"
  mv "$tmp_wrapper" "$wrapper"
  installed_bins+=("$wrapper")

  if [ "$LINK_DIR" != "none" ]; then
    mkdir -p "$LINK_DIR"
    ln -sfn "$wrapper" "$LINK_DIR/ai-dispatch"
    echo "    PATH link: $LINK_DIR/ai-dispatch"
  fi
}

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

install_cli

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
  none)
    ;;
  *)
    echo "ai-dispatch: invalid AI_DISPATCH_SKILL_TARGET='$TARGET' (expected claude, codex, all, or none)" >&2
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
echo "    verified ${#installed_bins[@]} install entrypoint(s)"
echo ""

echo "Done. ai-dispatch installed successfully."
echo ""
echo "CLI: ${AI_DISPATCH_HOME:-$HOME/.ai-dispatch}/bin/ai-dispatch"
if [ "$LINK_DIR" != "none" ]; then
  echo "PATH link: $LINK_DIR/ai-dispatch"
fi
echo ""
echo "On first send/resume, ~/.ai-dispatch/ will be auto-initialized"
echo "with config and preferences. Run 'ai-dispatch providers scan' when needed."
