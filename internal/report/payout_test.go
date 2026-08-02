package report

import (
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestComputeWorkerAddressObservationAndPoolFeeChanges(t *testing.T) {
	pool := model.Pool{ID: "solo", Name: "Solo"}
	fees := []float64{2, 1, 3}
	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	observations := make([]model.Observation, 0, len(fees))
	for i := range fees {
		fee := fees[i]
		observations = append(observations, model.Observation{ObservedAt: started.Add(time.Duration(i) * time.Hour), PoolID: pool.ID, BlockID: string(rune('a' + i)), Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee})
	}
	report := Compute([]model.Pool{pool}, observations, time.Now()).Reports[0]
	if report.WorkerAddressStatus != "always_observed" || report.CoinbaseSamples != 3 {
		t.Fatalf("report=%+v", report)
	}
	if report.LatestPoolFeePct == nil || *report.LatestPoolFeePct != 3 {
		t.Fatalf("latest fee=%v", report.LatestPoolFeePct)
	}
	if report.WorkerAddressObservedPct == nil || *report.WorkerAddressObservedPct != 100 {
		t.Fatalf("worker address observed=%v", report.WorkerAddressObservedPct)
	}
	if !report.PoolFeeChanged || report.PoolFeeChanges != 2 || report.PoolFeeSamples != 3 || report.PreviousPoolFeePct == nil || *report.PreviousPoolFeePct != 1 {
		t.Fatalf("fee change history=%+v", report)
	}
	if report.PoolFeeLastChangedAt == nil || !report.PoolFeeLastChangedAt.Equal(started.Add(2*time.Hour)) {
		t.Fatalf("last fee change=%v", report.PoolFeeLastChangedAt)
	}
}

func TestComputeVariedWorkerAddressObservations(t *testing.T) {
	pool := model.Pool{ID: "pool", Name: "Pool"}
	observations := []model.Observation{
		{PoolID: pool.ID, BlockID: "a", Eligible: true, Arrived: true, CoinbaseAnalyzed: true},
		{PoolID: pool.ID, BlockID: "b", Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true},
	}
	report := Compute([]model.Pool{pool}, observations, time.Now()).Reports[0]
	if report.WorkerAddressStatus != "varied" || report.WorkerAddressObservedPct == nil || *report.WorkerAddressObservedPct != 50 {
		t.Fatalf("report=%+v", report)
	}
}

func TestComputePoolFeeIgnoresSubHundredthNoise(t *testing.T) {
	pool := model.Pool{ID: "solo", Name: "Solo"}
	first, second := 68.050, 68.052
	observations := []model.Observation{
		{ObservedAt: time.Unix(1, 0), PoolID: pool.ID, BlockID: "a", Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &first},
		{ObservedAt: time.Unix(2, 0), PoolID: pool.ID, BlockID: "b", Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &second},
	}
	report := Compute([]model.Pool{pool}, observations, time.Now()).Reports[0]
	if report.PoolFeeChanged || report.PoolFeeChanges != 0 || report.LatestPoolFeePct == nil || *report.LatestPoolFeePct != 68.05 {
		t.Fatalf("sub-hundredth noise was treated as a change: %+v", report)
	}
}
