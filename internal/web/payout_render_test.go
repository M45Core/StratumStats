package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestDashboardSeparatesMissingAndPendingWorkerWalletEvidence(t *testing.T) {
	previousFee, fee, freeFee := 0.5, 0.75, 0.0
	now := time.Now()
	pools := []model.Pool{
		{ID: "present", Name: "Present Pool", Category: "solo"},
		{ID: "absent", Name: "Absent Pool", Category: "solo"},
		{ID: "unknown", Name: "Unknown Pool", Category: "solo"},
		{ID: "slower", Name: "Slower Pool", Category: "solo"},
		{ID: "free", Name: "Free Pool", Category: "solo"},
		{ID: "pplns", Name: "PPLNS Pool", Category: "shared", Products: []string{"FPPS", "PPLNS"}},
		{ID: "other", Name: "Other Pool", Category: "shared", Products: []string{"FPPS"}},
	}
	observations := []model.Observation{
		{Version: 1, ObservedAt: now.Add(-time.Hour), Vantage: "test", BlockID: "before", PoolID: "present", Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &previousFee},
		{Version: 1, ObservedAt: now, Vantage: "test", BlockID: "one", PoolID: "present", Eligible: true, Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee},
		{Version: 1, ObservedAt: now, Vantage: "test", BlockID: "two", PoolID: "absent", Eligible: true, Arrived: true, CoinbaseAnalyzed: true},
		{Version: 1, ObservedAt: now, Vantage: "test", BlockID: "three", PoolID: "slower", Eligible: true, Arrived: true, OffsetMS: 50, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee},
		{Version: 1, ObservedAt: now, Vantage: "test", BlockID: "four", PoolID: "free", Eligible: true, Arrived: true, OffsetMS: 25, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &freeFee},
		{Version: 1, ObservedAt: now, Vantage: "test", BlockID: "five", PoolID: "pplns", Eligible: true, Arrived: true, OffsetMS: 75},
		{Version: 1, ObservedAt: now, Vantage: "test", BlockID: "six", PoolID: "other", Eligible: true, Arrived: true, OffsetMS: 100},
	}
	h, err := (Server{Pools: pools, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	body := page.Body.String()
	for _, want := range []string{
		"Free solo pools",
		"Paid solo pools",
		"Worker wallet not found",
		"Pools without recent latency data",
		"worker wallet not found in 1 decoded coinbase payouts",
		"PPLNS shared pools",
		"Other shared pools",
		"Free Pool",
		"Present Pool",
		"Slower Pool",
		"Absent Pool",
		"Unknown Pool",
		"PPLNS Pool",
		"Other Pool",
		"Measured pool fee",
		"pool-info-link",
		"Click for more information",
		"More information about Free Pool",
		"0.75%",
		"&lt;0.1%",
		"changed 0.5% → 0.75%",
		"Recent fee increase",
		"−15.0 pts",
		"Score forced to 0",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing %q", want)
		}
	}
	for _, unwanted := range []string{"Trust pool", "Direct coinbase", "Payout custody", "Worker address observed", "Worker address absent", "1 change(s) · 2 checks", "1 of 1 checks"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("dashboard contains old label %q", unwanted)
		}
	}
	for _, hidden := range []string{`href="#pending-worker-wallet-pools" hidden`, `data-pool-section="wallet-pending" hidden`} {
		if !strings.Contains(body, hidden) {
			t.Errorf("empty pending section is not hidden: missing %q", hidden)
		}
	}
	freeSectionAt, normalAt, noDataAt, missingAt := strings.Index(body, "<h2>Free solo pools</h2>"), strings.Index(body, "<h2>Paid solo pools</h2>"), strings.Index(body, "<h2>Pools without recent latency data</h2>"), strings.Index(body, "<h2>Worker wallet not found</h2>")
	freePoolAt := strings.Index(body, "Free Pool")
	presentAt, slowerAt := strings.Index(body, "Present Pool"), strings.Index(body, "Slower Pool")
	absentAt := strings.Index(body, "Absent Pool")
	unknownAt := strings.Index(body, "Unknown Pool")
	if freeSectionAt < 0 || normalAt < 0 || noDataAt < 0 || missingAt < 0 || !(freeSectionAt < freePoolAt && freePoolAt < normalAt && normalAt < slowerAt && slowerAt < presentAt && presentAt < noDataAt && noDataAt < unknownAt && unknownAt < missingAt && missingAt < absentAt) {
		t.Fatalf("pools were not grouped by verification and sorted by overall score")
	}
	pplnsSectionAt, otherSectionAt := strings.Index(body, "<h2>PPLNS shared pools</h2>"), strings.Index(body, "<h2>Other shared pools</h2>")
	pplnsPoolAt, otherPoolAt := strings.Index(body, "PPLNS Pool"), strings.Index(body, "Other Pool")
	if !(pplnsSectionAt < pplnsPoolAt && pplnsPoolAt < otherSectionAt && otherSectionAt < otherPoolAt && otherPoolAt < noDataAt) {
		t.Fatalf("non-solo pools were not separated into PPLNS and Other sections")
	}
	absentRow := renderedPoolRow(t, body, "absent")
	for _, want := range []string{`data-sort-score="0.000000"`, "score-grade-0", "Score override", "Worker wallet not found", "Score forced to 0"} {
		if !strings.Contains(absentRow, want) {
			t.Errorf("missing-wallet score override missing %q: %s", want, absentRow)
		}
	}

	legacy := httptest.NewRecorder()
	h.ServeHTTP(legacy, httptest.NewRequest("GET", "/coinbase", nil))
	if legacy.Code != 301 || legacy.Header().Get("Location") != "/" {
		t.Fatalf("legacy coinbase route = %d %q, want 301 to /", legacy.Code, legacy.Header().Get("Location"))
	}

	api := httptest.NewRecorder()
	h.ServeHTTP(api, httptest.NewRequest("GET", "/api/v1/reports", nil))
	if !strings.Contains(api.Body.String(), `"worker_address_status":"always_observed"`) {
		t.Fatalf("neutral worker-address field missing from API: %s", api.Body.String())
	}
	if !strings.Contains(api.Body.String(), `"score_override_reason":"worker_wallet_not_found"`) {
		t.Fatalf("missing-wallet score override omitted from API: %s", api.Body.String())
	}
	for _, oldField := range []string{"payout_mode", "direct_coinbase_pct"} {
		if strings.Contains(api.Body.String(), oldField) {
			t.Errorf("API still contains old field %q", oldField)
		}
	}
}

