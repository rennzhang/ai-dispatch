#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

AI_DISPATCH_GO_PROVIDER_EXECUTION=off go test -race ./...
go vet ./...
scripts/go_active_caller_check.sh

shell_files=(
  bin/ai-dispatch
  scripts/install-remote.sh
  scripts/release.sh
  scripts/render-homebrew-formula.sh
  scripts/check-homebrew-formula.sh
  scripts/go_provider_acceptance.sh
  scripts/distribution_acceptance.sh
  scripts/go_active_caller_check.sh
  scripts/quality_gate.sh
  skills/ai-dispatch/scripts/ai-dispatch
)
while IFS= read -r shell_file; do
  shell_files+=("$shell_file")
done < <(git ls-files '*.sh')
bash -n "${shell_files[@]}"
ruby -e 'require "yaml"; ARGV.each { |file| YAML.load_file(file) }' \
  .github/workflows/ci.yml .github/workflows/release.yml

(
  cd npm/ai-dispatch
  npm test
  npm pack --dry-run
)

scripts/distribution_acceptance.sh
