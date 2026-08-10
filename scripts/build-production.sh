#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output="${1:-$repo_dir/.dist/stratumstats}"

if ! command -v go >/dev/null 2>&1; then
  echo "Go 1.25.12 or newer is required." >&2
  exit 1
fi

mkdir -p "$(dirname "$output")"
(
  cd "$repo_dir"
  CGO_ENABLED=0 go build -trimpath -o "$output" .
)

echo "Built $output"
