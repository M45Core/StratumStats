package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/proofofmike/stratumstats/internal/ingest"
	"github.com/proofofmike/stratumstats/internal/model"
	"github.com/proofofmike/stratumstats/internal/report"
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
	}).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	cache := &snapshotCache{pools: s.Pools, load: s.Load}
	snapshot := func(vantage string) (model.Snapshot, error) {
		return cache.snapshot(vantage, time.Now().UTC())
	}
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		vantage := r.URL.Query().Get("vantage")
		if !validVantage(vantage) {
			http.Error(w, "unknown vantage", http.StatusBadRequest)
			return
		}
		now := time.Now().UTC()
		data, err := cache.snapshot(vantage, now)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		observations, err := cache.records(now)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		statuses := buildVantageStatuses(observations, now)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = t.ExecuteTemplate(w, "index.html", buildDashboardPage(data, s.Pools, s.Demo, vantage, selectedVantageStatus(statuses, vantage)))
	})
	mux.HandleFunc("GET /pools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = t.ExecuteTemplate(w, "pools.html", struct {
			AsOf  string
			Pools []model.Pool
		}{registryAsOf(s.Pools), s.Pools})
	})
	mux.HandleFunc("GET /coinbase", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /methodology", func(w http.ResponseWriter, r *http.Request) {
		data, err := snapshot("")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = t.ExecuteTemplate(w, "methodology.html", data)
	})
	mux.HandleFunc("GET /api/v1/pools", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"research_as_of": registryAsOf(s.Pools),
			"pools":          s.Pools,
			"disclosure": []string{
				"Names, categories, product terms, and status are researched metadata, not Stratum measurements.",
				"Advertised fees are operator claims captured on the listed check date and may change.",
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
	}
	mux.HandleFunc("GET /api/v1/reports", func(w http.ResponseWriter, r *http.Request) {
		vantage := r.URL.Query().Get("vantage")
		if !validVantage(vantage) {
			http.Error(w, "unknown vantage", http.StatusBadRequest)
			return
		}
		data, err := snapshot(vantage)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, data)
	})
	mux.HandleFunc("GET /api/v1/vantages", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		observations, err := cache.records(now)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, buildVantageStatuses(observations, now))
	})
	mux.HandleFunc("GET /api/v1/methodology", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"version": report.MethodologyVersion, "scoring": "none", "latency_window_hours": int(report.LatencyWindow / time.Hour), "measurement_modes": []string{"continuous", "scheduled"}, "metrics": []string{"blocks", "arrivals", "availability_pct", "median_ms", "p95_ms", "estimated_mining_loss_pct", "coinbase_samples", "worker_address_observed_pct", "worker_address_status", "latest_pool_fee_pct", "previous_pool_fee_pct", "pool_fee_changed", "pool_fee_changes", "pool_fee_samples", "pool_fee_last_changed_at", "latest_coinbase_observed_at", "latest_coinbase_total_sats", "latest_coinbase_output_count", "latest_payout_destinations", "latest_payout_destinations_truncated", "latest_payout_omitted_sats", "template_latency_history", "pool_fee_history", "tls_observed", "connect_timing", "tls_handshake_timing", "subscribe_timing", "authorize_timing", "ping_timing"}})
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

func registryAsOf(pools []model.Pool) string {
	var latest string
	for _, pool := range pools {
		if pool.LastVerified > latest {
			latest = pool.LastVerified
		}
	}
	return latest
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", "default-src 'self'; style-src 'self'; img-src 'self' data:; base-uri 'none'; frame-ancestors 'none'")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}
