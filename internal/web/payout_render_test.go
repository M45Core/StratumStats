package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestDashboardSeparatesUnsafeCoinbaseObservations(t *testing.T) {
	previousFee, fee := 0.5, 0.75
	now := time.Now()
	pools := []model.Pool{
		{ID: "present", Name: "Present Pool"},
		{ID: "absent", Name: "Absent Pool"},
		{ID: "unknown", Name: "Unknown Pool"},
		{ID: "slower", Name: "Slower Pool"},
	}
	observations := []model.Observation{
		{Version: 1, ObservedAt: now.Add(-time.Hour), Vantage: "test", BlockID: "before", PoolID: "present", Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &previousFee},
		{Version: 1, ObservedAt: now, Vantage: "test", BlockID: "one", PoolID: "present", Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee},
		{Version: 1, ObservedAt: now, Vantage: "test", BlockID: "two", PoolID: "absent", Eligible: true, Arrived: true, CoinbaseAnalyzed: true},
		{Version: 1, ObservedAt: now, Vantage: "test", BlockID: "three", PoolID: "slower", Eligible: true, Arrived: true, OffsetMS: 50, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee},
	}
	h, err := (Server{Pools: pools, Load: func() ([]model.Observation, error) { return observations, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	body := page.Body.String()
	for _, want := range []string{
		"Normal pools",
		"Unsafe pools",
		"Present Pool",
		"Slower Pool",
		"Absent Pool",
		"Unknown Pool",
		"Pool fee",
		"0.75%",
		"changed 0.50 → 0.75%",
		"1 change(s) · 2 samples",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	for _, unwanted := range []string{"Trust pool", "Direct coinbase", "Payout custody", "Worker address observed", "Worker address absent"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("dashboard contains old label %q", unwanted)
		}
	}
	normalAt, unsafeAt := strings.Index(body, "<h2>Normal pools</h2>"), strings.Index(body, "<h2>Unsafe pools</h2>")
	presentAt, slowerAt := strings.Index(body, "Present Pool"), strings.Index(body, "Slower Pool")
	absentAt, unknownAt := strings.Index(body, "Absent Pool"), strings.Index(body, "Unknown Pool")
	if normalAt < 0 || unsafeAt < 0 || !(normalAt < presentAt && presentAt < slowerAt && slowerAt < unsafeAt && unsafeAt < absentAt && absentAt < unknownAt) {
		t.Fatalf("pools were not grouped by verification and sorted by template latency")
	}

	legacy := httptest.NewRecorder()
	h.ServeHTTP(legacy, httptest.NewRequest("GET", "/coinbase", nil))
	if legacy.Code != 301 || legacy.Header().Get("Location") != "/" {
		t.Fatalf("legacy coinbase route = %d %q, want 301 to /", legacy.Code, legacy.Header().Get("Location"))
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