func TestDashboardDoesNotClaimNonSoloFees(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	pools := []model.Pool{
		{ID: "shared", Name: "Shared Pool", Category: "shared", Products: []string{"PPLNS"}},
	}
	observations := []model.Observation{
		{Version: 1, ObservedAt: now, Vantage: "test", BlockID: "one", PoolID: "shared", Eligible: true, Arrived: true, OffsetMS: 40},
	}
	handler, err := (Server{Pools: pools, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	row := renderedPoolRow(t, page.Body.String(), "shared")
	for _, want := range []string{
		`data-sort-fee=""`,
		"<small>not measured</small>",
		"Latest coinbase payout",
	} {
		if !strings.Contains(row, want) {
			t.Errorf("shared-pool row missing %q", want)
		}
	}
	for _, unwanted := range []string{"Published fee", "published fee", "PPLNS 1%", "checked 2026", "Observed effective fee"} {
		if strings.Contains(row, unwanted) {
			t.Errorf("shared-pool row contains unmeasured fee claim %q", unwanted)
		}
	}
}

func TestDashboardShowsHighFeeScorePenalty(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	fee := 4.5
	pool := model.Pool{ID: "high-fee", Name: "High Fee Example", Category: "solo", Products: []string{"SOLO"}}
	observations := []model.Observation{{
		Version: model.ObservationVersion, ObservedAt: now, Vantage: "test", BlockID: "one", PoolID: pool.ID,
		Eligible: true, Arrived: true, OffsetMS: 50, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee,
	}}
	handler, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	row := renderedPoolRow(t, page.Body.String(), pool.ID)
	for _, want := range []string{"Fee above 2.5%", "4.5%", "−5.0 pts"} {
		if !strings.Contains(row, want) {
			t.Errorf("high-fee score detail missing %q: %s", want, row)
		}
	}

	reports := httptest.NewRecorder()
	handler.ServeHTTP(reports, httptest.NewRequest("GET", "/api/v1/reports", nil))
	if !strings.Contains(reports.Body.String(), `"high_fee_penalty":5`) {
		t.Fatalf("report API omitted high-fee penalty: %s", reports.Body.String())
	}
}

func renderedPoolRow(t *testing.T, body, poolID string) string {
	t.Helper()
	start := strings.Index(body, `data-pool-id="`+poolID+`"`)
	if start < 0 {
		t.Fatalf("pool row %q not found", poolID)
	}
	end := strings.Index(body[start:], "</article>")
	if end < 0 {
		t.Fatalf("pool row %q has no closing article", poolID)
	}
	return body[start : start+end+len("</article>")]
}

func TestDashboardRendersExpandablePayoutAndMetricHistory(t *testing.T) {
	previousFee, latestFee := 1.0, 1.25
	const workerAddress = "12ZEw5Hcv1hTb6YUQJ69y1V7uhcoDz92PH"
	const workerScript = "76a914111111111111111111111111111111111111111188ac"
	now := time.Now().UTC().Add(-time.Minute)
	pool := model.Pool{ID: "detail", Name: "Detail Pool", Category: "solo"}
	observations := []model.Observation{
		{
			Version: model.ObservationVersion, ObservedAt: now.Add(-time.Hour),
			Vantage: "test", BlockID: "before", PoolID: pool.ID,
			Eligible: true, Arrived: true, OffsetMS: 10,
			CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true,
			CoinbaseTotalSats: 10_000, WorkerPayoutSats: 9_900, EstimatedPoolFeePct: &previousFee,
		},
		{
			Version: model.ObservationVersion, ObservedAt: now,
			Vantage: "test", BlockID: "latest", PoolID: pool.ID,
			Eligible: true, Arrived: true, OffsetMS: 20,
			CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true,
			CoinbaseTotalSats: 10_000, WorkerPayoutSats: 9_875, EstimatedPoolFeePct: &latestFee,
			CoinbaseOutputCount: 3,
			CoinbaseOutputs: []model.CoinbaseOutput{
				{ValueSats: 9_875, ScriptPubKey: workerScript, Address: workerAddress, ScriptType: "p2pkh", Worker: true},
				{ValueSats: 125, ScriptPubKey: "0014751e76e8199196d454941c45d1b3a323f1433bd6", Address: "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4", ScriptType: "p2wpkh"},
			},
		},
	}
	handler, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}

	page := httptest.NewRecorder()
	handler.ServeHTTP(page, httptest.NewRequest("GET", "/", nil))
	body := page.Body.String()
	for _, want := range []string{
		"measurement-pool",
		"measurement-details",
		"More details",
		"details-toggle",
		"Show payment and recent performance details",
		"aria-controls=\"payout-history-detail\"",
		"Latest coinbase payout",
		"Other payment",
		"test miner address is kept private",
		"/methodology#non-worker-destination",
		"bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4",
		"wallet-explorer-link",
		"rel=\"noopener noreferrer\"",
		"1.25%",
		"Block-template latency",
		"latency-line-chart",
		"latency-chart-line",
		"Recent block-template latency for Detail Pool",
		"points=\"56.0,98.0 624.0,18.0\"",
		"lower is better",
		"tabindex=\"0\"",
		"Observed effective fee",
		"fee-change-list",
		"1%",
		"Only fee changes are shown",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expanded dashboard missing %q", want)
		}
	}
	for _, private := range []string{workerAddress, workerScript} {
		if strings.Contains(body, private) {
			t.Errorf("dashboard exposed private worker destination %q", private)
		}
	}
	if links := strings.Count(body, "class=\"wallet-explorer-link\""); links != 1 {
		t.Fatalf("wallet explorer links=%d, want one public non-worker address", links)
	}

	latencyAt := strings.Index(body, "<h3>Block-template latency</h3>")
	feeAt := strings.Index(body, "<h3>Observed effective fee</h3>")
	if latencyAt < 0 || feeAt <= latencyAt {
		t.Fatal("latency or fee history section not found")
	}
	latencyMarkup := body[latencyAt:feeAt]
	if strings.Contains(latencyMarkup, "<progress") || strings.Contains(latencyMarkup, "class=\"history-bars\"") {
		t.Fatal("template latency history still renders bars instead of a line graph")
	}
	feeMarkup := body[feeAt:]
	if feeEnd := strings.Index(feeMarkup, "</section>"); feeEnd >= 0 {
		feeMarkup = feeMarkup[:feeEnd]
	}
	if strings.Contains(feeMarkup, "<progress") || strings.Contains(feeMarkup, "fee-history-bars") {
		t.Fatal("observed effective fee still renders sample bars")
	}
	if changes := strings.Count(feeMarkup, "class=\"fee-change-values\""); changes != 1 {
		t.Fatalf("fee change rows=%d, want 1", changes)
	}

	poolCellAt := strings.Index(body, "<div class=\"measurement-pool\">")
	toggleAt := strings.Index(body, "class=\"details-toggle\"")
	poolCellEnd := -1
	if poolCellAt >= 0 {
		if relativeEnd := strings.Index(body[poolCellAt:], "</div>"); relativeEnd >= 0 {
			poolCellEnd = poolCellAt + relativeEnd
		}
	}
	if poolCellAt < 0 || toggleAt < poolCellAt || poolCellEnd < toggleAt || strings.Contains(body, "<summary") {
		t.Fatalf("payout/history control is not inline below the pool name")
	}

	api := httptest.NewRecorder()
	handler.ServeHTTP(api, httptest.NewRequest("GET", "/api/v1/reports", nil))
	for _, want := range []string{
		`"latest_payout_destinations"`,
		`"percentage":1.25`,
		`"template_latency_history"`,
		`"pool_fee_history"`,
	} {
		if !strings.Contains(api.Body.String(), want) {
			t.Errorf("report API missing %s: %s", want, api.Body.String())
		}
	}
	for _, private := range []string{workerAddress, workerScript, `"worker":true`} {
		if strings.Contains(api.Body.String(), private) {
			t.Errorf("report API exposed private worker destination %q: %s", private, api.Body.String())
		}
	}
}

func TestBuildFeeChangeHistoryOmitsStableSamples(t *testing.T) {
	started := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	history := []model.MetricHistoryPoint{
		{ObservedAt: started, Value: 1},
		{ObservedAt: started.Add(time.Minute), Value: 1},
		{ObservedAt: started.Add(2 * time.Minute), Value: 1.25},
		{ObservedAt: started.Add(3 * time.Minute), Value: 1.25},
		{ObservedAt: started.Add(4 * time.Minute), Value: 0.75},
	}
	got := buildFeeChangeHistory(history)
	if len(got) != 2 || got[0].Previous != 1 || got[0].Value != 1.25 || got[1].Previous != 1.25 || got[1].Value != 0.75 {
		t.Fatalf("fee change history=%+v", got)
	}
}
