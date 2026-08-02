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
	duration := 12.5
	h, err := (Server{
		Pools: []model.Pool{{ID: "test", Name: "Test Pool"}},
		Load: func() ([]model.Observation, error) {
			return []model.Observation{
				{Version: model.ObservationVersion, ObservedAt: time.Now(), Vantage: "test", BlockID: "block", PoolID: "test", Eligible: true, Arrived: true, OffsetMS: 42.5},
				{Version: model.ObservationVersion, RecordType: model.RecordTypeProtocol, PoolID: "test", ProtocolMethod: model.ProtocolSubscribe, ResponseStatus: model.ProtocolStatusOK, DurationMS: &duration},
			}, nil
		},
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	home := httptest.NewRecorder()
	h.ServeHTTP(home, httptest.NewRequest("GET", "/", nil))
	body := home.Body.String()
	for _, want := range []string{"Block template latency", "42.5", "12.5", "Ping / pong", "mining.ping"} {
		if !strings.Contains(body, want) {
			t.Errorf("homepage does not contain %q", want)
		}
	}
	if strings.Index(body, "42.5") > strings.Index(body, "12.5") {
		t.Fatal("block-template benchmark does not precede supporting protocol timing")
	}

	methodology := httptest.NewRecorder()
	h.ServeHTTP(methodology, httptest.NewRequest("GET", "/api/v1/methodology", nil))
	var payload struct {
		Metrics []string `json:"metrics"`
	}
	if err := json.Unmarshal(methodology.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !containsString(payload.Metrics, "subscribe_timing") || !containsString(payload.Metrics, "ping_timing") {
		t.Fatalf("protocol metrics missing from API: %v", payload.Metrics)
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
