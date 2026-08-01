package web

import (
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
