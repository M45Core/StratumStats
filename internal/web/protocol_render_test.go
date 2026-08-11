package web

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestProtocolTimingsRenderAndAppearInMethodologyAPI(t *testing.T) {
	observedAt := time.Now().UTC().Add(-time.Minute)
	duration := 12.6
	h, err := (Server{
		Pools: []model.Pool{
			{ID: "test", Name: "Test Pool", Endpoints: []model.Endpoint{{Host: "test.example", Port: 443, TLS: true}}},
			{ID: "healthy", Name: "Healthy TLS Pool", Endpoints: []model.Endpoint{{Host: "healthy.example", Port: 443, TLS: true}}},
			{ID: "awaiting", Name: "Awaiting TLS Pool", Endpoints: []model.Endpoint{{Host: "awaiting.example", Port: 443, TLS: true}}},
			{ID: "plaintext", Name: "Plaintext Pool", Endpoints: []model.Endpoint{{Host: "plaintext.example", Port: 3333}}},
		},
		Demo: true,
		Load: func() ([]model.Observation, error) {
			return []model.Observation{
				{Version: model.ObservationVersion, ObservedAt: observedAt, Vantage: "test", BlockID: "block", PoolID: "test", Eligible: true, Arrived: true, OffsetMS: 42.6},
				{Version: model.ObservationVersion, ObservedAt: observedAt, RecordType: model.RecordTypeProtocol, PoolID: "test", ProtocolMethod: model.ProtocolConnect, ResponseStatus: model.ProtocolStatusOK, DurationMS: &duration},
				{Version: model.ObservationVersion, ObservedAt: observedAt, RecordType: model.RecordTypeProtocol, PoolID: "test", ProtocolMethod: model.ProtocolConnect, ResponseStatus: model.ProtocolStatusTimeout},
				{Version: model.ObservationVersion, ObservedAt: observedAt, RecordType: model.RecordTypeProtocol, PoolID: "test", ProtocolMethod: model.ProtocolSubscribe, ResponseStatus: model.ProtocolStatusOK, DurationMS: &duration},
				{Version: model.ObservationVersion, ObservedAt: observedAt, RecordType: model.RecordTypeProtocol, PoolID: "test", ProtocolMethod: model.ProtocolTLSHandshake, ResponseStatus: model.ProtocolStatusError, ErrorCategory: model.ProtocolErrorTLSCertificateInvalid, DurationMS: &duration, TLS: true},
				{Version: model.ObservationVersion, ObservedAt: observedAt, RecordType: model.RecordTypeProtocol, PoolID: "healthy", ProtocolMethod: model.ProtocolTLSHandshake, ResponseStatus: model.ProtocolStatusOK, DurationMS: &duration, TLS: true},
			}, nil
		},
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	home := httptest.NewRecorder()
	h.ServeHTTP(home, httptest.NewRequest("GET", "/?transport=tls", nil))
	body := home.Body.String()
	for _, want := range []string{"Median block delay", "43 ms", "13 ms", "Security error", "tls-timing-error", "SECURITY ERROR", "The pool security certificate could not be verified", "1 ❌", "Invalid TLS certificate", "−10.0 pts"} {
		if !strings.Contains(body, want) {
			t.Errorf("homepage does not contain %q", want)
		}
	}
	if strings.Contains(body, "ok ") || strings.Contains(body, "/2") {
		t.Fatal("protocol success count is shown redundantly")
	}
	if strings.Contains(body, "· Secure connection") {
		t.Fatal("healthy TLS connection is labeled redundantly")
	}
	mutedTLSDash := `connection-tls tls-unavailable"><span><strong>—</strong></span>`
	plainHome := httptest.NewRecorder()
	h.ServeHTTP(plainHome, httptest.NewRequest("GET", "/", nil))
	plaintextRow := renderedPoolRow(t, plainHome.Body.String(), "plaintext")
	if !strings.Contains(plaintextRow, `connection-tcp`) || strings.Contains(plaintextRow, `connection-tls`) {
		t.Fatalf("plaintext view does not show only TCP connection timing: %s", plaintextRow)
	}
	awaitingRow := renderedPoolRow(t, body, "awaiting")
	if !strings.Contains(awaitingRow, mutedTLSDash) || strings.Contains(awaitingRow, "not offered") {
		t.Fatalf("configured but unchecked TLS state is unclear: %s", awaitingRow)
	}
	if strings.Contains(body, "Ping / pong") {
		t.Fatal("optional ping/pong still occupies a dashboard column")
	}
	if strings.Index(body, "43 ms") > strings.Index(body, "13 ms") {
		t.Fatal("block-template benchmark does not precede supporting protocol timing")
	}

	reports := httptest.NewRecorder()
	h.ServeHTTP(reports, httptest.NewRequest("GET", "/api/v1/reports", nil))
	if !strings.Contains(reports.Body.String(), `"certificate_errors":1`) {
		t.Fatalf("report API omitted Security error count: %s", reports.Body.String())
	}
	if !strings.Contains(reports.Body.String(), `"tls_certificate_penalty":10`) {
		t.Fatalf("report API omitted TLS certificate score penalty: %s", reports.Body.String())
	}
	stylesheet := httptest.NewRecorder()
	h.ServeHTTP(stylesheet, httptest.NewRequest("GET", "/static/style.css", nil))
	if !strings.Contains(stylesheet.Body.String(), ".tls-error-label,.tls-timing-error") || !strings.Contains(stylesheet.Body.String(), "color:var(--red)!important") {
		t.Fatal("Security error state is not styled red")
	}

	methodology := httptest.NewRecorder()
	h.ServeHTTP(methodology, httptest.NewRequest("GET", "/api/v1/methodology", nil))
	var payload struct {
		Metrics             []string `json:"metrics"`
		LatencyWindowHours  int      `json:"latency_window_hours"`
		RetentionWindowDays int      `json:"retention_window_days"`
		Scoring             struct {
			Scale                       string             `json:"scale"`
			WeightsPct                  map[string]float64 `json:"weights_pct"`
			MiningLossFullScoreBelowPct float64            `json:"mining_loss_full_score_below_pct"`
			RecentFeeIncrease           struct {
				MaximumPenaltyPoints float64 `json:"maximum_penalty_points"`
				DecayDays            int     `json:"decay_days"`
			} `json:"recent_fee_increase"`
			HighFee struct {
				ThresholdPct              float64 `json:"threshold_pct"`
				PenaltyPointsPerExcessPct float64 `json:"penalty_points_per_excess_pct"`
				MaximumPenaltyPoints      float64 `json:"maximum_penalty_points"`
			} `json:"high_fee"`
			InvalidTLSCertificate struct {
				PenaltyPoints float64 `json:"penalty_points"`
			} `json:"invalid_tls_certificate"`
			SoloWorkerWalletNotFound struct {
				Score    float64  `json:"score"`
				Statuses []string `json:"statuses"`
			} `json:"solo_worker_wallet_not_found"`
		} `json:"scoring"`
	}
	if err := json.Unmarshal(methodology.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !containsString(payload.Metrics, "subscribe_timing") || !containsString(payload.Metrics, "ping_timing") || !containsString(payload.Metrics, "estimated_mining_loss_pct") {
		t.Fatalf("protocol metrics missing from API: %v", payload.Metrics)
	}
	if !containsString(payload.Metrics, "overall_score") || !containsString(payload.Metrics, "recent_fee_increase_penalty") || !containsString(payload.Metrics, "high_fee_penalty") || !containsString(payload.Metrics, "tls_certificate_penalty") || !containsString(payload.Metrics, "score_override_reason") || payload.Scoring.Scale != "0-100" || payload.Scoring.WeightsPct["availability"] != 40 || payload.Scoring.WeightsPct["mining_loss"] != 25 || payload.Scoring.MiningLossFullScoreBelowPct != 0.1 || payload.Scoring.RecentFeeIncrease.MaximumPenaltyPoints != 15 || payload.Scoring.RecentFeeIncrease.DecayDays != 30 || payload.Scoring.HighFee.ThresholdPct != 2.5 || payload.Scoring.HighFee.PenaltyPointsPerExcessPct != 2.5 || payload.Scoring.HighFee.MaximumPenaltyPoints != 10 || payload.Scoring.InvalidTLSCertificate.PenaltyPoints != 10 || payload.Scoring.SoloWorkerWalletNotFound.Score != 0 || !containsString(payload.Scoring.SoloWorkerWalletNotFound.Statuses, "not_observed") || !containsString(payload.Scoring.SoloWorkerWalletNotFound.Statuses, "varied") {
		t.Fatalf("score methodology missing from API: %+v", payload.Scoring)
	}
	if payload.LatencyWindowHours != 24 {
		t.Fatalf("latency window=%d hours, want 24", payload.LatencyWindowHours)
	}
	if payload.RetentionWindowDays != 30 {
		t.Fatalf("retention window=%d days, want 30", payload.RetentionWindowDays)
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
