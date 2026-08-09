# Pool registry research

Research date: 2026-08-04

StratumStats keeps researched operator context separate from measured telemetry. Names, product categories, operating status, endpoints, and advertised fees come from current web research and connection checks. They do not affect reports.

The published registry covers 27 active pool or product records, 87 Stratum V1 endpoints, and 13 endpoints advertised as TLS. Account-required and inactive records remain in the research metadata but are excluded from the published configuration. Every included record was checked against a first-party page, first-party help material, operator source code, or a current operator announcement where available. Missing fee text is intentional: the registry does not guess when current terms cannot be established.

The machine-readable source of truth is [pool-metadata.json](../config/pool-metadata.json). It records the exact source links and check dates for every pool. [pools.json](../config/pools.json) is generated from that metadata plus the local StratumRace and PoolCensus registries.

## Research method

For every imported entry, the review looked for:

1. a first-party operator, pool, help, status, terms, or source-code page;
2. current evidence of operation, such as live pool statistics, recently updated documentation, recent blocks, or a valid Stratum V1 subscription response;
3. the product actually selected by the listed endpoint, including account or password requirements;
4. advertised fees and settlement descriptions only when supported by a current first-party source;
5. aliases that represented regions or ports rather than independent pools.

A website returning HTTP 200 was not enough to call a mining service active. Conversely, a failed marketing site did not make a pool inactive when its dashboard and Stratum service were current.

A read-only network pass checked the imported web URLs and Stratum ports from a US Mountain vantage. A live collector session also crossed current block transitions and produced real job observations without submitting shares. One-vantage failures remain caveats rather than universal downtime claims.

## Taxonomy

- solo: one finder receives the block economics, normally less an advertised service fee.
- shared: rewards are distributed across contributed work.
- hybrid: the same operator or endpoint offers materially different solo and shared modes.
- decentralized: no central pool operator or single public Stratum service.

Status describes the researched service, not moment-to-moment availability:

- active: current operation was corroborated.
- inactive: the specific product is retired or effectively dormant.
- unverified: current operation could not be established.

Collector fit is separate. Account-based pools may publish jobs but still reject pseudonymous credentials or require a configured account before the selected product can be identified.

## Consolidations and corrections

| Imported data | Researched result |
|---|---|
| Three regional 2Miners rows | One 2Miners Bitcoin SOLO record with Europe, US, and Asia endpoints |
| Four SoloHash rows | One SoloHash Bitcoin Solo record with UK, Germany, and Canada endpoint lanes |
| CKPool plus CKPool Solo | One Solo CKPool record using the current global stratum.ckpool.org host |
| DMND legacy entry | Excluded because no current documented Stratum V1 endpoint remains |
| M45Core website set to localhost | Corrected to https://m45core.com/ |
| M45Core EU endpoints | Removed as retired, per operator knowledge and current DNS |
| TinyMiner ports 3333 and 4333 | Corrected to current ports 3334 and 4334 |
| Braiins Solo port 443 marked TLS | Corrected to documented plaintext Stratum |
| NodeRunners type unsure | Corrected to solo |
| Parasite and ViaBTC | Corrected from a single-mode label to hybrid |
| Public Pool and Blitzpool | Split solo and PPLNS into separate product records with their own endpoints |
| LetsMine.it | Split SOLO and PROP because ports 3332 and 3432 select independently measurable products |
| SoloPool.Com | Kept one SOLO record because the collector password selects SOLO; PROP and Solo Split share the same endpoints and would be mislabeled by duplicate rows |
| Original Bitcoin P2Pool | Retained as an inactive decentralized historical reference with no central endpoint |
| f2pool TLS | Replaced the stale `btcssl.f2pool.com` host with the current documented `btcssl-asia.f2pool.com` lanes |
| Braiins Pool and SpiderPool fees | Corrected current standard PPLNS/FPPS fee text from first-party fee schedules |
| Regional coverage | Added current documented lanes for Helios, KanoPool, LetsMine.it, Mineshop, SECPOOL, SoloFury, SoloPool.Com, SoloPool.org, SpiderPool, and ViaBTC |

## Current registry

Advertised fees are snapshots, not promises. A blank fee means the review did not find sufficiently clear current first-party terms.

