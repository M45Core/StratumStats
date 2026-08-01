#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
primary_file="${1:-$repo_dir/../stratum-race/config/pools.json}"
census_file="${2:-$repo_dir/../PoolCensus/collector/pools.json}"
output_file="${3:-$repo_dir/config/pools.json}"
metadata_file="${4:-$repo_dir/config/pool-metadata.json}"
temporary_file="$(mktemp "${TMPDIR:-/tmp}/stratumstats-pools.XXXXXX")"
trap 'rm -f "$temporary_file"' EXIT

jq -n \
  --slurpfile primary "$primary_file" \
  --slurpfile census "$census_file" \
  --slurpfile metadata "$metadata_file" \
  -f "$repo_dir/scripts/merge-pools.jq" > "$temporary_file"

mv "$temporary_file" "$output_file"
printf 'Merged and enriched %s pools into %s\n' \
  "$(jq '.pools | length' "$output_file")" "$output_file"
