package web

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestDashboardCacheBuildFailureStopsStartup(t *testing.T) {
	const detail = "/srv/private/observations.jsonl: permission denied"
	_, err := (Server{Pools: []model.Pool{{ID: "test", Name: "Test Pool"}}, Load: func() ([]model.Observation, error) {
		return nil, errors.New(detail)
	}}).Handler()
	if err == nil || !strings.Contains(err.Error(), detail) {
		t.Fatalf("Handler error = %v, want load failure", err)
	}
}

func TestDashboardShellIsStaticAndDataIsCached(t *testing.T) {
	h, err := (Server{Pools: []model.Pool{{ID: "test", Name: "Test Pool"}}, Load: func() ([]model.Observation, error) { return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	if page.Code != http.StatusOK || !strings.Contains(page.Body.String(), "Loading measurements") {
		t.Fatalf("static shell status=%d body=%s", page.Code, page.Body.String())
	}
	if got := page.Header().Get("Cache-Control"); got != "public, max-age=300" {
		t.Fatalf("Cache-Control=%q", got)
	}

	data := httptest.NewRecorder()
	h.ServeHTTP(data, httptest.NewRequest(http.MethodGet, "/dashboard-data", nil))
	if data.Code != http.StatusOK || data.Header().Get("ETag") == "" {
		t.Fatalf("dashboard data status=%d headers=%v", data.Code, data.Header())
	}
	var payload dashboardPage
	if err := json.Unmarshal(data.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	gzipRequest := httptest.NewRequest(http.MethodGet, "/dashboard-data", nil)
	gzipRequest.Header.Set("Accept-Encoding", "gzip")
	gzipResponse := httptest.NewRecorder()
	h.ServeHTTP(gzipResponse, gzipRequest)
	if gzipResponse.Header().Get("Content-Encoding") != "gzip" || gzipResponse.Body.Len() >= data.Body.Len() {
		t.Fatalf("gzip headers=%v size=%d raw=%d", gzipResponse.Header(), gzipResponse.Body.Len(), data.Body.Len())
	}
	reader, err := gzip.NewReader(gzipResponse.Body)
	if err != nil {
		t.Fatal(err)
	}
	uncompressed, err := io.ReadAll(reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(uncompressed) != data.Body.String() {
		t.Fatal("gzip response differs from cached JSON")
	}
	conditional := httptest.NewRequest(http.MethodGet, "/dashboard-data", nil)
	conditional.Header.Set("If-None-Match", data.Header().Get("ETag"))
	notModified := httptest.NewRecorder()
	h.ServeHTTP(notModified, conditional)
	if notModified.Code != http.StatusNotModified || notModified.Body.Len() != 0 {
		t.Fatalf("conditional status=%d body=%q", notModified.Code, notModified.Body.String())
	}
	queryConditional := httptest.NewRequest(http.MethodGet, "/dashboard-data?generation="+data.Header().Get("ETag"), nil)
	queryNotModified := httptest.NewRecorder()
	h.ServeHTTP(queryNotModified, queryConditional)
	if queryNotModified.Code != http.StatusNotModified || queryNotModified.Body.Len() != 0 {
		t.Fatalf("query conditional status=%d body=%q", queryNotModified.Code, queryNotModified.Body.String())
	}
}

func TestSecurityHeaders(t *testing.T) {
	h, err := (Server{Pools: []model.Pool{{ID: "test", Name: "Test Pool"}}, Load: func() ([]model.Observation, error) { return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("health Cache-Control=%q", got)
	}
	for name, want := range map[string]string{"Content-Security-Policy": "object-src 'none'", "Cross-Origin-Opener-Policy": "same-origin", "Permissions-Policy": "camera=(), geolocation=(), microphone=()", "Referrer-Policy": "no-referrer", "X-Content-Type-Options": "nosniff", "X-Frame-Options": "DENY"} {
		if got := response.Header().Get(name); !strings.Contains(got, want) {
			t.Errorf("%s = %q", name, got)
		}
	}
}

func TestProbeConfigPublishesConfiguredEndpoints(t *testing.T) {
	pools := []model.Pool{{ID: "public", Name: "Public", Endpoints: []model.Endpoint{{Host: "public.example", Port: 3333}}}, {ID: "empty", Name: "Empty"}}
	h, err := (Server{Pools: pools, Load: func() ([]model.Observation, error) { return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/probe-config", nil))
	if got := response.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("probe config Cache-Control=%q", got)
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
	h.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/ingest", nil))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", response.Code)
	}
	if got := response.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("ingest Cache-Control=%q", got)
	}
}

func TestStaticAssetsAndMethodologyArePubliclyCacheable(t *testing.T) {
	h, err := (Server{Pools: []model.Pool{{ID: "test", Name: "Test"}}, Load: func() ([]model.Observation, error) { return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/methodology", "/static/dashboard.js", "/static/style.css"} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != "public, max-age=300" {
			t.Errorf("%s status=%d Cache-Control=%q", path, response.Code, response.Header().Get("Cache-Control"))
		}
	}
}
