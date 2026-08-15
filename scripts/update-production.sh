#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
binary="$repo_dir/.dist/stratumstats"

cd "$repo_dir"

echo "Building StratumStats..."
"$repo_dir/scripts/build-production.sh" "$binary"

echo "Installing and restarting StratumStats..."
sudo "$repo_dir/scripts/install-production.sh" --binary "$binary"

echo "Activating the repository pool registry..."
"$repo_dir/scripts/update-pools-production.sh" "$repo_dir/config/pools.json"

echo "Checking service health..."
curl --fail --silent --show-error --retry 5 --retry-delay 1 \
  --retry-connrefused \
  http://127.0.0.1:8081/healthz >/dev/null

echo "StratumStats is updated and healthy."
