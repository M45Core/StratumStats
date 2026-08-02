package model

// ObservationVersion 6 adds authenticated-source provenance and stable record
// identifiers so retried remote batches can be deduplicated deterministically.
// Readers remain backward compatible with version 1 latency-only records.
const ObservationVersion = 6
