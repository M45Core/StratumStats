package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestCoinbaseObservationsAreSeparateAndNeutral(t *testing.T) {
	fee := 0.75
	pools := []model.Pool{
		{ID: "present", Name: "Present Pool"},
		{ID: "absent", Name: "Absent Pool"},
		{ID: "unknown", Name: "Unknown Pool"},
	}
	observations := []model.Observation{
		{Version: 1, ObservedAt: time.Now(), Vantage: "test", BlockID: "one", PoolID: "present", Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee},
		{Version: 1, ObservedAt: time.Now(), Vantage: "test", BlockID: "two", PoolID: "absent", Eligible: true, Arrived: true, CoinbaseAnalyzed: true},
	}
	h, err := (Server{Pools: pools, Load: func() ([]model.Observation, error) { return observations, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "/coinbase", nil))
	body := page.Body.String()
	for _, want := range []string{
		"Coinbase observations.",
		"Observed in every sample",
		"Not observed in sampled outputs",
		"No decoded coinbase samples",
		"Present Pool",
		"Absent Pool",
		"Unknown Pool",
		"100.0%",
		"0.750%",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("coinbase page missing %q", want)
		}
	}
	for _, unwanted := range []string{"Trust pool", "Direct coinbase", "Payout custody"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("coinbase page contains old label %q", unwanted)
		}
	}

	home := httptest.NewRecorder()
	h.ServeHTTP(home, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(home.Body.String(), "0.750%") || strings.Contains(home.Body.String(), "Payout custody") {
		t.Fatal("coinbase evidence leaked back into the main telemetry table")
	}

	api := httptest.NewRecorder()
	h.ServeHTTP(api, httptest.NewRequest("GET", "/api/v1/reports", nil))
	if !strings.Contains(api.Body.String(), `"worker_address_status":"always_observed"`) {
		t.Fatalf("neutral worker-address field missing from API: %s", api.Body.String())
	}
	for _, oldField := range []string{"payout_mode", "direct_coinbase_pct"} {
		if strings.Contains(api.Body.String(), oldField) {
			t.Errorf("API still contains old field %q", oldField)
		}
	}
}
