package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestDashboardDataFiltersEndpointTransport(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	pool := model.Pool{ID: "pool", Name: "Pool", Category: "shared", Endpoints: []model.Endpoint{{Host: "plain.example", Port: 3333}, {Host: "secure.example", Port: 443, TLS: true}}}
	observations := []model.Observation{{ObservedAt: now, Vantage: "us-west", BlockID: "block", PoolID: pool.ID, Endpoint: "plain.example:3333", Eligible: true, Arrived: true}, {ObservedAt: now, Vantage: "us-west", BlockID: "block", PoolID: pool.ID, Endpoint: "secure.example:443", TLS: true, Eligible: true, Arrived: true}}
	h, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	plain := dashboardPayload(t, h, "/dashboard-data?vantage=us-west")
	secure := dashboardPayload(t, h, "/dashboard-data?vantage=us-west&transport=tls")
	if plain.OtherPools[0].Endpoint != "plain.example:3333" || plain.SelectedTransport != "plain" {
		t.Fatalf("plain=%+v", plain.OtherPools)
	}
	if secure.OtherPools[0].Endpoint != "secure.example:443" || secure.SelectedTransport != "tls" {
		t.Fatalf("secure=%+v", secure.OtherPools)
	}
	invalid := httptest.NewRecorder()
	h.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/dashboard-data?transport=quic", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid status=%d", invalid.Code)
	}
}
