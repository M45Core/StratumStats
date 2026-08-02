# Regional probe rollout runbook

Status: implementation complete; deployment and live validation pending.

This is the operational checklist for introducing StratumScout without
publishing unvalidated regional measurements. The collector remains useful
without Fly, and removing the dedicated Scout app must never affect the main
StratumStats service.

## Gate 0: finish rollout controls

Do these before creating billable Fly Machines:

- [ ] Add a collector publication gate. Authenticated Scout observations must
  still append to JSONL during the canary, but public reports, the dashboard,
  and `/api/v1/vantages` must exclude them until explicitly enabled.
- [ ] Make Fly provisioning phased and inventory-aware: create `lax` alone,
  then add exactly one Machine in `dfw` and `iad` after approval. Refuse
  duplicate or unexpected Machines.
- [ ] Add a promotion path for updating the same three Machines without
  creating replacements or allocating services, IPs, or volumes.
- [ ] Create the StratumScout GitHub repository, attach its remote, and push the
  existing commits. Choose repository visibility explicitly.

Gate exit: tests cover disabled/enabled publication and deployment tooling can
dry-run the intended Machine inventory.

## Gate 1: prepare the collector

- [ ] Deploy the current StratumStats `main` commit to the production server.
- [ ] Confirm `data/observations.jsonl` is on persistent, backed-up storage and
  remains writable by the service account.
- [ ] Confirm the public collector origin uses HTTPS. The Go service may remain
  behind the existing TLS-terminating reverse proxy.
- [ ] Generate a random secret containing at least 32 bytes. Use one key ID,
  such as `regional-2026-01`, and store the same values under:
  - Main server: `STRATUMSTATS_INGEST_KEY_ID` and
    `STRATUMSTATS_INGEST_SECRET`.
  - Scout app: `INGEST_KEY_ID` and `INGEST_SECRET`.
- [ ] Restart StratumStats and confirm its logs say authenticated regional-probe
  ingestion is enabled without printing the secret.
- [ ] Confirm `GET /healthz` and `GET /api/v1/probe-config` return successfully
  through the public HTTPS origin.
- [ ] Confirm the probe configuration contains only intended compatible
  endpoint tuples and has a valid `sha256:` revision.

Gate exit: the production collector is healthy, authenticated ingestion is
enabled, and regional publication remains disabled.

## Gate 2: run the `lax` canary

Create one 256 MiB shared-CPU Machine in `lax` with:

- native schedule `hourly`;
- restart policy `no`;
- `RUN_FOR=5m`;
- no service, public IP allocation, volume, standby, or autoscaler; and
- default dynamic egress rather than paid static egress.

Run it for at least 48 hours. For every run, verify:

- [ ] The process exits and the Machine returns to `stopped` within the hard
  runtime bound.
- [ ] Configuration fetch and signed gzip uploads succeed.
- [ ] A final `probe_run` record is present with a plausible endpoint count,
  at least one successful session when pools are reachable, and zero dropped
  observations.
- [ ] Replayed batches do not duplicate published samples because observation
  IDs deduplicate deterministically.
- [ ] Pool rejections, timeouts, reconnects, and unsupported pings are bounded
  and do not keep the Machine running.
- [ ] Any captured block-template offsets are non-negative, same-vantage, and
  based on the first structurally valid template rather than upload time.
- [ ] JSONL growth, request volume, Fly runtime, and network usage are near the
  design estimate.
- [ ] No sensitive key, signature, randomized credential, Machine ID, or raw
  Stratum message appears in public output.

Record the canary start/end time, Scout image label, StratumStats commit,
configuration revision, run count, successful-session rate, dropped records,
data growth, and observed Fly cost.

Gate exit: 48 hours of bounded runs with credible measurements, no persistent
upload failures, and no unexplained data or cost growth.

## Gate 3: promote to three regions

- [ ] Add exactly one equivalent scheduled Machine in `dfw` and one in `iad`,
  using the same reviewed image and secrets.
- [ ] Confirm the complete inventory is exactly `lax`, `dfw`, and `iad`; there
  must be no fourth Machine.
- [ ] Observe all three regions privately for at least several successful runs.
- [ ] Confirm authenticated region mappings publish only `us-west`,
  `us-central`, and `us-east`.
- [ ] Enable the collector publication gate.
- [ ] Verify `All data`, `US combined`, `West`, `Central`, and `East` dashboard
  views and the corresponding report API filters.
- [ ] Verify `/api/v1/vantages` shows run health, samples, last-seen time, and
  stale or partial status correctly.

Gate exit: all three scheduled vantages are public with accurate disclosures
and sample counts.

## Operations and rollback

- Review the Fly Machine inventory and invoice after the first day, week, and
  month. Fly has no native hard spending cap for this workload.
- Treat a region as stale after two missed hourly runs and investigate logs,
  host capacity, collector reachability, and endpoint behavior.
- Rotate the HMAC key deliberately. Deploy collector acceptance first, update
  Scout secrets second, and remove the old key only after successful runs.
- Back up and eventually rotate JSONL files without changing append-only or
  observation-ID deduplication semantics.
- To halt collection, remove the dedicated Scout app or its scheduled Machines.
  Preserve collector data and logs first. Do not modify or remove the main
  StratumStats service.
- If measurements are questionable, disable regional publication immediately;
  retain raw authenticated records for diagnosis and only republish after a
  documented review.

## Completion record

Fill this in during rollout:

| Item | Value |
|---|---|
| Collector origin | |
| StratumStats commit | |
| Scout commit/image | |
| Configuration revision | |
| Canary start/end | |
| Canary runs | |
| Successful-session rate | |
| Dropped observations | |
| JSONL growth | |
| Fly canary cost | |
| Three-region enable time | |
| Publication enable time | |
| Reviewer | |