| Pool | Type | Status | Advertised fee | Current context |
|---|---|---|---|---|
| [2Miners Bitcoin SOLO](https://2miners.com/solo-btc-mining-pool) | solo | active | 1.5% | One product with three regions and TLS alternatives |
| [AtlasPool](https://atlaspool.io/) | solo | active | 1.5% | Finder address documented in coinbase output |
| [Bitcoin Merch Lucky Pool](https://pool.bitcoinmerch.com/) | solo | active | 2% | Home-miner-oriented solo service |
| [Blitzpool — Solo](https://blitzpool.yourdevice.ch/mining-modes/solo) | solo | active | 0% | Dedicated solo endpoints |
| [Blitzpool — PPLNS](https://blitzpool.yourdevice.ch/mining-modes/pplns) | shared | active | 1% | Dedicated PPLNS endpoints |
| [Braiins Solo](https://solo.braiins.com/) | solo | active | 0.5% | Anonymous solo product; finder-address output documented |
| [Solo CKPool](https://solo.ckpool.org/) | solo | active | 2% | Current global host replaces duplicate aliases |
| [FindMyBlock.xyz](https://findmyblock.xyz/) | solo | active | 0% | Community home-miner pool |
| [Go Brrr Pool](https://pool.gobrrr.me/) | solo | active | 0% | Community pool; worker-address output documented |
| [HeliosPool](https://heliospool.com/) | solo | active | 1% | Regional multi-chain operator; Bitcoin record |
| [LetsMine.it — Bitcoin Solo](https://letsmine.it/coin/btc) | solo | active | 1% | Six regions on dedicated SOLO port 3332 |
| [LetsMine.it — Bitcoin PROP](https://letsmine.it/pool/BTCPOOL) | shared | active | 1% | Six regions on dedicated proportional port 3432 |
| [M45Core.com](https://m45core.com/) | solo | active | 0% | Main and corrected TinyMiner lanes; EU removed |
| [Mineshop Solo Pool](https://solo.mineshop.eu/) | solo | active | 0% | Finder address documented in coinbase output |
| [Noderunners Mining Pool](https://noderunners.network/en/pool) | solo | active | 0% | Community pool with TCP and TLS |
| [OCEAN](https://ocean.xyz/) | shared | active | 2%; 1% for miner-selected templates | TIDES rewards with miner-selected template option |
| [Parasite Pool](https://parasite.space/) | hybrid | active | 0% | Finder bonus plus shared round |
| [Public Pool — Solo](https://web.public-pool.io/) | solo | active | 0% | Dedicated ports 3333 and 4333 |
| [Public Pool — PPLNS](https://web.public-pool.io/) | shared | active | 0% | Dedicated ports 13333 and 14333 |
| [PyBLOCK](https://pool.pyblock.xyz/) | solo | active | 0.4% | Bitcoin solo product |
| [Satoshi Radio Mining Pool](https://pool.satoshiradio.nl/) | solo | active | not confirmed | Community home-miner pool |
| [solo.cat](https://solo.cat/) | solo | active | 0% | Finder address documented in coinbase output |
| [SoloFury](https://solofury.com/btc/) | solo | active | 1% | Nine Bitcoin regions; primary 6060 lanes measured, 6061 and 6062 remain documented failovers |
| [SoloHash Bitcoin Solo](https://solohash.co.uk/pool/bitcoin-solo) | solo | active | 0.5% | Three regions and two difficulty lanes |
| [SoloMining.de](https://pool.solomining.de/) | solo | active | 0% | Open-source TCP and TLS service |
| [SoloPool.Com — Solo](https://solopool.com/) | solo | active | 2% | Collector password selects the solo product |
| [SoloPool.org Bitcoin Solo](https://btc.solopool.org/) | solo | active | 1.5% | Europe and US with low, medium, and high difficulty; separate operator from SoloPool.Com |

## Live Stratum reachability audit

A bounded 20-second run of the native collector attempted all 137 endpoints present at the time of the audit from the same US Mountain vantage. It established TCP connections to 135 endpoints and completed a structurally valid `mining.subscribe` exchange with 131. Of those, 112 accepted the randomized anonymous authorization and 19 explicitly rejected it; the records marked `requires_credentials` and the inactive P2Pool record are now excluded from the published configuration.

Two endpoints did not establish TCP during that window: `de1.letsmine.it:3432` refused the connection, while `btc-af.spiderpool.com:2309` timed out even though the TLS lane on port 2310 was reachable. The LetsMine endpoint remains listed because it is in current first-party connection material and a single-vantage short failure is not enough to claim retirement. A separate citation pass received an HTTP response from 80 of 81 unique registry URLs; only the PyBLOCK dashboard timed out, while its Stratum endpoint still completed subscribe and authorization successfully. The collector submitted no shares, did not retain its randomized authorization credential, and emitted no wallet address in the temporary evidence.

## TLS certificate audit

A direct hostname-verifying TLS handshake was run against all 21 configured TLS endpoints on 2026-08-04. The probe required TLS 1.2 or newer, the endpoint hostname as SNI, a certificate valid for that hostname and time, and a chain trusted by the system CA store. Seventeen endpoints passed.

Four documented lanes did not complete a valid authenticated handshake:

- `btcssl-asia.f2pool.com:1300` and `:1301` returned a TLS internal-error alert before presenting any certificate, despite both remaining in f2pool current connection documentation.
- `pool.bitcoinmerch.com:4333` presented a self-signed certificate whose common name is `127.0.0.1`, not the configured hostname.
- `btc-ssl.powhashing.com:3333` resolved to three IPv4 backends: one served a valid Amazon-issued `powhashing.com` certificate, while two served Cloudflare Origin Certificates without publicly trusted chains. Certificate validity therefore depended on which backend answered.

At the time of the audit, these endpoints remained documented because the operators advertised them and hiding them would have concealed a material security failure. The collector performs normal certificate validation, records `tls_certificate_invalid` where applicable, and the dashboard renders any such result as a red certificate error. The status is measured telemetry rather than a permanent registry label, so it can recover automatically after an operator fixes the service.

The `parasite.wtf` marketing alias also had a hostname-mismatched web certificate during the audit. The registry uses the valid canonical `parasite.space` website, and the listed Parasite Pool Stratum endpoint is plaintext, so this is not represented as a mining-TLS result.

## Measurement caveats exposed by the research

- Regional aliases must not appear as independent operators. They now share a canonical pool ID.
- Account-required services need configured credentials before authorization failures can be interpreted as downtime or before settlement terms can be confirmed.
- A hybrid endpoint can change economics through account settings or a Stratum password. Registry metadata records that ambiguity.
- Multiple endpoints for one canonical pool are connection alternatives, not extra independent evidence.
- Advertised fee text is dated and separate from coinbase deductions observed by StratumStats.
- SoloHash high-difficulty lanes remain flagged for repeated coinbase-output validation because recent templates have not been uniform.
