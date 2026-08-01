# StratumStats

StratumStats is an independent Bitcoin mining-pool measurement service written
in Go. It records observable Stratum behavior as append-only JSONL and publishes
latency, availability, payout custody, pool-fee, TLS, and confidence data.

It deliberately does **not** assign pools a composite score, letter grade, or
rank. Those require subjective weights and would undermine the goal of an
unbiased report.

The pool registry is reproducibly seeded from the local `stratum-race` and
`PoolCensus` registries. Endpoints still require ongoing verification. Demo data
is synthetic and must not be published as real pool measurements.

![StratumStats dashboard with synthetic demonstration data](docs/stratumstats-dashboard.png)

_Regenerate this synthetic dashboard screenshot with `./scripts/screenshot.sh`._

## Principles

- **Measurements over claims.** Reports use automated observations, never paid
  placement, popularity, sponsorship, or operator questionnaires.
- **Metrics stay separate.** Availability, median latency, tail latency, payout
  custody, pool fee, TLS, sample count, and confidence remain independent facts.
- **Same block, same vantage.** Delivery delay is compared only for the same
  block observed from the same location, reducing geography and clock bias.
- **Uncertainty stays visible.** Confidence comes from eligible block count;
  small datasets remain explicitly provisional.
- **Raw evidence is canonical.** JSONL is append-only and independently
  recomputable. A future database may index it but will not replace it.
- **Pseudonymous, not magically anonymous.** Connections rotate a valid Bitcoin
  address, worker name, and miner agent. Pools can still observe the probe IP,
  so published vantage labels should be coarse regions.

## Quick start

Requires Go 1.22 or newer and has no third-party Go dependencies.

```bash
# Start a clearly labeled synthetic dashboard at http://localhost:8080.
go run .
```

For real measurements, use the same no-build entry point:

```bash
# Record real block observations until interrupted.
go run . collect -vantage us-west

# Serve reports recomputed from data/observations.jsonl.
go run . serve
```

Configuration is in `config/pools.json`. The collector never submits shares.
Current records use observation schema version 3 with `block_id` terminology.

Compiling a reusable binary is optional:

```bash
go build -o stratumstats .
./stratumstats collect -vantage us-west
```

## Published pool report

The deterministic aggregation is implemented in `internal/report/report.go`.

| Field | Meaning |
|---|---|
| Eligible blocks | Blocks for which the pool was connected at observation start |
| Availability | 95% Wilson lower bound of valid arrivals over eligible blocks |
| Median latency | Median delay behind the earliest valid template for the same block and vantage |
| P95 latency | 95th-percentile relative delay, exposing intermittent stalls |
| Confidence | Insufficient, provisional (10 blocks), moderate (30), or established (100) |
| TLS | Whether a configured TLS Stratum endpoint completed a measured session |
| Payout custody | Direct coinbase, trust pool, mixed, or unknown |
| Pool fee | Effective fee inferred from direct coinbase outputs |

Reports are sorted alphabetically by pool name, not by performance.

## Empty templates

Empty-first frequency is retained only as raw evidence and is not displayed or
used as a judgment. After a new block, Bitcoin Core must validate the block,
update chain and mempool state, and assemble a transaction-bearing template.
Sending coinbase-only work immediately can prevent miners from wasting time on
the stale parent.

The meaningful future measurement is empty-to-full duration and its estimated
transaction-fee opportunity cost—not whether an empty template appeared at all.
Bitcoin Core's `getblocktemplate` long poll wakes immediately for a new best
block, while transaction-only changes are checked less frequently.

## Payout custody and pool fee

The probe reconstructs each structurally valid coinbase using its negotiated
extranonces and matches the exact generated worker payout script.

- **Direct coinbase:** the worker script appears in the coinbase outputs.
- **Trust pool:** the worker script is absent, so payment depends on the pool's
  separate accounting system.
- **Mixed:** both behaviors were observed.
- **Unknown:** there is not enough decoded evidence.

For direct payment, the observed pool fee is:

```text
100 × (all coinbase outputs − worker outputs) / all coinbase outputs
```

Optional donations and configured payout splits can be included in this
effective percentage. Custodial payout fees and payment correctness cannot be
inferred from Stratum alone; they need a controlled hashrate/payment study.

Version 3 JSONL fields include `coinbase_analyzed`,
`worker_wallet_in_coinbase`, `coinbase_total_sats`, `worker_payout_sats`, and
`estimated_pool_fee_pct`. Reports expose `payout_mode`,
`direct_coinbase_pct`, `median_pool_fee_pct`, the observed fee range, and
`observed_fee_class` (`zero`, `positive`, `variable`, or `unknown`).

## Job verification

The collector currently performs:

1. strict `mining.notify` field and hex-length checks;
2. compact proof-of-work target sanity checks;
3. coinbase reconstruction with negotiated extranonces;
4. legacy and witness coinbase transaction parsing;
5. worker payout-script matching and output accounting;
6. merkle-root reconstruction from supplied branches;
7. clean-job and previous-hash transition checks.

Stratum V1 does not provide transaction bodies in `mining.notify`, so a passive
client can structurally verify work but cannot independently validate every
transaction. Optional Bitcoin Core RPC/ZMQ comparison is the next authoritative
verification layer.

## Pool registry

Regenerate the combined registry with:

```bash
./scripts/merge-pools.sh
```

Each pool and endpoint retains provenance from `stratum-race`, `PoolCensus`, or
both. The merge deduplicates normalized host/port/TLS tuples.

## HTTP API

- `GET /api/v1/reports` — current pool reports and disclosures
- `GET /api/v1/methodology` — published metrics and confidence thresholds
- `GET /healthz` — liveness

There is intentionally no public ingestion endpoint yet. This avoids exposing
an unauthenticated data-poisoning surface before signed collector reports and
anti-Sybil rules are designed.
