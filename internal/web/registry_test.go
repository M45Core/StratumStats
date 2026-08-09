package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestPoolRegistryPageAndAPI(t *testing.T) {
	pool := model.Pool{
		ID:            "example",
		Name:          "Example Pool",
		Operator:      "Example Operator",
		Website:       "https://example.com/pool",
		Category:      "hybrid",
		Status:        "active",
		AuthModel:     "wallet",
		ProbeStatus:   "compatible",
		Description:   "A researched example pool.",
		Products:      []string{"SOLO", "PPLNS"},
		AdvertisedFee: "SOLO 0%; PPLNS 1%",
		FeeCheckedAt:  "2026-08-01",
		LastVerified:  "2026-08-01",
		ResearchSources: []model.Reference{
			{Title: "Official documentation", URL: "https://example.com/docs"},
		},
		Endpoints: []model.Endpoint{
			{Host: "pool.example.com", Port: 3333, Region: "Test", Label: "standard"},
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
		"https://discord.gg/WWemsuTktk",
		"hybrid",
		"SOLO 0%; PPLNS 1%",
		"pool.example.com:3333",
		"Official documentation",
		"2026-08-01",
	} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("registry page missing %q", want)
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
	var researchAsOf string
	if err := json.Unmarshal(envelope["research_as_of"], &researchAsOf); err != nil {
		t.Fatal(err)
	}
	var pools []model.Pool
	if err := json.Unmarshal(envelope["pools"], &pools); err != nil {
		t.Fatal(err)
	}
	if researchAsOf != "2026-08-01" || len(pools) != 1 || pools[0].Category != "hybrid" {
		t.Fatalf("unexpected registry API response: as_of=%q pools=%+v", researchAsOf, pools)
	}
}
