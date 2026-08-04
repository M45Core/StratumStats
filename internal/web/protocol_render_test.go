package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestProtocolTimingsRenderAndAppearInMethodologyAPI(t *testing.T) {
	observedAt := time.Now().UTC().Add(-time.Minute)
	duration := 12.5
	h, err := (Server{
		Pools: []model.Pool{{ID: "test", Name: "Test Pool"}},
		Load: func() ([]model.Observation, error) {
			return []model.Observation{
				{Version: model.ObservationVersion, ObservedAt: observedAt, Vantage: "test", BlockID: "block", PoolID: "test", Eligible: true, Arrived: true, OffsetMS: 42.5},
				{Version: model.ObservationVersion, ObservedAt: observedAt, RecordType: model.RecordTypeProtocol, PoolID: "test", ProtocolMethod: model.ProtocolSubscribe, ResponseStatus: model.ProtocolStatusOK, DurationMS: &duration},
				{Version: model.ObservationVersion, ObservedAt: observedAt, RecordType: model.RecordTypeProtocol, PoolID: "test", ProtocolMethod: model.ProtocolTLSHandshake, ResponseStatus: model.ProtocolStatusError, ErrorCategory: model.ProtocolErrorTLSCertificateInvalid, DurationMS: &duration, TLS: true},
			}, nil
		},
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	home := httptest.NewRecorder()
	h.ServeHTTP(home, httptest.NewRequest("GET", "/", nil))
	body := home.Body.String()
	for _, want := range []string{"Block template latency", "42.5", "12.5", "TLS certificate error", "tls-timing-error", "CERT ERROR", "certificate validation failed", "0/1 ok"} {
		if !strings.Contains(body, want) {
			t.Errorf("homepage does not contain %q", want)
		}
	}
	if strings.Contains(body, "Ping / pong") {
		t.Fatal("optional ping/pong still occupies a dashboard column")
	}
	if strings.Index(body, "42.5") > strings.Index(body, "12.5") {
		t.Fatal("block-template benchmark does not precede supporting protocol timing")
	}

	reports := httptest.NewRecorder()
	h.ServeHTTP(reports, httptest.NewRequest("GET", "/api/v1/reports", nil))
	if !strings.Contains(reports.Body.String(), `"certificate_errors":1`) {
		t.Fatalf("report API omitted TLS certificate error count: %s", reports.Body.String())
	}
	stylesheet := httptest.NewRecorder()
	h.ServeHTTP(stylesheet, httptest.NewRequest("GET", "/static/style.css", nil))
	if !strings.Contains(stylesheet.Body.String(), ".tls-error-label,.tls-timing-error") || !strings.Contains(stylesheet.Body.String(), "color:var(--red)!important") {
		t.Fatal("TLS certificate error state is not styled red")
	}

	methodology := httptest.NewRecorder()
	h.ServeHTTP(methodology, httptest.NewRequest("GET", "/api/v1/methodology", nil))
	var payload struct {
		Metrics            []string `json:"metrics"`
		LatencyWindowHours int      `json:"latency_window_hours"`
	}
	if err := json.Unmarshal(methodology.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !containsString(payload.Metrics, "subscribe_timing") || !containsString(payload.Metrics, "ping_timing") || !containsString(payload.Metrics, "estimated_mining_loss_pct") {
		t.Fatalf("protocol metrics missing from API: %v", payload.Metrics)
	}
	if payload.LatencyWindowHours != 24 {
		t.Fatalf("latency window=%d hours, want 24", payload.LatencyWindowHours)
	}
	if strings.Contains(methodology.Body.String(), "minimum_blocks") || strings.Contains(methodology.Body.String(), "confidence") {
		t.Fatalf("methodology API contains qualitative confidence metadata: %s", methodology.Body.String())
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
