package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestPoolRegistryAPIIsRemovedAndWebsiteReachesDashboardData(t *testing.T) {
	pools := []model.Pool{{ID: "example", Name: "Example Pool", Website: "https://example.com/"}}
	h, err := (Server{Pools: pools, Load: func() ([]model.Observation, error) { return nil, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	payload := dashboardPayload(t, h, "/dashboard-data")
	if len(payload.NoRecentDataPools) != 1 || payload.NoRecentDataPools[0].Website != "https://example.com/" {
		t.Fatalf("pools=%+v", payload.NoRecentDataPools)
	}
	for _, path := range []string{"/pools", "/api/v1/pools"} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status=%d", path, response.Code)
		}
	}
}
