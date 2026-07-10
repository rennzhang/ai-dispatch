#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION_FILE="$ROOT/skills/ai-dispatch/VERSION"
VERSION="$(tr -d '[:space:]' < "$VERSION_FILE")"
EXPECTED_VERSION="${1:-$VERSION}"
OUTPUT="${2:-$ROOT/dist/ai-dispatch.rb}"
RELEASE_BASE_URL="${AI_DISPATCH_HOMEBREW_RELEASE_BASE_URL:-https://github.com/rennzhang/ai-dispatch/releases/download}"
CHECKSUMS="$ROOT/dist/SHA256SUMS"

if [ "$VERSION" != "$EXPECTED_VERSION" ]; then
  echo "ai-dispatch: VERSION mismatch: skill VERSION is $VERSION, expected $EXPECTED_VERSION" >&2
  exit 1
fi
if [ ! -f "$CHECKSUMS" ]; then
  echo "ai-dispatch: missing $CHECKSUMS; run scripts/release.sh first" >&2
  exit 1
fi

sha_for() {
  local filename="$1"
  local checksum
  checksum="$(awk -v filename="$filename" '$2 == filename { print $1 }' "$CHECKSUMS")"
  if [ -z "$checksum" ]; then
    echo "ai-dispatch: missing checksum for $filename" >&2
    exit 1
  fi
  printf '%s' "$checksum"
}

url_for() {
  printf '%s/%s/%s' "${RELEASE_BASE_URL%/}" "$VERSION" "$1"
}

mkdir -p "$(dirname "$OUTPUT")"
cat >"$OUTPUT" <<EOF
class AiDispatch < Formula
  desc "Dispatch tasks to local AI coding CLIs"
  homepage "https://github.com/rennzhang/ai-dispatch"
  version "${VERSION#v}"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "$(url_for ai-dispatch-darwin-arm64.tar.gz)"
      sha256 "$(sha_for ai-dispatch-darwin-arm64.tar.gz)"
    else
      url "$(url_for ai-dispatch-darwin-amd64.tar.gz)"
      sha256 "$(sha_for ai-dispatch-darwin-amd64.tar.gz)"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "$(url_for ai-dispatch-linux-arm64.tar.gz)"
      sha256 "$(sha_for ai-dispatch-linux-arm64.tar.gz)"
    else
      url "$(url_for ai-dispatch-linux-amd64.tar.gz)"
      sha256 "$(sha_for ai-dispatch-linux-amd64.tar.gz)"
    end
  end

  def install
    bin.install "scripts/ai-dispatch-go" => "ai-dispatch"
  end

  test do
    output = shell_output("#{bin}/ai-dispatch doctor --format json")
    assert_match '"ok":true', output
  end
end
EOF

echo "ai-dispatch: wrote Homebrew formula to $OUTPUT"
