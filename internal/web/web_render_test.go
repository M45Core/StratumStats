package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestDashboardFormatsLatencyValues(t *testing.T) {
	pool := model.Pool{ID: "measured", Name: "Measured Pool"}
	observation := model.Observation{Version: 1, ObservedAt: time.Now(), Vantage: "test", BlockID: "sample", PoolID: pool.ID, Eligible: true, Arrived: true, OffsetMS: 12.34}
	h, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return []model.Observation{observation}, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "12.3 ms") {
		t.Fatalf("latency not formatted: %s", w.Body.String())
	}
}
