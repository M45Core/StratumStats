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
- Repeats each region's freshness and latest observed previous-block hash at
  every results section, and highlights newly observed blocks live.
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

### Pool registry changes

Validate an edited registry before deploying it:

```bash
go run . validate-config -config config/pools.json
```

`serve` and `collect` read their pool registry at startup. A running `serve`
process reloads it only when it receives `SIGUSR1` (`sudo systemctl reload
stratumstats`); it validates and builds the replacement
completely before atomically swapping handlers. If validation or
dashboard generation fails, the last good registry remains active. A local
collector must still be restarted because it owns long-lived pool sessions.

Production keeps the live registry at `/var/lib/stratumstats/pools.json`. Apply
a repository edit without rebuilding the binary with:

```bash
./scripts/update-pools-production.sh
```

That helper validates the file, installs it atomically, sends `SIGUSR1`, and
checks that `/api/v1/probe-config` exposes the expected revision. The current
and immediately previous probe revisions are accepted during a one-hour grace
window so an in-flight regional run is not rejected during endpoint removals.
The dashboard marks a region as `Pool update pending` until that Scout's latest
atomic block sample reports the active revision.

### Pool registry admin

A production `serve` process creates `/var/lib/stratumstats/admin.json` on first
startup and logs a one-time generated `admin` password. The file contains only
a salted PBKDF2-SHA256 password hash and is mode `0600`; retrieve the initial
password from the service journal and store it in a password manager:

```bash
sudo journalctl -u stratumstats | grep "ADMIN INITIAL CREDENTIAL"
```

Open `/admin/login` over HTTPS (or directly from localhost) to edit the
complete pool JSON. Saves use strict validation, an atomic file replacement,
and immediate activation through the same reload
path. Sessions are in memory, expire after 12 hours, and use HttpOnly,
SameSite=Strict cookies plus CSRF protection. To deliberately rotate a lost
admin password, stop the service, remove `admin.json`, and start it again; save
the newly logged one-time password.

## Measurement model

StratumStats records measurements rather than pool claims. Each report represents
one configured pool endpoint and transport (`pool + host:port + TLS mode`).
Each regional Scout sends and StratumStats stores one atomic nested sample per
Bitcoin block. Endpoint entries are present only when Scout obtained arrival or
new connection-setup data; the authenticated configuration roster supplies the
eligible endpoints omitted from that compact payload.
Template latency is relative to the earliest clean previous-block-hash
transition observed for the same block and vantage. Arrival is timestamped as
soon as the first byte of the Stratum message is readable by Scout, before the
remaining message is read or parsed.
Scout sends only the block hash, one arrival timestamp per responding endpoint,
and setup timings that actually occurred since the preceding block.
StratumStats authenticates the configured endpoint identities and calculates
relative offsets before JSONL storage. Every configured endpoint is eligible
for that block, so an endpoint that is down records a missed delivery and
mining loss.
Median, P95, history, and protocol timings use a rolling 24-hour window. No report,
score, count, payout, or fee evidence uses observations older than 30 days.
The production server checks the oldest JSONL observation weekly and atomically
compacts the file to that same 30-day horizon. A startup check acts only when
the file has accumulated more than one extra week, avoiding rewrites on routine
service restarts.

### Dashboard freshness

Every visible results section repeats the same region-wide status. `Region
updated` is not a per-pool or per-section timestamp: for a remote vantage it is
the observation time of that region's latest successfully accepted atomic block
sample. Legacy terminal run records remain supported. For a local collector it
falls back to the newest displayed pool observation.

The block marker is the previous-block hash from the latest report-eligible
Stratum transition observed in that region. It is not a separate chain-tip RPC,
so it advances only when that vantage completes another usable block sample.
After the initial page render, every visible copy flashes together when the
hash changes.

Protocol measurements include TCP connect, TLS handshake, `mining.subscribe`,
and `mining.authorize`. Connect and TLS stop at operation completion; subscribe
and authorize stop when the first response byte is readable. Subsequent message
transfer and parsing are excluded. They are measured only when a session
connects or reconnects, retained by Scout, and included in the next block
sample; absent operations are omitted.
Shared-pool fees are not shown because they cannot be measured from the block
coinbase.

The dashboard's `/methodology` page documents scoring, payout interpretation,
and limitations in detail.

Regional probe ingestion is provided by
[StratumScout](https://github.com/M45Core/StratumScout). Each regional view
compares measurements only within the same vantage. A block sample enters
availability and score calculations only after its single authenticated request
is validated and appended, so an interrupted upload cannot create a partial
scoring cohort. A completed regional sample is also excluded when at least 20%
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
unprivileged service account, seeds the persistent pool registry when absent,
preserves existing pools, observations, admin credentials, and ingest
credentials, and enables the service.

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

The repository includes an automatic pool-only deployment workflow for pushes
to `main` that change `config/pools.json`. It targets a self-hosted GitHub Actions
runner labeled `stratumstats-production`; that runner needs passwordless sudo
for the narrowly scoped install, move, and `systemctl reload` commands used by
`scripts/update-pools-production.sh`. The `production` GitHub environment can
add approval protection if desired.

## HTTP endpoints

The dashboard HTML is a static shell. It loads and periodically revalidates
`GET /dashboard-data`, whose JSON is generated only when observations change.
The service keeps serving the previous complete response while a replacement is
built, then swaps the new response cache into place atomically. Accepted ingest
requests are coalesced for ten seconds, matching the browser refresh interval,
so the atomic block samples uploaded by multiple regions for the same Bitcoin
block trigger one cache rebuild. Arrivals during a rebuild request
at most one follow-up rebuild.

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
| `/api/v1/probe-config` | `no-cache` | Scouts revalidate the current revision after startup or a `SIGUSR1` registry reload. |
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
