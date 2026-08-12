package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func dashboardPayload(t *testing.T, handler interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, path string) dashboardPage {
	t.Helper()
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
	if response.Code != 200 {
		t.Fatalf("%s status=%d body=%s", path, response.Code, response.Body.String())
	}
	var payload dashboardPage
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	return payload
}

func TestDashboardDataGroupsPoolsByMeasuredEvidence(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	fee, freeFee := 0.75, 0.0
	pools := []model.Pool{{ID: "paid", Name: "Paid", Category: "solo"}, {ID: "free", Name: "Free", Category: "solo"}, {ID: "missing", Name: "Missing", Category: "solo"}, {ID: "pending", Name: "Pending", Category: "solo"}, {ID: "pplns", Name: "PPLNS", Category: "shared", Products: []string{"PPLNS"}}, {ID: "other", Name: "Other", Category: "shared"}}
	observations := []model.Observation{
		{ObservedAt: now, Vantage: "test", BlockID: "paid", PoolID: "paid", Eligible: true, Arrived: true, OffsetMS: 10, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee},
		{ObservedAt: now, Vantage: "test", BlockID: "free", PoolID: "free", Eligible: true, Arrived: true, OffsetMS: 20, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &freeFee},
		{ObservedAt: now, Vantage: "test", BlockID: "missing", PoolID: "missing", Eligible: true, Arrived: true, OffsetMS: 30, CoinbaseAnalyzed: true},
		{ObservedAt: now, Vantage: "test", BlockID: "pending", PoolID: "pending", Eligible: true, Arrived: true, OffsetMS: 35},
		{ObservedAt: now, Vantage: "test", BlockID: "pplns", PoolID: "pplns", Eligible: true, Arrived: true, OffsetMS: 40},
		{ObservedAt: now, Vantage: "test", BlockID: "other", PoolID: "other", Eligible: true, Arrived: true, OffsetMS: 50},
	}
	h, err := (Server{Pools: pools, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	payload := dashboardPayload(t, h, "/dashboard-data")
	if len(payload.FreePools) != 1 || payload.FreePools[0].PoolID != "free" {
		t.Fatalf("free=%+v", payload.FreePools)
	}
	if len(payload.NormalPools) != 1 || payload.NormalPools[0].PoolID != "paid" {
		t.Fatalf("paid=%+v", payload.NormalPools)
	}
	if len(payload.MissingWalletPools) != 1 || payload.MissingWalletPools[0].OverallScore == nil || *payload.MissingWalletPools[0].OverallScore != 0 {
		t.Fatalf("missing=%+v", payload.MissingWalletPools)
	}
	if len(payload.PendingWalletPools) != 1 || payload.PendingWalletPools[0].PoolID != "pending" {
		t.Fatalf("pending=%+v", payload.PendingWalletPools)
	}
	if len(payload.PPLNSPools) != 1 || len(payload.OtherPools) != 1 {
		t.Fatalf("shared groups=%+v %+v", payload.PPLNSPools, payload.OtherPools)
	}
}

func TestDashboardDataContainsClientDetailModelWithoutWorkerDestination(t *testing.T) {
	previousFee, latestFee := 1.0, 1.25
	const workerAddress = "12ZEw5Hcv1hTb6YUQJ69y1V7uhcoDz92PH"
	now := time.Now().UTC().Add(-time.Minute)
	pool := model.Pool{ID: "detail", Name: "Detail", Category: "solo"}
	observations := []model.Observation{
		{ObservedAt: now.Add(-time.Hour), Vantage: "test", BlockID: "before", PoolID: pool.ID, Eligible: true, Arrived: true, OffsetMS: 10, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &previousFee},
		{ObservedAt: now, Vantage: "test", BlockID: "latest", PoolID: pool.ID, Eligible: true, Arrived: true, OffsetMS: 20, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &latestFee, CoinbaseTotalSats: 10_000, WorkerPayoutSats: 9_875, CoinbaseOutputCount: 2, CoinbaseOutputs: []model.CoinbaseOutput{{ValueSats: 9_875, Address: workerAddress, Worker: true}, {ValueSats: 125, Address: "bc1public", ScriptType: "p2wpkh"}}},
	}
	h, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	payload := dashboardPayload(t, h, "/dashboard-data")
	got := payload.NormalPools[0]
	if len(got.TemplateLatencyHistory) != 2 || len(got.LatencyChart.Points) != 2 || len(got.FeeChangeHistory) != 1 {
		t.Fatalf("detail history=%+v", got)
	}
	if len(got.LatestPayoutDestinations) != 1 || got.LatestPayoutDestinations[0].Address != "bc1public" {
		t.Fatalf("destinations=%+v", got.LatestPayoutDestinations)
	}
	body, _ := json.Marshal(payload)
	if strings.Contains(string(body), workerAddress) {
		t.Fatal("dashboard payload exposed worker destination")
	}
}

func TestBuildFeeChangeHistoryOmitsStableSamples(t *testing.T) {
	started := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	history := []model.MetricHistoryPoint{{ObservedAt: started, Value: 1}, {ObservedAt: started.Add(time.Minute), Value: 1}, {ObservedAt: started.Add(2 * time.Minute), Value: 1.25}, {ObservedAt: started.Add(3 * time.Minute), Value: 1.25}, {ObservedAt: started.Add(4 * time.Minute), Value: .75}}
	got := buildFeeChangeHistory(history)
	if len(got) != 2 || got[0].Previous != 1 || got[0].Value != 1.25 || got[1].Previous != 1.25 || got[1].Value != .75 {
		t.Fatalf("history=%+v", got)
	}
}
