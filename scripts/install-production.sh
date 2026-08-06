#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
install_dir="/opt/stratumstats"
data_dir="/var/lib/stratumstats"
environment_file="/etc/stratumstats.env"
unit_file="/etc/systemd/system/stratumstats.service"
service_user="stratumstats"
binary=""
start_service=true

usage() {
  cat <<'EOF'
Usage: sudo ./scripts/install-production.sh [options]

Options:
  --binary PATH  Install an already-built StratumStats binary.
  --no-start     Install files but do not enable or start the service.
  -h, --help     Show this help.

Without --binary, the script builds a static binary from the current checkout.
Existing observations and /etc/stratumstats.env are always preserved.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --binary)
      [[ $# -ge 2 ]] || { echo "--binary requires a path" >&2; exit 2; }
      binary="$2"
      shift 2
      ;;
    --no-start)
      start_service=false
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

if [[ ${EUID:-$(id -u)} -ne 0 ]]; then
  echo "Run this installer as root, normally with sudo." >&2
  exit 1
fi
if ! command -v systemctl >/dev/null 2>&1; then
  echo "systemd is required." >&2
  exit 1
fi
if [[ ! -e "$environment_file" ]] && ! command -v openssl >/dev/null 2>&1; then
  echo "OpenSSL is required to generate the initial ingest secret." >&2
  exit 1
fi

temporary_dir="$(mktemp -d)"
trap 'rm -rf "$temporary_dir"' EXIT

if [[ -z "$binary" ]]; then
  if ! command -v go >/dev/null 2>&1; then
    echo "Go 1.22 or newer is required when --binary is not supplied." >&2
    exit 1
  fi
  binary="$temporary_dir/stratumstats"
  (
    cd "$repo_dir"
    CGO_ENABLED=0 go build -trimpath -o "$binary" .
  )
fi
if [[ ! -f "$binary" ]]; then
  echo "Binary not found: $binary" >&2
  exit 1
fi

if ! getent group "$service_user" >/dev/null; then
  groupadd --system "$service_user"
fi
if ! getent passwd "$service_user" >/dev/null; then
  nologin_shell="$(command -v nologin || true)"
  [[ -n "$nologin_shell" ]] || nologin_shell="/usr/sbin/nologin"
  useradd --system --gid "$service_user" --home-dir "$data_dir" --shell "$nologin_shell" "$service_user"
fi

install -d -o root -g root -m 0755 "$install_dir" "$install_dir/config"
install -d -o "$service_user" -g "$service_user" -m 0750 "$data_dir"
install -o root -g root -m 0755 "$binary" "$install_dir/stratumstats"
install -o root -g root -m 0644 "$repo_dir/config/pools.json" "$install_dir/config/pools.json"
install -o root -g root -m 0644 "$repo_dir/deploy/stratumstats.service" "$unit_file"

if [[ ! -e "$data_dir/observations.jsonl" ]]; then
  install -o "$service_user" -g "$service_user" -m 0640 /dev/null "$data_dir/observations.jsonl"
else
  chown "$service_user:$service_user" "$data_dir/observations.jsonl"
  chmod 0640 "$data_dir/observations.jsonl"
fi

created_environment=false
if [[ ! -e "$environment_file" ]]; then
  secret="$(openssl rand -hex 32)"
  umask 077
  {
    echo "STRATUMSTATS_INGEST_KEY_ID=regional-2026-01"
    echo "STRATUMSTATS_INGEST_SECRET=$secret"
  } >"$environment_file"
  created_environment=true
else
  echo "Preserving existing $environment_file"
fi
chown root:root "$environment_file"
chmod 0600 "$environment_file"

systemctl daemon-reload
if [[ "$start_service" == true ]]; then
  systemctl enable stratumstats
  systemctl restart stratumstats
fi

echo "Installed StratumStats in $install_dir"
echo "Persistent observations: $data_dir/observations.jsonl"
if [[ "$created_environment" == true ]]; then
  echo "Created $environment_file; copy its key ID and secret into Fly secrets."
fi
if [[ "$start_service" == true ]]; then
  echo "Check service health with: curl http://127.0.0.1:8080/healthz"
else
  echo "Service files installed without starting StratumStats."
fi
