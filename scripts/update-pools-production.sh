#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_file="${1:-$repo_dir/config/pools.json}"
service="stratumstats"
data_dir="/var/lib/stratumstats"
target="$data_dir/pools.json"
last_good="$data_dir/pools.last-good.json"
temporary="$data_dir/.pools.json.new"
binary="/opt/stratumstats/stratumstats"

if [[ ! -x "$binary" ]]; then
  echo "Installed StratumStats binary not found: $binary" >&2
  exit 1
fi
if [[ ! -f "$source_file" ]]; then
  echo "Pool registry not found: $source_file" >&2
  exit 1
fi
if [[ ! -f "$target" ]]; then
  echo "Installed pool registry not found: $target" >&2
  exit 1
fi

validation="$($binary validate-config -config "$source_file")"
expected_revision="${validation##*revision=}"
echo "$validation"

sudo install -o stratumstats -g stratumstats -m 0640 "$target" "$last_good"
sudo install -o stratumstats -g stratumstats -m 0640 "$source_file" "$temporary"
sudo mv "$temporary" "$target"
sudo systemctl reload "$service"

for attempt in {1..10}; do
  probe_config="$(curl --fail --silent --show-error http://127.0.0.1:8081/api/v1/probe-config || true)"
  if [[ "$probe_config" == *"$expected_revision"* ]]; then
    echo "Pool registry activated: revision=$expected_revision"
    exit 0
  fi
  sleep 1
done

echo "Service did not activate revision $expected_revision; restoring the last good registry" >&2
sudo install -o stratumstats -g stratumstats -m 0640 "$last_good" "$temporary"
sudo mv "$temporary" "$target"
sudo systemctl reload "$service"
exit 1
