package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestDashboardDefaultsToPlainEndpointsAndCanSelectTLS(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	pool := model.Pool{ID: "pool", Name: "Pool", Category: "shared", Endpoints: []model.Endpoint{
		{Host: "plain.example", Port: 3333}, {Host: "secure.example", Port: 443, TLS: true},
	}}
	observations := []model.Observation{
		{Version: model.ObservationVersion, ObservedAt: now, Vantage: "us-west", BlockID: "block", PoolID: pool.ID, Endpoint: "plain.example:3333", Eligible: true, Arrived: true},
		{Version: model.ObservationVersion, ObservedAt: now, Vantage: "us-west", BlockID: "block", PoolID: pool.ID, Endpoint: "secure.example:443", TLS: true, Eligible: true, Arrived: true},
	}
	handler, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	plain := httptest.NewRecorder()
	handler.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, "/?vantage=us-west", nil))
	if !strings.Contains(plain.Body.String(), "plain.example:3333") || strings.Contains(plain.Body.String(), "secure.example:443") || !strings.Contains(plain.Body.String(), `data-transport="plain" aria-current="page"`) {
		t.Fatalf("plain endpoint view is incorrect: %s", plain.Body.String())
	}
	plainRow := renderedPoolRow(t, plain.Body.String(), pool.ID)
	if !strings.Contains(plainRow, `connection-tcp`) || strings.Contains(plainRow, `connection-tls`) {
		t.Fatalf("plain endpoint row does not show only TCP connection timing: %s", plainRow)
	}

	secure := httptest.NewRecorder()
	handler.ServeHTTP(secure, httptest.NewRequest(http.MethodGet, "/?vantage=us-west&transport=tls", nil))
	if !strings.Contains(secure.Body.String(), "secure.example:443") || strings.Contains(secure.Body.String(), "plain.example:3333") || !strings.Contains(secure.Body.String(), `data-transport="tls" aria-current="page"`) {
		t.Fatalf("TLS endpoint view is incorrect: %s", secure.Body.String())
	}
	secureRow := renderedPoolRow(t, secure.Body.String(), pool.ID)
	if !strings.Contains(secureRow, `connection-tls`) || strings.Contains(secureRow, `connection-tcp`) {
		t.Fatalf("TLS endpoint row does not show only TLS connection timing: %s", secureRow)
	}

	invalid := httptest.NewRecorder()
	handler.ServeHTTP(invalid, httptest.NewRequest(http.MethodGet, "/?transport=quic", nil))
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid transport status = %d, want 400", invalid.Code)
	}
}
