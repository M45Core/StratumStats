#!/usr/bin/env bash
set -euo pipefail

umask 077

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
node_home="${STRATUMSTATS_BITCOIN_HOME:-$repo_dir/.bitcoin-node}"
if [[ "$node_home" != /* ]]; then
  node_home="$repo_dir/$node_home"
fi
core_version="31.1"
release_dir="$node_home/releases/bitcoin-$core_version"
bitcoin_bin="$release_dir/bin/bitcoind"
bitcoin_cli="$release_dir/bin/bitcoin-cli"
default_data_dir="$node_home/data"
data_state="$node_home/data-state"
manager_lock="$node_home/manager.lock"
managed_marker=".stratumstats-bitcoin-node"
config_template="$repo_dir/config/bitcoin.conf"
checksum_manifest="$repo_dir/scripts/bitcoin-core-checksums.txt"
release_base_url="https://bitcoincore.org/bin/bitcoin-core-$core_version"
install_work=""
selected_data_dir=""
selected_data_token=""
stored_data_dir=""
stored_data_token=""
managed_pid=""
data_override=""
lock_held=false

cleanup() {
  if [[ -n "$install_work" && -d "$install_work" ]]; then
    rm -rf -- "$install_work"
  fi
  if [[ "$lock_held" == true && -d "$manager_lock" ]]; then
    rmdir "$manager_lock" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

die() {
  echo "bitcoin-node: $*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: ./scripts/bitcoin-node.sh COMMAND [options]

Commands:
  install [--data-dir PATH]  Install verified Bitcoin Core locally
  start [--data-dir PATH]    Start the managed node and remember PATH
  fast-sync SNAPSHOT         Load an AssumeUTXO snapshot into a running node
  stop                       Request a graceful shutdown and wait
  status                     Show active and background sync status
  logs [--follow]            Show the last 100 log lines
  cli ARG...                 Run the managed bitcoin-cli
  paths                      Show the managed binary and data paths
  help                       Show this help

The default install and data directories are both under .bitcoin-node in this
workspace. An external --data-dir must already exist and be empty (or already
managed by this script). Remembered paths are never recreated automatically.

fast-sync accepts a local snapshot file. Bitcoin Core validates its UTXO-set
hash against the commitments compiled into this exact Core release.
EOF
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

acquire_manager_lock() {
  mkdir -p "$node_home"
  if ! mkdir "$manager_lock" 2>/dev/null; then
    die "another node-manager command is active (if not, remove stale lock $manager_lock)"
  fi
  lock_held=true
}

parse_data_dir_args() {
  data_override=""
  while [[ $# -gt 0 ]]; do
    case "$1" in
      --data-dir|-d)
        [[ $# -ge 2 ]] || die "$1 requires a path"
        data_override="$2"
        shift 2
        ;;
      --data-dir=*)
        data_override="${1#*=}"
        shift
        ;;
      *)
        die "unknown option: $1"
        ;;
    esac
  done
}

canonicalize_existing_data_dir() {
  local requested="$1"

  [[ -n "$requested" ]] || die "data directory must not be empty"
  [[ "$requested" != *$'\n'* ]] || die "data directory must not contain a newline"
  if [[ "$requested" != /* ]]; then
    requested="$PWD/$requested"
  fi
  [[ -d "$requested" ]] || die "data directory must already exist: $requested (is the drive mounted?)"
  [[ -w "$requested" ]] || die "data directory is not writable: $requested"
  [[ -O "$requested" ]] || die "data directory must be owned by the current user: $requested"
  selected_data_dir="$(cd "$requested" && pwd -P)"
}

preflight_data_override() {
  local first_entry=""

  [[ -n "$data_override" ]] || return 0
  canonicalize_existing_data_dir "$data_override"
  data_override="$selected_data_dir"
  if [[ ! -f "$selected_data_dir/$managed_marker" ]]; then
    first_entry="$(find "$selected_data_dir" -mindepth 1 -maxdepth 1 -print -quit)"
    [[ -z "$first_entry" ]] || die "refusing non-empty unmanaged data directory: $selected_data_dir"
  fi
}

read_data_state() {
  local extra=""

  [[ -f "$data_state" ]] || die "no managed data directory; run install first"
  stored_data_dir=""
  stored_data_token=""
  {
    IFS= read -r stored_data_dir || die "could not read data path from $data_state"
    IFS= read -r stored_data_token || die "could not read marker token from $data_state"
    if IFS= read -r extra; then
      die "invalid extra data in $data_state"
    fi
  } <"$data_state"
  [[ -n "$stored_data_dir" && -n "$stored_data_token" ]] || die "invalid managed data state"
}

validate_selected_data_dir() {
  local marker_token=""

  [[ -d "$selected_data_dir" ]] || die "stored data directory is unavailable: $selected_data_dir (is the drive mounted?)"
  [[ -f "$selected_data_dir/$managed_marker" ]] || die "managed marker is missing at $selected_data_dir (is the drive mounted?)"
  IFS= read -r marker_token <"$selected_data_dir/$managed_marker" || die "could not read managed marker"
  [[ "$marker_token" == "$selected_data_token" ]] || die "managed marker does not match the remembered drive at $selected_data_dir"
  [[ -f "$selected_data_dir/bitcoin.conf" ]] || die "missing managed config: $selected_data_dir/bitcoin.conf"
}

select_existing_data_dir() {
  read_data_state
  selected_data_dir="$stored_data_dir"
  selected_data_token="$stored_data_token"
  validate_selected_data_dir
}

directory_mode() {
  case "$(uname -s)" in
    Linux) stat -c '%a' "$1" ;;
    Darwin) stat -f '%Lp' "$1" ;;
    *) return 1 ;;
  esac
}

warn_about_storage() {
  local mode=""
  local available_kib=""
  local recommended_kib=$((20 * 1024 * 1024))

  chmod 700 "$selected_data_dir" 2>/dev/null || true
  mode="$(directory_mode "$selected_data_dir" 2>/dev/null || true)"
  if [[ -n "$mode" && "$mode" != "700" ]]; then
    echo "Warning: $selected_data_dir has mode $mode; a private 0700 directory is recommended." >&2
  fi
  available_kib="$(df -Pk "$selected_data_dir" 2>/dev/null | awk 'NR == 2 {print $4}')"
  if [[ "$available_kib" =~ ^[0-9]+$ && "$available_kib" -lt "$recommended_kib" ]]; then
    echo "Warning: less than 20 GiB is free in $selected_data_dir; even a pruned node may run out of space." >&2
  fi
}

initialize_new_data_dir() {
  local first_entry=""
  local config_tmp

  if [[ -f "$selected_data_dir/$managed_marker" ]]; then
    IFS= read -r selected_data_token <"$selected_data_dir/$managed_marker" || die "could not read managed marker"
    [[ -n "$selected_data_token" ]] || die "managed marker is empty: $selected_data_dir/$managed_marker"
  else
    first_entry="$(find "$selected_data_dir" -mindepth 1 -maxdepth 1 -print -quit)"
    [[ -z "$first_entry" ]] || die "refusing non-empty unmanaged data directory: $selected_data_dir"
    selected_data_token="stratumstats-$(date +%s)-$$-$RANDOM-$RANDOM"
    printf '%s\n' "$selected_data_token" >"$selected_data_dir/$managed_marker"
  fi

  if [[ ! -f "$selected_data_dir/bitcoin.conf" ]]; then
    [[ -f "$config_template" ]] || die "missing config template: $config_template"
    config_tmp="$(mktemp "$selected_data_dir/.bitcoin.conf.XXXXXX")"
    cp "$config_template" "$config_tmp"
    chmod 600 "$config_tmp"
    mv "$config_tmp" "$selected_data_dir/bitcoin.conf"
    echo "Created $selected_data_dir/bitcoin.conf"
  fi
  warn_about_storage
}

remember_data_dir() {
  local state_tmp

  mkdir -p "$node_home"
  state_tmp="$(mktemp "$node_home/.data-state.XXXXXX")"
  printf '%s\n%s\n' "$selected_data_dir" "$selected_data_token" >"$state_tmp"
  chmod 600 "$state_tmp"
  mv "$state_tmp" "$data_state"
}

select_or_initialize_data_dir() {
  local target_data_dir

  if [[ -n "$data_override" ]]; then
    canonicalize_existing_data_dir "$data_override"
    target_data_dir="$selected_data_dir"
    if [[ -f "$data_state" ]]; then
      read_data_state
      if [[ "$target_data_dir" == "$stored_data_dir" ]]; then
        selected_data_dir="$stored_data_dir"
        selected_data_token="$stored_data_token"
        validate_selected_data_dir
        return
      fi
      selected_data_dir="$stored_data_dir"
      selected_data_token="$stored_data_token"
      validate_selected_data_dir
      if is_running || managed_process_running; then
        die "cannot change data directory while the remembered managed node is running"
      fi
    fi
    selected_data_dir="$target_data_dir"
    selected_data_token=""
    initialize_new_data_dir
    remember_data_dir
    return
  fi

  if [[ -f "$data_state" ]]; then
    select_existing_data_dir
    return
  fi

  mkdir -p "$default_data_dir"
  selected_data_dir="$(cd "$default_data_dir" && pwd -P)"
  selected_data_token=""
  initialize_new_data_dir
  remember_data_dir
}

platform_archive() {
  local platform
  platform="$(uname -s):$(uname -m)"
  case "$platform" in
    Linux:x86_64|Linux:amd64)
      echo "bitcoin-$core_version-x86_64-linux-gnu.tar.gz"
      ;;
    Linux:aarch64|Linux:arm64)
      echo "bitcoin-$core_version-aarch64-linux-gnu.tar.gz"
      ;;
    Darwin:x86_64|Darwin:amd64)
      echo "bitcoin-$core_version-x86_64-apple-darwin.tar.gz"
      ;;
    Darwin:arm64|Darwin:aarch64)
      echo "bitcoin-$core_version-arm64-apple-darwin.tar.gz"
      ;;
    *)
      die "unsupported platform: $platform (supported: Linux/macOS x86_64 or arm64)"
      ;;
  esac
}

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
  else
    die "sha256sum or shasum is required"
  fi
}

release_is_valid() {
  local candidate="$1"
  local version_line=""

  [[ -x "$candidate/bin/bitcoind" && -x "$candidate/bin/bitcoin-cli" ]] || return 1
  version_line="$("$candidate/bin/bitcoind" --version 2>/dev/null | awk 'NR == 1 {print; exit}')" || return 1
  [[ "$version_line" == *"version v$core_version"* ]]
}

install_core() {
  local archive
  local expected_hash
  local actual_hash
  local archive_path
  local extract_root
  local extracted_dir

  if release_is_valid "$release_dir"; then
    echo "Bitcoin Core $core_version is already installed in $release_dir"
    return
  fi
  [[ ! -e "$release_dir" ]] || die "incomplete or wrong release directory already exists: $release_dir"

  require_command curl
  require_command tar
  require_command awk
  [[ -f "$checksum_manifest" ]] || die "missing checksum manifest: $checksum_manifest"
  archive="$(platform_archive)"
  expected_hash="$(awk -v file="$archive" '$2 == file {print $1}' "$checksum_manifest")"
  [[ "$expected_hash" =~ ^[0-9a-f]{64}$ ]] || die "no unique pinned checksum for $archive"

  mkdir -p "$node_home" "$node_home/releases"
  install_work="$(mktemp -d "$node_home/.install.XXXXXX")"
  archive_path="$install_work/$archive"
  extract_root="$install_work/extracted"
  mkdir "$extract_root"

  echo "Downloading Bitcoin Core $core_version for $(uname -s) $(uname -m)..."
  curl --fail --location --proto '=https' --tlsv1.2 --retry 3 \
    --output "$archive_path" "$release_base_url/$archive"
  actual_hash="$(sha256_file "$archive_path")"
  [[ "$actual_hash" == "$expected_hash" ]] || die "SHA-256 verification failed for $archive"
  echo "Verified $archive"

  tar -xzf "$archive_path" -C "$extract_root"
  extracted_dir="$extract_root/bitcoin-$core_version"
  release_is_valid "$extracted_dir" || die "verified archive did not contain a working Bitcoin Core $core_version release"
  mv "$extracted_dir" "$release_dir"
  echo "Installed Bitcoin Core $core_version in $release_dir"
}

require_install() {
  release_is_valid "$release_dir" || die "Bitcoin Core is not installed correctly; run ./scripts/bitcoin-node.sh install"
}

core_cli() {
  "$bitcoin_cli" -datadir="$selected_data_dir" -conf="$selected_data_dir/bitcoin.conf" "$@"
}

is_running() {
  core_cli getblockchaininfo >/dev/null 2>&1
}

managed_process_running() {
  local pid_file="$selected_data_dir/bitcoind.pid"
  local pid=""
  local command_line=""

  managed_pid=""
  [[ -f "$pid_file" ]] || return 1
  IFS= read -r pid <"$pid_file" || return 1
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  kill -0 "$pid" 2>/dev/null || return 1
  command_line="$(ps -p "$pid" -o command= 2>/dev/null)" || return 1
  [[ "$command_line" == *"$bitcoin_bin"* ]] || return 1
  [[ "$command_line" == *"-datadir=$selected_data_dir"* ]] || return 1
  managed_pid="$pid"
}

start_node() {
  require_install
  select_or_initialize_data_dir
  if is_running; then
    echo "Bitcoin Core is already running."
    return
  fi
  if managed_process_running; then
    die "Bitcoin Core process $managed_pid is running but RPC is not ready; inspect logs"
  fi
  echo "Starting Bitcoin Core with data in $selected_data_dir"
  "$bitcoin_bin" -datadir="$selected_data_dir" -conf="$selected_data_dir/bitcoin.conf" -daemonwait
  is_running || die "Bitcoin Core did not become ready; inspect logs with ./scripts/bitcoin-node.sh logs"
  echo "Bitcoin Core is running. Use ./scripts/bitcoin-node.sh status to view sync progress."
}

fast_sync_node() {
  local requested_snapshot="$1"
  local snapshot_dir
  local snapshot_path

  require_install
  select_existing_data_dir
  is_running || die "Bitcoin Core is not running; start it before loading a snapshot"
  [[ -n "$requested_snapshot" ]] || die "snapshot path must not be empty"
  if [[ "$requested_snapshot" != /* ]]; then
    requested_snapshot="$PWD/$requested_snapshot"
  fi
  [[ -f "$requested_snapshot" ]] || die "snapshot is not a regular file: $requested_snapshot"
  [[ -r "$requested_snapshot" ]] || die "snapshot is not readable: $requested_snapshot"
  snapshot_dir="$(cd "$(dirname "$requested_snapshot")" && pwd -P)"
  snapshot_path="$snapshot_dir/$(basename "$requested_snapshot")"

  echo "Loading AssumeUTXO snapshot: $snapshot_path"
  echo "Bitcoin Core will reject it unless its UTXO-set hash and base height are compiled into Core $core_version."
  core_cli -rpcclienttimeout=0 loadtxoutset "$snapshot_path"
  echo "Snapshot loaded. The node will sync the snapshot chain to tip while full validation continues in the background."
  echo "The snapshot file can now be removed manually if desired."
  core_cli getchainstates
}

stop_node() {
  local timeout="${STRATUMSTATS_BITCOIN_STOP_TIMEOUT:-600}"
  local elapsed

  require_install
  select_existing_data_dir
  [[ "$timeout" =~ ^[0-9]+$ && "$timeout" -ge 1 ]] || die "STRATUMSTATS_BITCOIN_STOP_TIMEOUT must be a positive integer"

  if is_running; then
    core_cli stop >/dev/null
    echo "Graceful RPC shutdown requested; waiting for Bitcoin Core to flush data..."
  elif managed_process_running; then
    echo "RPC is unavailable; sending SIGTERM to verified managed process $managed_pid."
    kill -TERM "$managed_pid"
  else
    echo "Bitcoin Core is not running."
    return
  fi

  for ((elapsed = 0; elapsed < timeout; elapsed++)); do
    if ! is_running && ! managed_process_running; then
      echo "Bitcoin Core stopped."
      return
    fi
    sleep 1
  done
  die "Bitcoin Core is still shutting down after $timeout seconds; it was not force-killed"
}

status_node() {
  require_install
  select_existing_data_dir
  if is_running; then
    echo "Bitcoin Core $core_version is running."
    echo "Data directory: $selected_data_dir"
    core_cli getblockchaininfo
    echo "Chainstates:"
    core_cli getchainstates
    return
  fi
  if managed_process_running; then
    echo "Bitcoin Core process $managed_pid is running, but RPC is unavailable (starting, stopping, or unhealthy)."
    echo "Inspect: ./scripts/bitcoin-node.sh logs"
    return 4
  fi
  echo "Bitcoin Core is not running."
  return 3
}

logs_node() {
  local follow=false
  local log_file

  if [[ $# -gt 0 ]]; then
    [[ "$1" == "--follow" || "$1" == "-f" ]] || die "unknown logs option: $1"
    [[ $# -eq 1 ]] || die "logs accepts only --follow"
    follow=true
  fi
  select_existing_data_dir
  log_file="$selected_data_dir/debug.log"
  [[ -f "$log_file" ]] || die "log file does not exist yet: $log_file"
  if [[ "$follow" == true ]]; then
    tail -n 100 -f "$log_file"
  else
    tail -n 100 "$log_file"
  fi
}

show_paths() {
  echo "Bitcoin Core: $release_dir"
  if [[ -f "$data_state" ]]; then
    select_existing_data_dir
    echo "Node data:    $selected_data_dir"
  else
    echo "Node data:    $default_data_dir (not initialized)"
  fi
}

command_name="${1:-help}"
if [[ $# -gt 0 ]]; then
  shift
fi

case "$command_name" in
  install)
    parse_data_dir_args "$@"
    acquire_manager_lock
    preflight_data_override
    install_core
    select_or_initialize_data_dir
    echo "Data directory: $selected_data_dir"
    echo "Start it with ./scripts/bitcoin-node.sh start"
    ;;
  start)
    parse_data_dir_args "$@"
    acquire_manager_lock
    require_install
    preflight_data_override
    start_node
    ;;
  fast-sync)
    [[ $# -eq 1 ]] || die "fast-sync requires exactly one local snapshot path"
    acquire_manager_lock
    fast_sync_node "$1"
    ;;
  stop)
    [[ $# -eq 0 ]] || die "stop takes no options"
    acquire_manager_lock
    stop_node
    ;;
  status)
    [[ $# -eq 0 ]] || die "status takes no options"
    status_node
    ;;
  logs)
    logs_node "$@"
    ;;
  cli)
    [[ $# -gt 0 ]] || die "cli requires a bitcoin-cli command"
    require_install
    select_existing_data_dir
    core_cli "$@"
    ;;
  paths)
    [[ $# -eq 0 ]] || die "paths takes no options"
    show_paths
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    usage >&2
    die "unknown command: $command_name"
    ;;
esac
