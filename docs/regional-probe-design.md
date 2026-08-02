# Regional probe design

Status: MVP implemented in StratumStats and StratumScout; live Fly canary pending

This specifies a separate, disposable Fly.io application that sends sampled
regional Stratum measurements to the main StratumStats server. It is not a
second StratumStats deployment: it has no dashboard, database, volume, public
service, or long-lived local state.

The first deployment has three United States vantages:

| Public vantage | Fly region | Location |
|---|---|---|
| `us-west` | `lax` | Los Angeles, California |
| `us-central` | `dfw` | Dallas, Texas |
| `us-east` | `iad` | Ashburn, Virginia |

Fly region codes are private deployment details. Reports publish only the
coarse vantage labels.

## Decisions

- The probe is its own repository, image, and Fly app.
- One Fly app contains one Machine in each region; there is not one app per
  region.
- Machines use Fly's native `hourly` schedule, run for five minutes, upload,
  and exit. Schedules are approximate and need not be synchronized.
- The main StratumStats server is the only durable collector and publisher.
- Relative block-template offsets are computed inside each regional process.
  Upload transit time is never treated as pool latency.
- Fly Machines have no volumes. Canonical data remains append-only JSONL on the
  main server.
- Scheduled data is disclosed as sampled measurement, never 24/7 availability.

## Components

```text
                          HTTPS + HMAC
  lax Machine  ─┐
  dfw Machine  ─┼── observation batches ──> StratumStats collector
  iad Machine  ─┘                              │
                                               ├── append-only JSONL
                                               ├── deterministic reports
                                               └── regional dashboard
```

## Standalone probe

The program has one lifecycle:

```text
fetch configuration -> connect -> measure -> upload -> exit
```

It is a static Go binary built with the standard library in a `scratch`
image. It does not listen on a port.

```sh
COLLECTOR_URL=https://stats.example.com \
INGEST_KEY_ID=current \
INGEST_SECRET=replace-with-32-random-bytes \
FLY_REGION=lax FLY_MACHINE_ID=local RUN_FOR=5m stratum-scout
```

| Environment | Meaning |
|---|---|
| `COLLECTOR_URL` | HTTPS origin of the main server |
| `INGEST_KEY_ID` | Active HMAC key identifier |
| `INGEST_SECRET` | Random HMAC-SHA256 secret stored as a Fly secret |
| `RUN_FOR` | Measurement window, default `5m` |
| `FLY_REGION` | Fly-supplied region, mapped to a public vantage |
| `FLY_MACHINE_ID` | Fly diagnostic identity; never published |

Unknown regions are fatal. This prevents an accidentally cloned Machine from
publishing under a misleading vantage.

On each start, the probe:

1. Maps `FLY_REGION` to a public vantage and creates a random 128-bit run ID.
2. Fetches the current endpoint configuration.
3. Opens all configured endpoints concurrently.
4. Measures TCP connect, TLS handshake where applicable, subscribe, authorize,
   and one ping attempt per successful session.
5. Rejects the initial current-block job. A measurement window opens only after
   a clean previous-hash transition seen while connected.
6. Structurally verifies jobs and computes delivery offsets from the first
   valid template seen in that vantage for the block.
7. Uploads batches during the run, performs a bounded final flush, and exits.

The probe never submits shares. Credentials rotate on every connection and are
not uploaded.

The existing 15-second block window and same-block/same-vantage rules remain.
Offsets use Go's monotonic clock inside one regional process. Wall-clock time is
evidence metadata, so clock skew and upload latency cannot change an offset.

Five minutes each hour samples roughly one twelfth of time. It should encounter
about 12 future Bitcoin block transitions per day per vantage on average, with
normal random variation. Reports must show the actual sample count.

## Collector API

The main server adds:

- `GET /api/v1/probe-config`: a minimal, versioned list of pool IDs and
  compatible endpoints.
- `POST /api/v1/ingest`: authenticate, validate, append, sync, and acknowledge
  one complete batch.

The configuration response includes a SHA-256 revision. Uploads identify the
revision used, making stale configuration visible without Fly storage.

The version 1 upload envelope is:

```json
{
  "schema_version": 1,
  "batch_id": "01J...",
  "run_id": "c0c8...",
  "agent_version": "0.1.0",
  "config_revision": "sha256:...",
  "region": "lax",
  "vantage": "us-west",
  "machine_id": "5683...",
  "started_at": "2026-08-01T17:55:00Z",
  "sent_at": "2026-08-01T18:00:05Z",
  "observations": []
}
```

Each observation follows the StratumStats schema and has an
`observation_id` made from the run ID and a monotonically increasing sequence.
IDs, durations, and timestamps are fixed before the first upload attempt.

Requests use gzip and include:

```text
Content-Type: application/json
Content-Encoding: gzip
X-StratumStats-Key-ID: <key id>
X-StratumStats-Timestamp: <Unix seconds>
X-StratumStats-Signature: <lowercase hex HMAC>
```

The signature is:

```text
HMAC-SHA256(secret, timestamp + "\n" + compressed_request_body)
```

