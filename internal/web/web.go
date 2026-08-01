package web

import (
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
	"github.com/proofofmike/stratumstats/internal/report"
)

//go:embed templates/*.html static/*
var assets embed.FS

type Server struct {
	Pools []model.Pool
	Load  func() ([]model.Observation, error)
	Demo  bool
}

type coinbasePage struct {
	Snapshot       model.Snapshot
	Demo           bool
	AlwaysObserved []model.PoolReport
	NotObserved    []model.PoolReport
	Varied         []model.PoolReport
	Unknown        []model.PoolReport
}

func (s Server) Handler() (http.Handler, error) {
	t, err := template.New("site").Funcs(template.FuncMap{"metric": func(value *float64) float64 {
		if value == nil {
			return 0
		}
		return *value
	}}).ParseFS(assets, "templates/*.html")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	snapshot := func() (model.Snapshot, error) {
		obs, err := s.Load()
		if err != nil {
			return model.Snapshot{}, err
		}
		return report.Compute(s.Pools, obs, time.Now()), nil
	}
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		data, err := snapshot()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = t.ExecuteTemplate(w, "index.html", struct {
			Snapshot model.Snapshot
			Demo     bool
		}{data, s.Demo})
	})
	mux.HandleFunc("GET /pools", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = t.ExecuteTemplate(w, "pools.html", struct {
			AsOf  string
			Pools []model.Pool
		}{registryAsOf(s.Pools), s.Pools})
	})
	mux.HandleFunc("GET /coinbase", func(w http.ResponseWriter, r *http.Request) {
		data, err := snapshot()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		page := coinbasePage{Snapshot: data, Demo: s.Demo}
		for _, pool := range data.Reports {
			switch pool.WorkerAddressStatus {
			case "always_observed":
				page.AlwaysObserved = append(page.AlwaysObserved, pool)
			case "not_observed":
				page.NotObserved = append(page.NotObserved, pool)
			case "varied":
				page.Varied = append(page.Varied, pool)
			default:
				page.Unknown = append(page.Unknown, pool)
			}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_ = t.ExecuteTemplate(w, "coinbase.html", page)
	})
	mux.HandleFunc("GET /methodology", func(w http.ResponseWriter, r *http.Request) {
		data, err := snapshot()
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
	mux.HandleFunc("GET /api/v1/reports", func(w http.ResponseWriter, r *http.Request) {
		data, err := snapshot()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		writeJSON(w, data)
	})
	mux.HandleFunc("GET /api/v1/methodology", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"version": report.MethodologyVersion, "scoring": "none", "metrics": []string{"blocks", "arrivals", "availability_pct", "median_ms", "p95_ms", "coinbase_samples", "worker_address_observed_pct", "worker_address_status", "median_pool_fee_pct", "tls_observed", "connect_timing", "tls_handshake_timing", "subscribe_timing", "authorize_timing", "ping_timing"}})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.Handle("GET /static/", http.FileServer(http.FS(assets)))
	return securityHeaders(mux), nil
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
