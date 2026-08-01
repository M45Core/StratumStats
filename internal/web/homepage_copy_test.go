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
		"Eligible blocks",
		"Demo data — synthetic measurements shown for interface preview only.",
		"https://github.com/proofofmike/stratumstats",
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
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("homepage still contains old copy %q", unwanted)
		}
	}
}