Signing the exact transmitted bytes avoids ambiguous re-serialization. The
collector may accept the active and immediately previous key during rotation.
It returns `202 Accepted` only after all records are appended and synced.
Validation is all-or-nothing.

## Delivery semantics

The probe batches for five seconds or 100 observations. Network errors and
`5xx` responses retry with exponential backoff and jitter. `4xx` responses
are permanent failures. The in-memory queue is capped at 2,000 observations;
overflow drops the oldest observations and is reported explicitly.

An acknowledgement may be lost after append. A retry sends the identical
batch. Reports deduplicate by `observation_id`, retaining the first valid
occurrence. Raw JSONL stays append-only and may contain the retry. This avoids a
transactional idempotency database.

## Validation and trust

Ingestion must:

- require HTTPS at the public boundary;
- cap compressed input at 256 KiB and decompressed input at 1 MiB;
- accept no more than 500 observations;
- compare HMAC values in constant time;
- reject request timestamps more than five minutes from collector time;
- allow only configured keys, schema versions, regions, and region/vantage
  mappings;
- require known pool IDs and configured endpoint tuples;
- bound observation times to the declared run;
- require finite, non-negative durations and offsets;
- reject duplicate IDs within one batch; and
- replace provenance fields with values from the authenticated envelope.

The secret prevents public data poisoning. It does not make probes anonymous:
pools can observe source IPs. Machine and key IDs remain private.

## Schema, reporting, and UI

Observation schema version 6 adds:

- `observation_id` for retry-safe deduplication;
- `source` such as `local` or `fly-scheduled`; and
- `run_id` for operational tracing.

Older records remain readable. Appends are serialized with one writer lock and
one `fsync` per accepted batch.

The current registry has 60 endpoints. Three hourly probes will produce tens
of thousands of protocol records per day. The server must cache computed
snapshots and invalidate after append instead of re-reading all JSONL on every
ten-second dashboard poll. Daily JSONL rotation can follow without changing
the append-only evidence model.

Reports support:

```text
GET /api/v1/reports
GET /api/v1/reports?vantage=us-west
GET /api/v1/reports?vantage=us-central
GET /api/v1/reports?vantage=us-east
```

The unfiltered endpoint keeps its current aggregate behavior. Unknown
vantages return `400 Bad Request`.

`GET /api/v1/vantages` publishes each coarse label, measurement mode, last
successful run, last observation, configuration revision, sample counts, and
incomplete or dropped-record status.

The dashboard adds an `All data / US combined / West / Central / East`
selector and a last-seen indicator. It filters latency, availability, and
protocol timing while preserving the existing unfiltered view.
Coinbase-based pool safety remains global so a pool does not move between
Normal and Unsafe merely because one region lacks evidence.

The methodology calls Fly data **scheduled regional samples**. Availability is
deliveries divided by eligible samples while connected, not continuous uptime.

## Fly deployment and cost controls

The Fly app has no `http_service`, services, checks, mounts, public IPs, or
standby Machines. Each regional Machine uses:

- one shared CPU and 256 MiB RAM;
- restart policy `no`;
- native schedule `hourly`; and
- an ephemeral root filesystem.

The same image is created once in `lax`, `dfw`, and `iad`. Correctness does
not depend on synchronized starts.

At currently listed rates, three Machines running five minutes each hour cost
about $0.50-$0.65 per month in compute, before small network and stopped-rootfs
charges. Fly does not currently provide a native hard spending cap or billing
alerts. The practical ceiling is therefore architectural: exactly three fixed
Machines, no autoscaler, volumes, paid IPs, or managed services. If all three
failed to exit and ran continuously for a month, expected compute would remain
roughly $6-$8, plus small rootfs and bounded network charges.

The process enforces a hard deadline even when sessions or uploads are stuck.
It never sleeps until the next hour while the Machine remains billable.

## Run health

Every run emits a final `probe_run` record containing region, start/end times,
endpoint count, successful session count, accepted blocks, uploaded records,
dropped records, and status.

Logs include run ID, coarse vantage, pool ID, endpoint, and error category.
Secrets, credentials, full Stratum messages, and signatures are never logged.
A vantage becomes visibly stale after two missed hourly runs.

## Rollout

1. **Complete:** add schema v6, authenticated ingestion, validation, report
   deduplication, serialized append, caching, and contract tests to StratumStats.
2. **Complete:** create the standalone repository, finite executable, scratch image,
   bounded uploader, `httptest` collector contract, and local fake Stratum tests.
3. Run `lax` manually for 48 hours with uploads excluded from public reports.
4. Verify offsets, retries, termination, data volume, and cost.
5. Enable `dfw` and `iad`, then expose the vantage API and UI selector.
6. Publish only after endpoint and provenance review.

## Acceptance criteria

- Existing local `collect` behavior is unchanged.
- A Fly Machine has no volume or listening socket and exits by its deadline.
- Retrying a batch cannot change a published report.
- Forged, stale, oversized, or malformed batches append nothing.
- Regional offsets never use collector receipt time or another region's clock.
- The UI shows sample counts, scheduled mode, last-seen, and stale state.
- Removing the Fly app leaves the main server functional with local data.
