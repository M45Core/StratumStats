package model

import (
	"encoding/json"
	"testing"
)

func TestObservationReadsVersion2Names(t *testing.T) {
	var observation Observation
	err := json.Unmarshal([]byte(`{"version":2,"race_id":"legacy-block","estimated_coinbase_deduction_pct":1.25}`), &observation)
	if err != nil {
		t.Fatal(err)
	}
	if observation.BlockID != "legacy-block" {
		t.Fatalf("block_id=%q", observation.BlockID)
	}
	if observation.EstimatedPoolFeePct == nil || *observation.EstimatedPoolFeePct != 1.25 {
		t.Fatalf("pool fee=%v", observation.EstimatedPoolFeePct)
	}
}

func TestObservationPrefersVersion3Names(t *testing.T) {
	var observation Observation
	err := json.Unmarshal([]byte(`{"version":3,"block_id":"current-block","estimated_pool_fee_pct":0.5,"race_id":"old"}`), &observation)
	if err != nil {
		t.Fatal(err)
	}
	if observation.BlockID != "current-block" {
		t.Fatalf("block_id=%q", observation.BlockID)
	}
	if observation.EstimatedPoolFeePct == nil || *observation.EstimatedPoolFeePct != 0.5 {
		t.Fatalf("pool fee=%v", observation.EstimatedPoolFeePct)
	}
}
