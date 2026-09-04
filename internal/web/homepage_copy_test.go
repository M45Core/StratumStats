package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestStaticDashboardShellAndClientRenderer(t *testing.T) {
	h, err := (Server{Pools: []model.Pool{{ID: "test", Name: "Test"}}, Load: func() ([]model.Observation, error) { return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/", nil))
	for _, want := range []string{"Bitcoin pool performance.", "Free solo pools", "Paid solo pools", "PPLNS shared pools", "Worker wallet not found", "Loading measurements…", "/static/dashboard.js"} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("shell missing %q", want)
		}
	}
	for _, unwanted := range []string{"data-pool-base-id=", "/api/v1/reports", "/api/v1/vantages", "/api/v1/pools", "/api/v1/methodology"} {
		if strings.Contains(page.Body.String(), unwanted) {
			t.Errorf("shell contains dynamic or removed content %q", unwanted)
		}
	}
	if strings.Contains(page.Body.String(), `class="region-summary"`) {
		t.Fatal("regional status still rendered in the toolbar shell")
	}

	script := httptest.NewRecorder()
	h.ServeHTTP(script, httptest.NewRequest(http.MethodGet, "/static/dashboard.js", nil))
	for _, want := range []string{"fetch(`/dashboard-data?${requestParams}`", "fetch(`/pool-history?${params}`", `requestParams.set("generation", currentETag)`, `"If-None-Match": currentETag`, "response.status === 304", "response.json()", "currentETag", "data-sort-score", "Connect / setup", "connection-setup", "latency-chart-bar", "latency-chart-win", "latency-chart-hit-area", "Warning: invalid mining job detected", "Invalid mining job details", `section.querySelector("[data-section-meta]")`, "data-region-summary", "latest_block_height", "block-height-changed", "setInterval(() => refresh(false)"} {
		if !strings.Contains(script.Body.String(), want) {
			t.Errorf("renderer missing %q", want)
		}
	}
	if strings.Contains(script.Body.String(), "DOMParser") || strings.Contains(script.Body.String(), "response.text()") {
		t.Fatal("dashboard still downloads and parses rendered HTML")
	}

	style := httptest.NewRecorder()
	h.ServeHTTP(style, httptest.NewRequest(http.MethodGet, "/static/style.css", nil))
	for _, want := range []string{".job-validation-warning", ".job-warning-details", ".latency-chart-win", ".latency-chart-hit-area", ".measurement-row>.measurement-details{grid-column:1/-1", ".details-grid{display:grid;grid-template-columns:minmax(320px,480px) minmax(0,1fr);min-width:0;overflow:hidden"} {
		if !strings.Contains(style.Body.String(), want) {
			t.Errorf("dashboard style missing %q", want)
		}
	}
	if strings.Contains(style.Body.String(), "grid-template-columns:subgrid") {
		t.Fatal("per-section subgrid sizing can misalign expanded detail panels")
	}
}

func TestMethodologyRemainsHTMLOnly(t *testing.T) {
	h, err := (Server{Pools: []model.Pool{{ID: "test", Name: "Test"}}, Load: func() ([]model.Observation, error) { return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "/methodology", nil))
	for _, want := range []string{"How the measurements work.", "id=\"block-template-latency\"", "id=\"score-calculation\""} {
		if !strings.Contains(page.Body.String(), want) {
			t.Errorf("methodology missing %q", want)
		}
	}
	removed := httptest.NewRecorder()
	h.ServeHTTP(removed, httptest.NewRequest(http.MethodGet, "/api/v1/methodology", nil))
	if removed.Code != http.StatusNotFound {
		t.Fatalf("removed endpoint status=%d", removed.Code)
	}
}
