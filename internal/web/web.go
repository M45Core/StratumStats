package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/M45Core/StratumStats/internal/ingest"
	"github.com/M45Core/StratumStats/internal/model"
	"github.com/M45Core/StratumStats/internal/report"
)

//go:embed templates/*.html static/*
var assets embed.FS

type Server struct {
	Pools  []model.Pool
	Load   func() ([]model.Observation, error)
	Demo   bool
	Ingest http.Handler
}

func (s Server) Handler() (http.Handler, error) {
	t, err := template.New("site").Funcs(template.FuncMap{
		"metric": func(value *float64) float64 {
			if value == nil {
				return 0
			}
			return *value
		},
		"btc":         formatBTC,
		"payoutPct":   formatPayoutPercentage,
		"payoutShare": payoutShare,
		"shortScript": shortScript,
		"historyTime": formatHistoryTime,
		"miningLoss":  formatMiningLoss,
		"feePct":      formatFeePercentage,
		"scoreGrade":  scoreGrade,
		"failedChecks": func(stats model.TimingStats) int {
			return stats.Timeouts + stats.Errors + stats.Rejected
		},
		"failureSummary": protocolFailureSummary,
	}).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	cache := &snapshotCache{pools: s.Pools, load: s.Load}
	snapshot := func(vantage string) (model.Snapshot, error) {
		return cache.snapshot(vantage, time.Now().UTC())
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		observations, err := cache.records(now)
		if err != nil {
			internalServerError(w, r, err)
			return
		}
		available := availableVantages(observations)
		statuses := buildVantageStatuses(observations, now)
		if !s.Demo {
			hideStaleRegionalVantages(available, statuses)
		}
		vantage := r.URL.Query().Get("vantage")
		if vantage == "" && s.Demo && (available["us-west"] || available["us-central"] || available["us-east"] || available["europe"]) {
			vantage = "us-all"
		} else if vantage == "" && !s.Demo {
			if available["us-west"] || available["us-central"] || available["us-east"] {
				vantage = "us-all"
			} else if available["unknown"] {
				vantage = "unknown"
			} else {
				vantage = "us-all"
			}
		}
		if !validVantage(vantage) {
			http.Error(w, "unknown vantage", http.StatusBadRequest)
			return
		}
		transport := r.URL.Query().Get("transport")
		if transport == "" {
			transport = "plain"
		}
		if transport != "plain" && transport != "tls" {
			http.Error(w, "unknown transport", http.StatusBadRequest)
			return
		}
		data, err := cache.snapshot(vantage, now)
		if err != nil {
			internalServerError(w, r, err)
			return
		}
		page := buildDashboardPage(data, s.Pools, s.Demo, vantage, selectedVantageStatus(statuses, vantage), transport)
		page.AvailableVantages = available
		page.ShowUSCombined = available["us-west"] || available["us-central"] || available["us-east"]
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = t.ExecuteTemplate(w, "index.html", page)
	})
	mux.HandleFunc("GET /coinbase", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /methodology", func(w http.ResponseWriter, r *http.Request) {
		data, err := snapshot("")
		if err != nil {
			internalServerError(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = t.ExecuteTemplate(w, "methodology.html", data)
	})
	mux.HandleFunc("GET /api/v1/pools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"pools": s.Pools,
			"disclosure": []string{
				"Names, categories, product terms, and status are registry metadata, not Stratum measurements.",
			},
		})
	})
	probeConfig, err := ingest.BuildProbeConfig(s.Pools)
	if err != nil {
		return nil, err
	}
	mux.HandleFunc("GET /api/v1/probe-config", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, probeConfig)
	})
	if s.Ingest != nil {
		mux.HandleFunc("POST /api/v1/ingest", func(w http.ResponseWriter, r *http.Request) {
			tracked := &statusResponseWriter{ResponseWriter: w}
			s.Ingest.ServeHTTP(tracked, r)
			if tracked.status == http.StatusAccepted {
				cache.invalidate()
			}
		})
	} else {
		mux.HandleFunc("POST /api/v1/ingest", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "ingest disabled", http.StatusMethodNotAllowed)
		})
	}
	mux.HandleFunc("GET /api/v1/reports", func(w http.ResponseWriter, r *http.Request) {
		vantage := r.URL.Query().Get("vantage")
		if !validVantage(vantage) {
			http.Error(w, "unknown vantage", http.StatusBadRequest)
			return
		}
		data, err := snapshot(vantage)
		if err != nil {
			internalServerError(w, r, err)
			return
		}
		writeJSON(w, data)
	})
	mux.HandleFunc("GET /api/v1/vantages", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		observations, err := cache.records(now)
		if err != nil {
			internalServerError(w, r, err)
			return
		}
		writeJSON(w, buildVantageStatuses(observations, now))
	})
	mux.HandleFunc("GET /api/v1/methodology", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"version": report.MethodologyVersion, "scoring": map[string]any{"scale": "0-100", "weights_pct": map[string]float64{"availability": report.ScoreAvailabilityWeight, "mining_loss": report.ScoreMiningLossWeight, "p95_delay": report.ScoreP95Weight, "responsiveness": report.ScoreResponsivenessWeight, "fee_stability": report.ScoreFeeStabilityWeight}, "mining_loss_full_score_below_pct": 0.1, "recent_fee_increase": map[string]any{"maximum_penalty_points": report.ScoreFeeIncreaseMaxPenalty, "decay_days": int(report.ScoreFeeIncreaseWindow / (24 * time.Hour))}, "high_fee": map[string]any{"threshold_pct": report.ScoreHighFeeThreshold, "penalty_points_per_excess_pct": report.ScoreHighFeePenaltyPerPct, "maximum_penalty_points": report.ScoreHighFeeMaxPenalty}, "invalid_tls_certificate": map[string]any{"penalty_points": report.ScoreTLSCertificatePenalty}, "solo_worker_wallet_not_found": map[string]any{"score": 0, "statuses": []string{"not_observed", "varied"}}, "missing_optional_components": "reweighted", "fee_stability_minimum_samples": 2}, "latency_window_hours": int(report.LatencyWindow / time.Hour), "retention_window_days": int(report.RetentionWindow / (24 * time.Hour)), "measurement_modes": []string{"continuous", "scheduled"}, "metrics": []string{"endpoint", "endpoint_tls", "endpoint_region", "blocks", "arrivals", "availability_pct", "median_ms", "p95_ms", "estimated_mining_loss_pct", "overall_score", "recent_fee_increase_penalty", "high_fee_penalty", "tls_certificate_penalty", "score_override_reason", "coinbase_samples", "worker_address_observed_pct", "worker_address_status", "latest_pool_fee_pct", "previous_pool_fee_pct", "pool_fee_changed", "pool_fee_changes", "pool_fee_samples", "pool_fee_last_changed_at", "latest_coinbase_observed_at", "latest_coinbase_total_sats", "latest_coinbase_output_count", "latest_payout_destinations", "latest_payout_destinations_truncated", "latest_payout_omitted_sats", "template_latency_history", "pool_fee_history", "tls_observed", "connect_timing", "tls_handshake_timing", "subscribe_timing", "authorize_timing", "ping_timing"}})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.Handle("GET /static/", http.FileServer(http.FS(assets)))
	return securityHeaders(mux), nil
}

