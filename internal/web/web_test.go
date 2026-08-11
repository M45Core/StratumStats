package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestInternalErrorsAreNotExposed(t *testing.T) {
	const detail = "/srv/private/observations.jsonl: permission denied"
	for _, path := range []string{"/", "/methodology", "/api/v1/reports", "/api/v1/vantages"} {
		t.Run(path, func(t *testing.T) {
			h, err := (Server{
				Pools: []model.Pool{{ID: "test", Name: "Test Pool"}},
				Load:  func() ([]model.Observation, error) { return nil, errors.New(detail) },
			}).Handler()
			if err != nil {
				t.Fatal(err)
			}
			r := httptest.NewRecorder()
			h.ServeHTTP(r, httptest.NewRequest(http.MethodGet, path, nil))
			if r.Code != http.StatusInternalServerError {
				t.Fatalf("status = %d, want %d", r.Code, http.StatusInternalServerError)
			}
			if got := r.Body.String(); got != "internal server error\n" {
				t.Fatalf("body = %q, want generic error", got)
			}
			if strings.Contains(r.Body.String(), detail) {
				t.Fatalf("response exposed internal detail: %q", r.Body.String())
			}
		})
	}
}

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
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("Cache-Control=%q, want no-store", got)
	}
}

func TestSecurityHeaders(t *testing.T) {
	h, err := (Server{Pools: []model.Pool{{ID: "test", Name: "Test Pool"}}, Load: func() ([]model.Observation, error) { return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	for name, want := range map[string]string{
		"Content-Security-Policy":    "object-src 'none'",
		"Cross-Origin-Opener-Policy": "same-origin",
		"Permissions-Policy":         "camera=(), geolocation=(), microphone=()",
		"Referrer-Policy":            "no-referrer",
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
	} {
		if got := response.Header().Get(name); !strings.Contains(got, want) {
			t.Errorf("%s = %q, want it to contain %q", name, got, want)
		}
	}
}

func TestProbeConfigPublishesConfiguredEndpoints(t *testing.T) {
	pools := []model.Pool{
		{ID: "public", Name: "Public", Endpoints: []model.Endpoint{{Host: "public.example", Port: 3333}}},
		{ID: "empty", Name: "Empty"},
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
