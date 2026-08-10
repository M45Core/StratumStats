package report

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestComputeWorkerAddressObservationAndPoolFeeChanges(t *testing.T) {
	pool := model.Pool{ID: "solo", Name: "Solo", Category: "solo"}
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

func TestComputeDoesNotPublishSharedPoolFee(t *testing.T) {
	pool := model.Pool{ID: "shared", Name: "Shared", Category: "shared"}
	fee := 1.5
	report := Compute([]model.Pool{pool}, []model.Observation{{
		PoolID: pool.ID, BlockID: "a", Eligible: true, Arrived: true,
		CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee,
	}}, time.Now()).Reports[0]
	if report.LatestPoolFeePct != nil || report.PoolFeeSamples != 0 || len(report.PoolFeeHistory) != 0 {
		t.Fatalf("shared-pool report exposes a fee: %+v", report)
	}
}

func TestComputePoolFeeIgnoresSubHundredthNoise(t *testing.T) {
	pool := model.Pool{ID: "solo", Name: "Solo", Category: "solo"}
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

func TestComputePublishesLatestPayoutSplitAndBoundedHistory(t *testing.T) {
	pool := model.Pool{ID: "solo", Name: "Solo", Category: "solo"}
	started := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	observations := make([]model.Observation, 0, 14)
	for i := 0; i < 14; i++ {
		fee := float64(i) / 10
		total := uint64(10_000)
		poolShare := uint64(i * 10)
		observation := model.Observation{
			ObservedAt: started.Add(time.Duration(i) * time.Hour),
			PoolID:     pool.ID, Vantage: "west", BlockID: fmt.Sprintf("block-%02d", i),
			Eligible: true, Arrived: true, OffsetMS: float64(i * 10),
			CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true,
			CoinbaseTotalSats: total, WorkerPayoutSats: total - poolShare,
			EstimatedPoolFeePct: &fee,
		}
		if i == 13 {
			observation.CoinbaseOutputCount = 3
			observation.CoinbaseOutputs = []model.CoinbaseOutput{
				{ValueSats: total - poolShare, ScriptPubKey: "76a914111111111111111111111111111111111111111188ac", Address: "12ZEw5Hcv1hTb6YUQJ69y1V7uhcoDz92PH", ScriptType: "p2pkh", Worker: true},
				{ValueSats: poolShare, ScriptPubKey: "52", ScriptType: "unknown"},
			}
		}
		observations = append(observations, observation)
	}

	got := Compute([]model.Pool{pool}, observations, started.Add(14*time.Hour)).Reports[0]
	if len(got.TemplateLatencyHistory) != reportHistoryLimit || len(got.PoolFeeHistory) != reportHistoryLimit {
		t.Fatalf("history lengths=%d/%d, want %d", len(got.TemplateLatencyHistory), len(got.PoolFeeHistory), reportHistoryLimit)
	}
	if got.TemplateLatencyHistory[0].Value != 20 || got.TemplateLatencyHistory[11].Value != 130 ||
		got.PoolFeeHistory[0].Value != 0.2 || got.PoolFeeHistory[11].Value != 1.3 {
		t.Fatalf("unexpected recent histories: latency=%+v fee=%+v", got.TemplateLatencyHistory, got.PoolFeeHistory)
	}
	if got.LatestCoinbaseObservedAt == nil || !got.LatestCoinbaseObservedAt.Equal(started.Add(13*time.Hour)) ||
		got.LatestCoinbaseTotalSats != 10_000 || got.LatestCoinbaseOutputCount != 3 || len(got.LatestPayoutDestinations) != 1 {
		t.Fatalf("latest payout metadata=%+v", got)
	}
	if got.LatestPayoutDestinations[0].Percentage != 1.3 || got.LatestPayoutDestinations[0].ScriptPubKey != "52" {
		t.Fatalf("latest payout destinations=%+v", got.LatestPayoutDestinations)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"12ZEw5Hcv1hTb6YUQJ69y1V7uhcoDz92PH", "76a914111111111111111111111111111111111111111188ac", "\"worker\":true"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("report exposed private worker destination %q: %s", private, encoded)
		}
	}
}
