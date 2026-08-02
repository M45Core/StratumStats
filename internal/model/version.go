package model

// ObservationVersion 5 measures the first valid block-template delivery even
// when it is coinbase-only, and retains the protocol records introduced in v4.
// Readers remain backward compatible with version 1 latency-only records.
const ObservationVersion = 5
