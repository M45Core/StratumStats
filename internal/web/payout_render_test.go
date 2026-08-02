package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestDashboardSeparatesUnsafeCoinbaseObservations(t *testing.T) {
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
	h.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	body := page.Body.String()
	for _, want := range []string{
		"Normal pools",
		"Unsafe pools",
		"Worker address observed",
		"Worker address absent",
		"Not measured",
		"Present Pool",
		"Absent Pool",
		"Unknown Pool",
		"1/1 decoded jobs",
		"Pool fee",
		"0.750%",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	for _, unwanted := range []string{"Trust pool", "Direct coinbase", "Payout custody"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("dashboard contains old label %q", unwanted)
		}
	}
	normalAt, unsafeAt := strings.Index(body, "<h2>Normal pools</h2>"), strings.Index(body, "<h2>Unsafe pools</h2>")
	presentAt, unknownAt, absentAt := strings.Index(body, "Present Pool"), strings.Index(body, "Unknown Pool"), strings.Index(body, "Absent Pool")
	if normalAt < 0 || unsafeAt < 0 || !(normalAt < presentAt && presentAt < unknownAt && unknownAt < unsafeAt && unsafeAt < absentAt) {
		t.Fatalf("pools were not grouped alphabetically into normal then unsafe sections")
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
