package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestDashboardRendersPayoutCustody(t *testing.T) {
	fee := 0.75
	pool := model.Pool{ID: "direct", Name: "Direct Pool"}
	observation := model.Observation{Version: 1, ObservedAt: time.Now(), Vantage: "test", BlockID: "sample", PoolID: pool.ID, Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee}
	h, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return []model.Observation{observation}, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	if !strings.Contains(body, "Direct coinbase") || !strings.Contains(body, "0.750%") {
		t.Fatalf("payout evidence missing: %s", body)
	}
}
