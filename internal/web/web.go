package web

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"time"

	"github.com/M45Core/StratumStats/internal/ingest"
	"github.com/M45Core/StratumStats/internal/model"
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
	t, err := template.New("site").ParseFS(assets, "templates/methodology.html")
	if err != nil {
		return nil, err
	}
	index, err := assets.ReadFile("templates/index.html")
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	cache := &snapshotCache{pools: s.Pools, load: s.Load}
	dashboardResponses := &dashboardResponseCache{}
	if err := dashboardResponses.rebuildFrom(cache, s.Pools, s.Demo); err != nil {
		return nil, err
	}
	dashboardRefreshes := &dashboardRefreshScheduler{
		refresh: func() error {
			cache.invalidate()
			return dashboardResponses.rebuildFrom(cache, s.Pools, s.Demo)
		},
		onError: func(err error) {
			log.Printf("dashboard cache rebuild failed: error=%q", err.Error())
		},
	}
	methodologyData, err := cache.snapshot("", time.Now().UTC())
	if err != nil {
		return nil, err
	}
	var methodology bytes.Buffer
	if err := t.ExecuteTemplate(&methodology, "methodology.html", methodologyData); err != nil {
		return nil, err
	}
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(index)
	})
	mux.HandleFunc("GET /dashboard-data", func(w http.ResponseWriter, r *http.Request) {
		vantage := r.URL.Query().Get("vantage")
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
		key := vantage + "\x00" + transport
		response, ok := dashboardResponses.response(key)
		if !ok {
			http.Error(w, "dashboard unavailable", http.StatusServiceUnavailable)
			return
		}
		writeCachedDashboard(w, r, response)
	})
	mux.HandleFunc("GET /coinbase", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/", http.StatusMovedPermanently)
	})
	mux.HandleFunc("GET /methodology", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=300")
		_, _ = w.Write(methodology.Bytes())
	})
	probeConfig, err := ingest.BuildProbeConfig(s.Pools)
	if err != nil {
		return nil, err
	}
	mux.HandleFunc("GET /api/v1/probe-config", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=300")
		writeJSON(w, probeConfig)
	})
	if s.Ingest != nil {
		mux.HandleFunc("POST /api/v1/ingest", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			tracked := &statusResponseWriter{ResponseWriter: w}
			s.Ingest.ServeHTTP(tracked, r)
			if tracked.status == http.StatusAccepted {
				dashboardRefreshes.schedule()
			}
		})
	} else {
		mux.HandleFunc("POST /api/v1/ingest", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			http.Error(w, "ingest disabled", http.StatusMethodNotAllowed)
		})
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		fmt.Fprintln(w, "ok")
	})
	mux.Handle("GET /static/", cacheControl("public, max-age=300", http.FileServer(http.FS(assets))))
	return securityHeaders(mux), nil
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

func cacheControl(value string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", value)
		next.ServeHTTP(w, r)
	})
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
