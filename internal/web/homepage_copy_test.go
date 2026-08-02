package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestHomepageUsesConciseTelemetryCopy(t *testing.T) {
	h, err := (Server{
		Pools: []model.Pool{{ID: "test", Name: "Test Pool"}},
		Load:  func() ([]model.Observation, error) { return nil, nil },
		Demo:  true,
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	for _, want := range []string{
		"Bitcoin pool telemetry.",
		"Block template latency",
		"Normal pools",
		"Unsafe pools",
		"Bitcoin blocks observed",
		"Pools with block data",
		"Demo data — synthetic measurements shown for interface preview only.",
		"https://github.com/proofofmike/stratumstats",
		"/static/dashboard.js",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("homepage does not contain %q", want)
		}
	}
	for _, unwanted := range []string{
		"Independent pool telemetry",
		"Fair by construction",
		"No composite score",
		"Trust pool",
		"Direct coinbase",
		"Payout custody",
		"Confidence",
		"Leaderboard",
		"Current Leader",
		"Wins",
		"Races",
		"Template observations",
		"Wilson",
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("homepage still contains old copy %q", unwanted)
		}
	}

	script := httptest.NewRecorder()
	h.ServeHTTP(script, httptest.NewRequest("GET", "/static/dashboard.js", nil))
	if script.Code != 200 {
		t.Fatalf("dashboard updater status=%d", script.Code)
	}
	for _, want := range []string{"fetch(window.location.pathname + window.location.search", "data-pool-id", "row-updated"} {
		if !strings.Contains(script.Body.String(), want) {
			t.Errorf("dashboard updater missing %q", want)
		}
	}
	if strings.Contains(script.Body.String(), "location.reload") {
		t.Fatal("dashboard updater performs a full page reload")
	}
}
