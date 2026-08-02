package web

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestDashboardRenders(t *testing.T) {
	h, err := (Server{Pools: []model.Pool{{ID: "test", Name: "Test Pool"}}, Load: func() ([]model.Observation, error) { return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestProbeConfigPublishesOnlyCompatibleEndpoints(t *testing.T) {
	pools := []model.Pool{
		{ID: "public", Name: "Public", ProbeStatus: "compatible", Endpoints: []model.Endpoint{{Host: "public.example", Port: 3333}}},
		{ID: "private", Name: "Private", ProbeStatus: "requires_credentials", Endpoints: []model.Endpoint{{Host: "private.example", Port: 3333}}},
	}
	h, err := (Server{Pools: pools, Load: func() ([]model.Observation, error) { return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/probe-config", nil))
	if response.Code != 200 {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body struct {
		ConfigRevision string `json:"config_revision"`
		Pools          []struct {
			ID string `json:"id"`
		} `json:"pools"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.ConfigRevision == "" || len(body.Pools) != 1 || body.Pools[0].ID != "public" {
		t.Fatalf("response=%+v", body)
	}
}

func TestIngestRouteIsDisabledUnlessConfigured(t *testing.T) {
	h, err := (Server{Pools: []model.Pool{{ID: "test", Name: "Test"}}, Load: func() ([]model.Observation, error) { return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/ingest", nil))
	if response.Code != 405 {
		t.Fatalf("status=%d, want 405", response.Code)
	}
}
