# To-do

## Block-template correctness

Strengthen `internal/probe.VerifyJob` beyond its current structural checks.

- [ ] Add captured, real-world `mining.notify` golden vectors with independently
  verified coinbase transaction IDs and exact merkle roots.
- [ ] Reconstruct the complete 80-byte candidate header and compare it byte for
  byte with known-good vectors, including Bitcoin hash byte order.
- [ ] Add mutation tests for the previous hash, merkle-branch order and byte
  order, coinbase split, BIP34 height, `nBits`, and transaction serialization.
- [ ] Match Bitcoin Core's compact-target and coinbase consensus rules,
  including minimal BIP34 encoding, scriptSig limits, output-value bounds, and
  witness-commitment structure.
- [ ] Cross-check the same fixtures with an independent Bitcoin implementation
  to catch shared assumptions in the local verifier and its tests.
- [ ] Add an optional Bitcoin Core integration test that correlates `prevhash`,
  height, `nBits`, and `ntime` with chain state to identify stale, wrong-chain,
  or internally inconsistent jobs.
- [ ] Where full transaction data is available, verify the witness merkle root
  and witness commitment. Document that ordinary Stratum V1 `mining.notify`
  messages do not provide enough information to prove this independently.

Start with the deterministic golden vectors, exact merkle/header assertions,
and mutation tests. Keep live-node checks in a separate integration-test suite
so the default test run remains reproducible and offline.
