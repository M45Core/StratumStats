package web

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/proofofmike/stratumstats/internal/model"
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
		"Median block delay",
		"Free solo pools",
		"Paid solo pools",
		"Unverified solo pools",
		"PPLNS shared pools",
		"Other shared pools",
		"Bitcoin blocks observed",
		"Pools with block data",
		"Preview only — these numbers are examples, not live results.",
		"https://github.com/proofofmike/stratumstats",
		"/static/dashboard.js",
		"data-sort-key=\"fee\"",
		"data-sort-key=\"loss\"",
		"Est. mining loss",
		"Availability",
		"aria-label=\"Jump to pool group\"",
		"href=\"#free-solo-pools\"",
		"href=\"#unsafe-solo-pools\"",
		"PPLNS shared pools",
		"latest 24 hours",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("homepage does not contain %q", want)
		}
	}
	for _, unwanted := range []string{
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
	for _, want := range []string{"How the measurements work.", "id=\"block-template-latency\"", "id=\"pplns\"", "id=\"coinbase-payout\"", "Pay Per Last N Shares"} {
		if !strings.Contains(methodology.Body.String(), want) {
			t.Errorf("methodology glossary missing %q", want)
		}
	}

	script := httptest.NewRecorder()
	h.ServeHTTP(script, httptest.NewRequest("GET", "/static/dashboard.js", nil))
	if script.Code != 200 {
		t.Fatalf("dashboard updater status=%d", script.Code)
	}
	for _, want := range []string{"fetch(window.location.pathname + window.location.search", "data-pool-id", "row-updated", "sortStates", "applySort", "placeRows", "captureViewport", "restoreViewport"} {
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
}

func TestDonationBannerAppearsOnEveryPublicPage(t *testing.T) {
	h, err := (Server{
		Pools: []model.Pool{{ID: "test", Name: "Test Pool"}},
		Load:  func() ([]model.Observation, error) { return nil, nil },
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{"/", "/pools", "/methodology"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != 200 {
				t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
			}
			for _, want := range []string{
				"donation-banner",
				"This site is run by donations.",
				"Donate Bitcoin",
				"bitcoin:3B86bWqfjdQeLEr8nkeeWU6ygksc2K7MoL?label=StratumStats%20donation",
				"3B86bWqfjdQeLEr8nkeeWU6ygksc2K7MoL",
			} {
				if !strings.Contains(w.Body.String(), want) {
					t.Errorf("%s donation banner missing %q", path, want)
				}
			}
		})
	}
}
