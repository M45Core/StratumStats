# Pool registry research

Research date: 2026-08-01

StratumStats keeps researched operator context separate from measured telemetry. Names, product categories, operating status, endpoints, and advertised fees come from current web research and connection checks. They do not affect reports.

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
| Parasite, Public Pool, SoloPool.Com, ViaBTC | Corrected from a single-mode label to hybrid |
| Original Bitcoin P2Pool | Retained as an inactive decentralized historical reference with no central endpoint |

## Current registry

Advertised fees are snapshots, not promises. A blank fee means the review did not find sufficiently clear current first-party terms.

| Pool | Type | Status | Advertised fee | Current context |
|---|---|---|---|---|
| [2Miners Bitcoin SOLO](https://2miners.com/solo-btc-mining-pool) | solo | active | 1.5% | One product with three regions and TLS alternatives |
| [ANTPOOL](https://www.antpool.com/home?language=en) | shared | active | not confirmed | Account-based FPPS and PPLNS |
| [AtlasPool](https://atlaspool.io/) | solo | active | 1.5% | Finder address documented in coinbase output |
| [Binance Pool](https://pool.binance.com/en) | shared | active | not confirmed | Account-based FPPS |
| [Bitcoin Merch Lucky Pool](https://pool.bitcoinmerch.com/) | solo | active | 2% | Home-miner-oriented solo service |
| [Blitzpool](https://blitzpool.yourdevice.ch/) | hybrid | active | 0% to 1.5% by mode | Configured endpoints select its solo lane |
| [Braiins Pool](https://braiins.com/pool) | shared | active | 2.5% standard; 0% with Braiins OS | Account-based FPPS |
| [Braiins Solo](https://solo.braiins.com/) | solo | active | 0.5% | Anonymous solo product; finder-address output documented |
| [Solo CKPool](https://solo.ckpool.org/) | solo | active | 2% | Current global host replaces duplicate aliases |
| [f2pool](https://www.f2pool.com/) | shared | active | FPPS 4%; PPLNS 2% | Account-based shared products |
| [FindMyBlock.xyz](https://findmyblock.xyz/) | solo | active | 0% | Community home-miner pool |
| [Go Brrr Pool](https://pool.gobrrr.me/) | solo | active | 0% | Community pool; worker-address output documented |
| [HeliosPool](https://heliospool.com/) | solo | active | 1% | Regional multi-chain operator; Bitcoin record |
| [KanoPool](https://kano.is/) | hybrid | active | 0.5% | Account selects PPLNS or solo |
| [LetsMine.it](https://letsmine.it/) | hybrid | active | 1% for SOLO and PROP | Configured endpoint is the Bitcoin solo lane |
| [M45Core.com](https://m45core.com/) | solo | active | 0% | Main and corrected TinyMiner lanes; EU removed |
| [Mineshop Solo Pool](https://solo.mineshop.eu/) | solo | active | 0% | Finder address documented in coinbase output |
| [Noderunners Mining Pool](https://noderunners.network/en/pool) | solo | active | 0% | Community pool with TCP and TLS |
| [OCEAN](https://ocean.xyz/) | shared | active | 2%; 1% for miner-selected templates | TIDES rewards with miner-selected template option |
| [P2Pool original Bitcoin network](https://github.com/p2pool/p2pool) | decentralized | inactive | not applicable | Historical sharechain; no central endpoint |
| [Parasite Pool](https://parasite.space/) | hybrid | active | 0% | Finder bonus plus shared round |
| [Public Pool](https://web.public-pool.io/) | hybrid | active | 0% | Configured endpoints select solo; shared product also exists |
| [PyBLOCK](https://pool.pyblock.xyz/) | solo | active | 0.4% | Bitcoin solo product |
| [Satoshi Radio Mining Pool](https://pool.satoshiradio.nl/) | solo | active | not confirmed | Community home-miner pool |
| [SECPOOL](https://v3.secpool.com/) | shared | active | PPLNS 0% displayed; FPPS 4% | Account-based; displayed PPLNS rate may be promotional |
| [solo.cat](https://solo.cat/) | solo | active | 0% | Finder address documented in coinbase output |
| [SoloFury](https://solofury.com/) | solo | active | 1% | Multi-coin operator; Bitcoin record |
| [SoloHash Bitcoin Solo](https://solohash.co.uk/pool/bitcoin-solo) | solo | active | 0.5% | Three regions and two difficulty lanes |
| [SoloMining.de](https://pool.solomining.de/) | solo | active | 0% | Open-source TCP and TLS service |
| [SoloPool.Com](https://solopool.com/) | hybrid | active | 2% | Password selects solo, shared, or split mode |
| [SoloPool.org Bitcoin Solo](https://btc.solopool.org/) | solo | active | 1.5% | Separate operator from SoloPool.Com |
| [SpiderPool](https://www.spiderpool.com/) | shared | active | PPLNS 1% displayed; FPPS 4% | Account-based; PPLNS rate may be promotional |
| [ViaBTC](https://www.viabtc.com/) | hybrid | active | 1% to 4% by mode | Account-based PPS+, PPLNS, and SOLO |

## Measurement caveats exposed by the research

- Regional aliases must not appear as independent operators. They now share a canonical pool ID.
- Account-required services need configured credentials before authorization failures can be interpreted as downtime or before settlement terms can be confirmed.
- A hybrid endpoint can change economics through account settings or a Stratum password. Registry metadata records that ambiguity.
- Multiple endpoints for one canonical pool are connection alternatives, not extra independent evidence.
- Advertised fee text is dated and separate from coinbase deductions observed by StratumStats.
- SoloHash high-difficulty lanes remain flagged for repeated coinbase-output validation because recent templates have not been uniform.
