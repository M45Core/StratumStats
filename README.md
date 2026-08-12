# StratumStats

StratumStats is a dependency-free Go service for measuring observable Bitcoin
mining-pool behavior. It collects Stratum V1 observations as append-only JSONL
and publishes a dashboard covering block-template delivery, availability,
connection timing, TLS, coinbase payouts, and measurable solo-pool fees.

![StratumStats dashboard with synthetic demonstration data](docs/stratumstats-dashboard.png)

<img src="docs/stratumstats-dashboard-mobile.png" alt="StratumStats mobile dashboard with synthetic demonstration data" width="390">

The screenshots use synthetic data and must not be presented as real pool
measurements.

## Features

- Compares template delivery only for the same Bitcoin block and vantage.
- Publishes median and P95 delay, availability, protocol timing, and sample counts.
- Repeats each region's freshness and latest observed Bitcoin block height at
  every results section, and highlights newly observed heights live.
- Measures solo-pool fees only when the probe worker output is found in coinbase.
- Verifies TLS certificates and reports failures explicitly.
- Keeps raw JSONL evidence independently recomputable.
- Provides regional views and a transparent 0–100 performance score.
- Never submits shares.

## Quick start

Requires Go 1.25.12 or newer and has no third-party Go dependencies.

```bash
# Start a synthetic dashboard at http://localhost:8080.
go run .

# Collect real observations until interrupted.
go run . collect -vantage us-west

# Serve endpoint reports from data/observations-v9.jsonl.
go run . serve
```

The dashboard binds to `127.0.0.1:8080` by default. Pass `-addr` explicitly
when it should listen on another interface.

Build a reusable binary with:

```bash
go build -o stratumstats .
```

Pool configuration lives in [`config/pools.json`](config/pools.json). Pass
`-filter-continent` to `collect` to skip known endpoints outside the collector's
continent; global and unlocated endpoints remain enabled.

## Measurement model

StratumStats records observations rather than pool claims. Each report represents
one configured pool endpoint and transport (`pool + host:port + TLS mode`).
Template latency is relative to the earliest structurally valid template observed
for the same block and vantage. Every configured endpoint is eligible for that
block, so an endpoint that is down records a missed delivery and mining loss.
Median, P95, history, and protocol timings use a rolling 24-hour window. No report,
score, count, payout, or fee evidence uses observations older than 30 days.
The production server checks the oldest JSONL observation weekly and atomically
compacts the file to that same 30-day horizon. A startup check acts only when
the file has accumulated more than one extra week, avoiding rewrites on routine
service restarts.

### Dashboard freshness

Every visible results section repeats the same region-wide status. `Region
updated` is not a per-pool or per-section timestamp: for a remote vantage it is
the completion time of that region's latest successful, lossless scheduled run.
For a local collector it falls back to the newest displayed pool observation.

The block-height pill is decoded from the BIP34 height in the coinbase input of
the latest report-eligible Stratum job observed in that region. It is an
observation from the selected vantage, not a separate chain-tip RPC, so it can
lag until that vantage receives and completes another usable cohort. After the
initial page render, every visible copy flashes together when the height
changes.

Protocol measurements include TCP connect, TLS handshake, `mining.subscribe`,
`mining.authorize`, and optional `mining.ping`. Coinbase reconstruction checks
whether the generated worker script is present and redacts that destination
before telemetry is stored. Shared-pool fees are not shown because they cannot
be measured from the block coinbase.

The dashboard's `/methodology` page documents scoring, payout interpretation,
and limitations in detail.

