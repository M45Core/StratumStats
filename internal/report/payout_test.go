package report

import (
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestComputePayoutCustodyAndMedianPoolFee(t *testing.T) {
	pool := model.Pool{ID: "solo", Name: "Solo"}
	fees := []float64{2, 1, 3}
	observations := make([]model.Observation, 0, len(fees))
	for i := range fees {
		fee := fees[i]
		observations = append(observations, model.Observation{PoolID: pool.ID, BlockID: string(rune('a' + i)), Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee})
	}
	report := Compute([]model.Pool{pool}, observations, time.Now()).Reports[0]
	if report.PayoutMode != "direct" || report.PayoutSamples != 3 {
		t.Fatalf("report=%+v", report)
	}
	if report.PoolFeePct == nil || *report.PoolFeePct != 2 {
		t.Fatalf("fee=%v", report.PoolFeePct)
	}
	if report.DirectPayoutPct == nil || *report.DirectPayoutPct != 100 {
		t.Fatalf("direct=%v", report.DirectPayoutPct)
	}
	if report.FeeClass != "positive" || report.PoolFeeMinPct == nil || *report.PoolFeeMinPct != 1 || report.PoolFeeMaxPct == nil || *report.PoolFeeMaxPct != 3 {
		t.Fatalf("fee classification=%+v", report)
	}
}

func TestComputeCustodialAndMixedPayouts(t *testing.T) {
	pool := model.Pool{ID: "pool", Name: "Pool"}
	observations := []model.Observation{
		{PoolID: pool.ID, BlockID: "a", Eligible: true, Arrived: true, CoinbaseAnalyzed: true},
		{PoolID: pool.ID, BlockID: "b", Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true},
	}
	report := Compute([]model.Pool{pool}, observations, time.Now()).Reports[0]
	if report.PayoutMode != "mixed" || report.DirectPayoutPct == nil || *report.DirectPayoutPct != 50 {
		t.Fatalf("report=%+v", report)
	}
}
