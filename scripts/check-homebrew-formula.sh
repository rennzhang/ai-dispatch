#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FORMULA="$ROOT/dist/ai-dispatch.rb"
VERSION="$(tr -d '[:space:]' < "$ROOT/skills/ai-dispatch/VERSION")"
CHECKSUMS="$ROOT/dist/SHA256SUMS"

if [ ! -f "$FORMULA" ]; then
  echo "ai-dispatch: missing $FORMULA; run scripts/release.sh first" >&2
  exit 1
fi
ruby -c "$FORMULA" >/dev/null
grep -Fq "version \"${VERSION#v}\"" "$FORMULA"

for asset in \
  ai-dispatch-darwin-amd64.tar.gz \
  ai-dispatch-darwin-arm64.tar.gz \
  ai-dispatch-linux-amd64.tar.gz \
  ai-dispatch-linux-arm64.tar.gz; do
  checksum="$(awk -v filename="$asset" '$2 == filename { print $1 }' "$CHECKSUMS")"
  test -n "$checksum"
  grep -Fq "$asset" "$FORMULA"
  grep -Fq "sha256 \"$checksum\"" "$FORMULA"
done

echo "ai-dispatch: Homebrew formula matches release $VERSION"
