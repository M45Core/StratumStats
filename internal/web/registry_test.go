package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestPoolRegistryPageAndAPI(t *testing.T) {
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
	if page.Code != 200 {
		t.Fatalf("page status=%d body=%s", page.Code, page.Body.String())
	}
	for _, want := range []string{
		"Example Pool",
		"Know a pool that should be added, removed, or corrected?",
		"hybrid",
		`href="` + pool.Website + `"`,
		"Visit website",
	} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("registry page missing %q", want)
		}
	}
	for _, hidden := range []string{
		"pool.example.com",
		"3333",
		"Connection addresses",
		"data-copy-value",
		"/static/copy.js",
		">Sources<",
		"Account needed?",
		"No account needed",
		"Published fee",
		"Checked ",
		"2026-08-01",
	} {
		if strings.Contains(page.Body.String(), hidden) {
			t.Errorf("registry page exposes hidden connection detail %q", hidden)
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