Regional probe ingestion is provided by
[StratumScout](https://github.com/M45Core/StratumScout). Each regional view
compares observations only within the same vantage. Scheduled block samples
enter availability and score calculations only after the complete, lossless
probe run has been received, so an interrupted upload cannot create a partial
scoring cohort. A completed regional cohort is also excluded when at least 20%
of eligible endpoints across at least five distinct pools miss together. This
retains the raw diagnostic evidence without treating a vantage-wide observer
failure as independent pool downtime.
Neither the misses nor the surviving measurements in an excluded cohort affect
availability, latency, payout evidence, fees, or score; the original JSONL
records remain available for diagnosis. The dashboard payload publishes the
number of excluded cohorts as `snapshot.excluded_regional_cohorts`.
The production Fly regions are IAD (`us-east`), FRA (`europe`), LAX
(`us-west`), NRT (`japan`), and SIN (`singapore`). IAD is the default view.
The embedded [`regions.json`](internal/model/regions.json) catalogs all current
Fly regions with friendly city/country names; `enabled` controls ingestion and
dashboard tabs, `order` controls display/default order, and `continent` is kept
for future combined views.

## Production installation

The generic systemd installer supports Debian and Ubuntu. It creates an
unprivileged service account, installs the pool registry, preserves existing
observations and credentials, and enables the service.

```bash
# Optional static build.
./scripts/build-production.sh

# Install the prebuilt binary. Omit --binary to build during installation.
sudo ./scripts/install-production.sh --binary .dist/stratumstats
```

The service listens on `127.0.0.1:8081` and stores observations in
`/var/lib/stratumstats/observations-v9.jsonl`. Review the
[systemd unit](deploy/stratumstats.service) and
[environment example](deploy/stratumstats.env.example) before installing. Use
`--no-start` to install without starting the service.

## HTTP endpoints

The dashboard HTML is a static shell. It loads and periodically revalidates
`GET /dashboard-data`, whose JSON is generated only when observations change.
The service keeps serving the previous complete response while a replacement is
built, then swaps the new response cache into place atomically.

- `GET /dashboard-data` — data used by the dashboard renderer
- `GET /api/v1/probe-config` — probe-compatible endpoint configuration
- `GET /healthz` — liveness

`POST /api/v1/ingest` is enabled only when
`STRATUMSTATS_INGEST_KEY_ID` and `STRATUMSTATS_INGEST_SECRET` are set. The secret
must contain at least 32 bytes.

### Reverse-proxy caching

StratumStats sends cache headers suited to each endpoint:

| Path | Policy | Reason |
| --- | --- | --- |
| `/`, `/methodology`, `/static/*` | `public, max-age=300` | These responses change only when the binary is replaced. The short lifetime prevents an old shell or script surviving a deployment for long. |
| `/dashboard-data` | `private, no-cache` with `ETag` | Browsers retain the selected region and transport response, then revalidate it. The client also sends the ETag as a `generation` query parameter so conditional refreshes survive proxies that discard `If-None-Match`. Unchanged data returns an empty `304`; updated data returns the atomically replaced response. |
| `/api/v1/probe-config` | `public, max-age=300` | Probe configuration changes when the service is restarted with a new registry. |
| `/api/v1/ingest`, `/healthz` | `no-store` | Writes and liveness checks must never be cached. |

Do not put `/dashboard-data` in an nginx `proxy_cache`: nginx cannot know when
an ingest has replaced the application's in-memory response. Let the request
reach StratumStats so its current `ETag` can produce either `304 Not Modified`
or the new cached JSON. The request does not recalculate reports or re-encode
JSON.

A minimal nginx reverse proxy can therefore rely on the upstream cache headers:

```nginx
server {
    listen 80;
    server_name stats.example.com;

    location / {
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    location = /dashboard-data {
        proxy_pass http://127.0.0.1:8081;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header If-None-Match $http_if_none_match;
        proxy_cache off;
        proxy_buffering on;
    }
}
```

Avoid overriding `Cache-Control`, `ETag`, `Vary`, or `Content-Encoding` on the
dashboard-data location. StratumStats pre-compresses each dashboard response
when its data changes and serves the cached gzip representation directly.
Explicitly forwarding `If-None-Match` protects conditional refreshes from a
broader nginx configuration that clears request validators. If an unchanged
conditional request returns `200` with the same ETag instead of an empty `304`,
inspect the effective configuration with `nginx -T`. An `X-Cache-Status: MISS`
response on `/dashboard-data` is a sign that the request is still entering an
nginx cache configuration; make sure the exact location above is active and is
not shadowed by generated or included configuration.

## Development

```bash
go test ./...
go vet ./...
```

Regenerate the synthetic screenshots with:

```bash
./scripts/screenshot.sh
./scripts/screenshot.sh --mobile
```
