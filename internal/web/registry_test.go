package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestRemovedPoolRegistryPageReturnsNotFoundAndAPIRemains(t *testing.T) {
	pool := model.Pool{
		ID:       "example",
		Name:     "Example Pool",
		Operator: "Example Operator",
		Website:  "https://example.com/",
		Category: "hybrid",
		Status:   "active",
		Products: []string{"SOLO", "PPLNS"},
		Endpoints: []model.Endpoint{
			{Host: "pool.example.com", Port: 3333, Region: "Test"},
		},
	}
	h, err := (Server{
		Pools: []model.Pool{pool},
		Load:  func() ([]model.Observation, error) { return nil, nil },
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "/pools", nil))
	if page.Code != 404 {
		t.Fatalf("page status=%d body=%s", page.Code, page.Body.String())
	}

	dashboard := httptest.NewRecorder()
	h.ServeHTTP(dashboard, httptest.NewRequest("GET", "/", nil))
	for _, want := range []string{`href="` + pool.Website + `"`, `aria-label="Visit the website for Example Pool"`, `target="_blank" rel="noopener noreferrer"`} {
		if !strings.Contains(dashboard.Body.String(), want) {
			t.Errorf("dashboard pool link missing %q", want)
		}
	}
	for _, unwanted := range []string{"/pools#example", "pool-info-link", ">Pool registry<"} {
		if strings.Contains(dashboard.Body.String(), unwanted) {
			t.Errorf("dashboard still contains removed registry link %q", unwanted)
		}
	}

	api := httptest.NewRecorder()
	h.ServeHTTP(api, httptest.NewRequest("GET", "/api/v1/pools", nil))
	if api.Code != 200 {
		t.Fatalf("api status=%d body=%s", api.Code, api.Body.String())
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(api.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	var pools []model.Pool
	if err := json.Unmarshal(envelope["pools"], &pools); err != nil {
		t.Fatal(err)
	}
	if len(pools) != 1 || pools[0].Category != "hybrid" {
		t.Fatalf("unexpected registry API response: pools=%+v", pools)
	}
}
