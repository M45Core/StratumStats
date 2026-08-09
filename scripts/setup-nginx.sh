#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
source_config="$repo_dir/deploy/nginx-stratumstats.conf"
limits_config="$repo_dir/deploy/nginx-stratumstats-limits.conf"
security_config="$repo_dir/deploy/nginx-stratumstats-security.conf"
available_config="/etc/nginx/sites-available/stratumstats.m45core.com"
enabled_config="/etc/nginx/sites-enabled/stratumstats.m45core.com"
installed_limits_config="/etc/nginx/conf.d/stratumstats-limits.conf"
installed_security_config="/etc/nginx/snippets/stratumstats-security.conf"
run_certbot=false
force=false

usage() {
  cat <<'EOF'
Usage: sudo ./scripts/setup-nginx.sh [options]

Options:
  --certbot  Request and install the HTTPS certificate after reloading Nginx.
  --force    Replace a differing existing site configuration.
  -h, --help Show this help.

The script refuses to replace a differing site by default. DNS must resolve to
this server before --certbot is used.
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --certbot)
      run_certbot=true
      shift
      ;;
    --force)
      force=true
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
  echo "Run this setup as root, normally with sudo." >&2
  exit 1
fi
if ! command -v nginx >/dev/null 2>&1; then
  echo "Nginx must be installed first." >&2
  exit 1
fi
if [[ ! -d /etc/nginx/sites-available || ! -d /etc/nginx/sites-enabled ]]; then
  echo "This script expects the Debian/Ubuntu Nginx sites-available layout." >&2
  exit 1
fi
if [[ ! -d /etc/nginx/conf.d || ! -d /etc/nginx/snippets ]]; then
  echo "This script expects the Debian/Ubuntu Nginx conf.d and snippets layout." >&2
  exit 1
fi

if [[ -e "$available_config" ]] && ! cmp -s "$source_config" "$available_config"; then
  if [[ "$force" != true ]]; then
    echo "$available_config already exists and differs; review it or rerun with --force." >&2
    exit 1
  fi
  backup="$available_config.backup.$(date -u +%Y%m%dT%H%M%SZ)"
  cp --preserve=mode,ownership,timestamps "$available_config" "$backup"
  echo "Backed up the previous site to $backup"
fi

install -o root -g root -m 0644 "$limits_config" "$installed_limits_config"
install -o root -g root -m 0644 "$security_config" "$installed_security_config"
install -o root -g root -m 0644 "$source_config" "$available_config"
if [[ -L "$enabled_config" ]]; then
  current_target="$(readlink -f "$enabled_config")"
  if [[ "$current_target" != "$available_config" ]]; then
    echo "$enabled_config points to an unexpected target: $current_target" >&2
    exit 1
  fi
elif [[ -e "$enabled_config" ]]; then
  echo "$enabled_config exists and is not a symbolic link; refusing to replace it." >&2
  exit 1
else
  ln -s "$available_config" "$enabled_config"
fi

nginx -t
systemctl reload nginx
echo "Enabled the HTTP reverse proxy for stratumstats.m45core.com"

if [[ "$run_certbot" == true ]]; then
  if ! command -v certbot >/dev/null 2>&1; then
    echo "Certbot and its Nginx plugin must be installed first." >&2
    exit 1
  fi
  certbot --nginx -d stratumstats.m45core.com
fi
