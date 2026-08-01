#!/usr/bin/env bash
set -euo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
manager="$repo_dir/scripts/bitcoin-node.sh"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/stratumstats-bitcoin-node-test.XXXXXX")"
fake_bin="$test_root/bin"
state_dir="$test_root/state"
external_parent="$test_root/external-drive"
external_data="$external_parent/node-data"
resident_pid=""

cleanup() {
  if [[ -n "$resident_pid" ]]; then
    kill -TERM "$resident_pid" 2>/dev/null || true
    wait "$resident_pid" 2>/dev/null || true
  fi
  rm -rf -- "$test_root"
}
trap cleanup EXIT INT TERM

fail() {
  echo "test-bitcoin-node: $*" >&2
  exit 1
}

mkdir -p "$fake_bin"

cat >"$fake_bin/uname" <<'EOF'
#!/usr/bin/env bash
case "${1:-}" in
  -s) echo Linux ;;
  -m) echo x86_64 ;;
  *) echo Linux ;;
esac
EOF

cat >"$fake_bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
output=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output)
      output="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
[[ -n "$output" ]]
printf 'fake verified archive\n' >"$output"
EOF

cat >"$fake_bin/sha256sum" <<'EOF'
#!/usr/bin/env bash
if [[ "${FAKE_BAD_HASH:-0}" == 1 ]]; then
  printf '%064d  %s\n' 0 "$1"
else
  printf '%s  %s\n' b80d9c3e04da78fb6f0569685673418cf686fadba9042d926d13fb87ff503f9e "$1"
fi
EOF

cat >"$fake_bin/tar" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
destination=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -C)
      destination="$2"
      shift 2
      ;;
    *)
      shift
      ;;
  esac
done
[[ -n "$destination" ]]
mkdir -p "$destination/bitcoin-31.1/bin"
cat >"$destination/bitcoin-31.1/bin/bitcoind" <<'PROGRAM'
#!/usr/bin/env bash
set -euo pipefail
data_dir=""
resident=false
for argument in "$@"; do
  case "$argument" in
    --version)
      echo "Bitcoin Core version v31.1.0"
      exit 0
      ;;
    -datadir=*) data_dir="${argument#*=}" ;;
    -fake-resident) resident=true ;;
  esac
done
[[ -n "$data_dir" ]]
if [[ "$resident" == true ]]; then
  on_term() {
    rm -f "$data_dir/bitcoind.pid"
    exit 0
  }
  trap on_term TERM INT
  printf '%s\n' "$$" >"$data_dir/bitcoind.pid"
  printf 'fake resident Bitcoin Core started\n' >>"$data_dir/debug.log"
  while :; do sleep 0.1; done
fi
printf '%s\n' "$$" >"$data_dir/bitcoind.pid"
printf '__cookie__:fixture\n' >"$data_dir/.cookie"
touch "$data_dir/.fake-running"
printf 'fake Bitcoin Core started\n' >>"$data_dir/debug.log"
PROGRAM
cat >"$destination/bitcoin-31.1/bin/bitcoin-cli" <<'PROGRAM'
#!/usr/bin/env bash
set -euo pipefail
data_dir=""
command_name=""
for argument in "$@"; do
  case "$argument" in
    -datadir=*) data_dir="${argument#*=}" ;;
    -*) ;;
    *) command_name="$argument"; break ;;
  esac
done
[[ -n "$data_dir" ]]
case "$command_name" in
  getblockchaininfo)
    [[ -f "$data_dir/.fake-running" ]] || exit 1
    printf '%s\n' '{"chain":"main","blocks":123,"headers":456,"verificationprogress":0.5,"initialblockdownload":true,"pruned":true}'
    ;;
  getchainstates)
    [[ -f "$data_dir/.fake-running" ]] || exit 1
    printf "%s\n" "{\"headers\":456,\"chainstates\":[{\"blocks\":123},{\"blocks\":456,\"snapshot_blockhash\":\"fixture\"}]}"
    ;;
  loadtxoutset)
    [[ -f "$data_dir/.fake-running" ]] || exit 1
    snapshot_path="${*: -1}"
    [[ -f "$snapshot_path" ]] || exit 1
    printf "%s\n" "$snapshot_path" >"$data_dir/.fake-loaded-snapshot"
    printf "%s\n" "{\"coins_loaded\":100,\"tip_hash\":\"fixture\",\"base_height\":935000,\"path\":\"fixture\"}"
    ;;
  stop)
    [[ -f "$data_dir/.fake-running" ]] || exit 1
    rm -f "$data_dir/.fake-running" "$data_dir/bitcoind.pid" "$data_dir/.cookie"
    printf 'fake Bitcoin Core stopped\n' >>"$data_dir/debug.log"
    ;;
  *)
    printf 'fake bitcoin-cli: %s\n' "$command_name"
    ;;
esac
PROGRAM
chmod +x "$destination/bitcoin-31.1/bin/bitcoind" "$destination/bitcoin-31.1/bin/bitcoin-cli"
EOF

chmod +x "$fake_bin/uname" "$fake_bin/curl" "$fake_bin/sha256sum" "$fake_bin/tar"
mkdir -p "$external_data"

run_manager() {
  PATH="$fake_bin:$PATH" \
    STRATUMSTATS_BITCOIN_HOME="$state_dir" \
    STRATUMSTATS_BITCOIN_STOP_TIMEOUT=3 \
    "$manager" "$@"
}

