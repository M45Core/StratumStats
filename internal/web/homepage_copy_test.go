package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestHomepageUsesConciseTelemetryCopy(t *testing.T) {
	h, err := (Server{
		Pools: []model.Pool{{ID: "test", Name: "Test Pool"}},
		Load:  func() ([]model.Observation, error) { return nil, nil },
		Demo:  true,
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	for _, want := range []string{
		"Bitcoin pool performance.",
		"Preview only — these numbers are examples, not live results.",
		"/static/dashboard.js",
		"latest 24 hours",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("homepage does not contain %q", want)
		}
	}
	for _, unwanted := range []string{"Pools monitored", "Bitcoin blocks observed", "Pools with recent latency data", "Worker payout alerts", "data-live-summary"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("homepage still contains removed summary metric %q", unwanted)
		}
	}
	for _, unwanted := range []string{
		"/api/v1/",
		"Independent pool telemetry",
		"Fair by construction",
		"No composite score",
		"Trust pool",
		"Direct coinbase",
		"Payout custody",
		"Confidence",
		"Leaderboard",
		"Current Leader",
		"Wins",
		"Races",
		"Template observations",
		"Wilson",
	} {
		if strings.Contains(body, unwanted) {
			t.Errorf("homepage still contains old copy %q", unwanted)
		}
	}
	methodology := httptest.NewRecorder()
	h.ServeHTTP(methodology, httptest.NewRequest("GET", "/methodology", nil))
	for _, want := range []string{"How the measurements work.", "id=\"block-template-latency\"", "id=\"pplns\"", "id=\"coinbase-payout\"", "Pay Per Last N Shares", "id=\"score-calculation\"", "The scoring math", "mining_loss =", "base =", "increase_penalty =", "high_fee_penalty =", "tls_certificate_penalty =", "score = round", "score = 0 for a solo pool", "straight-line interpolation", "Component curves"} {
		if !strings.Contains(methodology.Body.String(), want) {
			t.Errorf("methodology glossary missing %q", want)
		}
	}
	for _, unwanted := range []string{"fresh Bitcoin address", "worker name", "miner agent", "probe IP", "rotation behavior"} {
		if strings.Contains(methodology.Body.String(), unwanted) {
			t.Errorf("methodology exposes operational detail %q", unwanted)
		}
	}

	script := httptest.NewRecorder()
	h.ServeHTTP(script, httptest.NewRequest("GET", "/static/dashboard.js", nil))
	if script.Code != 200 {
		t.Fatalf("dashboard updater status=%d", script.Code)
	}
	for _, want := range []string{"fetch(window.location.pathname + window.location.search", "data-pool-id", "row-updated", "sortStates", "applySort", "placeRows", "captureViewport", "restoreViewport", "stratumstats.selectedVantage", "stratumstats.selectedTransport", "localStorage", "restoreSelections", "window.location.replace", "data-relative-time", "relativeAge", "toLocaleString", `clone.querySelectorAll("time[data-relative-time]")`} {
		if !strings.Contains(script.Body.String(), want) {
			t.Errorf("dashboard updater missing %q", want)
		}
	}
	if strings.Contains(script.Body.String(), "location.reload") {
		t.Fatal("dashboard updater performs a full page reload")
	}
	for _, unwanted := range []string{"rows.forEach((row) => list.append(row))", "if (footnote && nextFootnote) footnote.innerHTML"} {
		if strings.Contains(script.Body.String(), unwanted) {
			t.Errorf("dashboard updater still contains scroll-disrupting refresh logic %q", unwanted)
		}
	}

	stylesheet := httptest.NewRecorder()
	h.ServeHTTP(stylesheet, httptest.NewRequest("GET", "/static/style.css", nil))
	if !strings.Contains(stylesheet.Body.String(), ".sort-button[data-sort-key=\"loss\"],.sort-button[data-sort-key=\"p95\"]{white-space:nowrap}") {
		t.Fatal("estimated mining loss or P95 sort header is allowed to wrap")
	}
	for _, want := range []string{
		"/* Phone layouts keep every measurement visible without a desktop-width scroller. */",
		".measurement-row{display:grid;grid-template-columns:minmax(0,1fr) minmax(0,1fr);min-width:0",
		".measurement-list{display:block;min-width:0;overflow:visible",
		"header nav{display:grid;width:100%;grid-template-columns:repeat(3,minmax(0,1fr))",
		".score-curve-table,.score-curve-table tbody,.score-curve-table tr,.score-curve-table td{display:block",
	} {
		if !strings.Contains(stylesheet.Body.String(), want) {
			t.Errorf("stylesheet is missing mobile layout rule %q", want)
		}
	}
}

func TestDonationBannerAppearsOnEveryPublicPage(t *testing.T) {
	h, err := (Server{
		Pools: []model.Pool{{ID: "test", Name: "Test Pool"}},
		Load:  func() ([]model.Observation, error) { return nil, nil },
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/", "/methodology"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			for _, unwanted := range []string{"/api/v1/"} {
				if strings.Contains(w.Body.String(), unwanted) {
					t.Errorf("public page still links to %q", unwanted)
				}
			}
			for _, want := range []string{
				"donation-banner",
				"This site is run by donations.",
				"Powered by M45Core",
				"Donate Bitcoin",
				`href="bitcoin:`,
				`target="_blank" rel="noopener noreferrer"`,
			} {
				if !strings.Contains(w.Body.String(), want) {
					t.Errorf("%s donation banner missing %q", path, want)
				}
			}
		})
	}
}