func formatBTC(sats uint64) string {
	return fmt.Sprintf("%d.%08d BTC", sats/100_000_000, sats%100_000_000)
}

func formatPayoutPercentage(value float64) string {
	switch {
	case value > 0 && value < 0.0001:
		return "<0.0001%"
	case value > 0 && value < 0.01:
		return fmt.Sprintf("%.4f%%", value)
	default:
		return fmt.Sprintf("%.2f%%", value)
	}
}

func formatMiningLoss(value float64) string {
	if value < 0.1 {
		return "<0.1%"
	}
	return fmt.Sprintf("%.2f%%", value)
}

func formatFeePercentage(value float64) string {
	formatted := strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", value), "0"), ".")
	return formatted + "%"
}

func protocolFailureSummary(stats model.TimingStats) string {
	total := stats.Timeouts + stats.Errors + stats.Rejected
	parts := make([]string, 0, 3)
	if stats.Timeouts > 0 {
		parts = append(parts, fmt.Sprintf("%d timed out", stats.Timeouts))
	}
	if stats.Errors > 0 {
		parts = append(parts, fmt.Sprintf("%d error%s", stats.Errors, pluralSuffix(stats.Errors)))
	}
	if stats.Rejected > 0 {
		parts = append(parts, fmt.Sprintf("%d refused", stats.Rejected))
	}
	return fmt.Sprintf("%d failed check%s (%s)", total, pluralSuffix(total), strings.Join(parts, ", "))
}

func pluralSuffix(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func scoreGrade(score float64) string {
	if score < 0 {
		score = 0
	} else if score > 100 {
		score = 100
	}
	return fmt.Sprintf("score-grade-%d", int(score+5)/10)
}

func payoutShare(value, total uint64) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(value) / float64(total)
}

func shortScript(script string) string {
	if len(script) <= 36 {
		return script
	}
	return script[:20] + "…" + script[len(script)-12:]
}

func formatHistoryTime(value time.Time) string {
	if value.IsZero() {
		return "time unavailable"
	}
	return value.UTC().Format("02 Jan 15:04")
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}

func internalServerError(w http.ResponseWriter, _ *http.Request, err error) {
	log.Printf("web request failed: error=%q", err.Error()) // #nosec G706 -- %q escapes control characters.
	http.Error(w, "internal server error", http.StatusInternalServerError)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self' data:; object-src 'none'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		w.Header().Set("Cross-Origin-Opener-Policy", "same-origin")
		w.Header().Set("Permissions-Policy", "camera=(), geolocation=(), microphone=()")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
