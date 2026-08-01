#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
output_file="${1:-$repo_dir/docs/stratumstats-dashboard.png}"
screenshot_port="${STRATUMSTATS_SCREENSHOT_PORT:-18080}"
temporary_dir="$(mktemp -d "${TMPDIR:-/tmp}/stratumstats-screenshot.XXXXXX")"
server_pid=""

cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf "$temporary_dir"
}
trap cleanup EXIT INT TERM

browser=""
for candidate in google-chrome chromium-browser chromium; do
  if command -v "$candidate" >/dev/null 2>&1; then
    browser="$candidate"
    break
  fi
done
if [[ -z "$browser" ]]; then
  echo "screenshot requires Google Chrome or Chromium" >&2
  exit 1
fi

(cd "$repo_dir" && go build -o "$temporary_dir/stratumstats" ./cmd/stratumstats)
"$temporary_dir/stratumstats" demo -addr "127.0.0.1:$screenshot_port" \
  >"$temporary_dir/server.log" 2>&1 &
server_pid="$!"

ready=false
for _ in $(seq 1 50); do
  if curl --fail --silent "http://127.0.0.1:$screenshot_port/healthz" >/dev/null; then
    ready=true
    break
  fi
  if ! kill -0 "$server_pid" 2>/dev/null; then
    cat "$temporary_dir/server.log" >&2
    exit 1
  fi
  sleep 0.1
done
if [[ "$ready" != true ]]; then
  echo "demo server did not become ready" >&2
  cat "$temporary_dir/server.log" >&2
  exit 1
fi

mkdir -p "$(dirname "$output_file")"
"$browser" \
  --headless \
  --disable-gpu \
  --no-sandbox \
  --hide-scrollbars \
  --window-size=1440,1100 \
  --screenshot="$output_file" \
  "http://127.0.0.1:$screenshot_port/"

echo "Wrote $output_file"