run_manager install --data-dir "$external_data" >"$test_root/install.out"
[[ -x "$state_dir/releases/bitcoin-31.1/bin/bitcoind" ]] || fail "bitcoind was not installed"
[[ -f "$external_data/.stratumstats-bitcoin-node" ]] || fail "managed marker was not created"
[[ -f "$external_data/bitcoin.conf" ]] || fail "bitcoin.conf was not created"
grep -q '^prune=10000$' "$external_data/bitcoin.conf" || fail "pruning default is missing"
grep -q "$external_data" "$state_dir/data-state" || fail "external data path was not remembered"

if ! run_manager install >"$test_root/install-again.out" 2>&1; then
  cat "$test_root/install-again.out" >&2
  fail "second install failed"
fi
grep -q 'already installed' "$test_root/install-again.out" || fail "install was not idempotent"

run_manager start >"$test_root/start.out"
[[ -f "$external_data/.fake-running" ]] || fail "start did not launch the managed node"
snapshot_file="$test_root/utxo snapshot.dat"
printf "fake UTXO snapshot\n" >"$snapshot_file"
run_manager fast-sync "$snapshot_file" >"$test_root/fast-sync.out"
grep -q "base_height" "$test_root/fast-sync.out" || fail "fast-sync did not report the loaded snapshot"
grep -qx "$snapshot_file" "$external_data/.fake-loaded-snapshot" || fail "fast-sync did not preserve the snapshot path"
run_manager status >"$test_root/status.out"
grep -q 'initialblockdownload' "$test_root/status.out" || fail "status did not report chain sync state"
grep -q 'snapshot_blockhash' "$test_root/status.out" || fail "status did not report AssumeUTXO chainstates"

second_data="$external_parent/second-data"
mkdir "$second_data"
if run_manager start --data-dir "$second_data" >"$test_root/live-switch.out" 2>&1; then
  fail "manager changed data directories while the old node was running"
fi
grep -q 'cannot change data directory' "$test_root/live-switch.out" || fail "live data-directory switch error was unclear"
grep -q "$external_data" "$state_dir/data-state" || fail "failed switch changed remembered data path"
[[ -z "$(find "$second_data" -mindepth 1 -print -quit)" ]] || fail "failed switch initialized the new directory"

run_manager logs >"$test_root/logs.out"
grep -q 'fake Bitcoin Core started' "$test_root/logs.out" || fail "logs did not read the managed log"
run_manager stop >"$test_root/stop.out"
[[ ! -e "$external_data/.fake-running" ]] || fail "stop did not shut down the managed node"
if run_manager status >"$test_root/stopped-status.out" 2>&1; then
  fail "status unexpectedly succeeded for a stopped node"
fi
grep -q 'not running' "$test_root/stopped-status.out" || fail "stopped status was unclear"

mv "$external_data" "$external_parent/mounted-data"
mkdir "$external_data"
if run_manager status >"$test_root/unmounted.out" 2>&1; then
  fail "status accepted an empty underlying mountpoint"
fi
grep -q 'managed marker is missing' "$test_root/unmounted.out" || fail "missing-drive error was unclear"
[[ -z "$(find "$external_data" -mindepth 1 -print -quit)" ]] || fail "manager wrote into an unmounted drive path"
rmdir "$external_data"
mv "$external_parent/mounted-data" "$external_data"

mkdir "$state_dir/manager.lock"
if run_manager start >"$test_root/locked.out" 2>&1; then
  fail "manager ignored an active lifecycle lock"
fi
grep -q 'another node-manager command is active' "$test_root/locked.out" || fail "lock error was unclear"
rmdir "$state_dir/manager.lock"

PATH="$fake_bin:$PATH" "$state_dir/releases/bitcoin-31.1/bin/bitcoind" \
  -datadir="$external_data" -fake-resident &
resident_pid="$!"
for _ in $(seq 1 50); do
  [[ -f "$external_data/bitcoind.pid" ]] && break
  sleep 0.02
done
[[ -f "$external_data/bitcoind.pid" ]] || fail "resident fake did not start"
if run_manager status >"$test_root/rpc-unavailable.out" 2>&1; then
  fail "status treated a live process without RPC as healthy"
fi
grep -q 'RPC is unavailable' "$test_root/rpc-unavailable.out" || fail "RPC-unavailable status was unclear"
run_manager stop >"$test_root/resident-stop.out"
wait "$resident_pid"
resident_pid=""
grep -q 'sending SIGTERM to verified managed process' "$test_root/resident-stop.out" || fail "stop did not use the verified PID fallback"

bad_state="$test_root/bad-state"
if PATH="$fake_bin:$PATH" STRATUMSTATS_BITCOIN_HOME="$bad_state" FAKE_BAD_HASH=1 \
  "$manager" install >"$test_root/bad-hash.out" 2>&1; then
  fail "installer accepted a bad archive checksum"
fi
[[ ! -e "$bad_state/releases/bitcoin-31.1" ]] || fail "failed verification left an installed release"
[[ ! -e "$bad_state/data-state" && ! -e "$bad_state/data" ]] || fail "failed verification initialized node data"
grep -q 'SHA-256 verification failed' "$test_root/bad-hash.out" || fail "checksum error was unclear"

unmanaged="$test_root/unmanaged"
mkdir "$unmanaged"
printf 'existing data\n' >"$unmanaged/file"
if run_manager start --data-dir "$unmanaged" >"$test_root/unmanaged.out" 2>&1; then
  fail "manager accepted a non-empty unmanaged data directory"
fi
grep -q 'refusing non-empty unmanaged' "$test_root/unmanaged.out" || fail "unmanaged-directory error was unclear"

echo "bitcoin-node mocked lifecycle tests passed"
