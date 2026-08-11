package web

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestDashboardFormatsLatencyValues(t *testing.T) {
	pool := model.Pool{ID: "measured", Name: "Measured Pool"}
	perfect := model.Pool{ID: "perfect", Name: "Perfect Pool"}
	observations := []model.Observation{
		{Version: 1, ObservedAt: time.Now(), Vantage: "test", BlockID: "sample", PoolID: pool.ID, Eligible: true, Arrived: true, OffsetMS: 12.34},
		{Version: 1, ObservedAt: time.Now(), Vantage: "test", BlockID: "missed", PoolID: pool.ID, Eligible: true},
		{Version: 1, ObservedAt: time.Now(), Vantage: "test", BlockID: "perfect", PoolID: perfect.ID, Eligible: true, Arrived: true, OffsetMS: 10},
	}
	h, err := (Server{Pools: []model.Pool{pool, perfect}, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Fatalf("status=%d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "12 ms") {
		t.Fatalf("latency not formatted: %s", w.Body.String())
	}
	measuredRow := renderedPoolRow(t, w.Body.String(), pool.ID)
	if !strings.Contains(measuredRow, `<div class="availability-compact"><strong>50.0%</strong></div>`) {
		t.Fatalf("availability is not shown as a plain percentage: %s", measuredRow)
	}
	perfectRow := renderedPoolRow(t, w.Body.String(), perfect.ID)
	if !strings.Contains(perfectRow, `<div class="availability-compact"><strong>100.0%</strong></div>`) {
		t.Fatalf("perfect availability percentage is hidden: %s", perfectRow)
	}
	if strings.Contains(measuredRow, `<div class="availability-compact"><progress`) || strings.Contains(perfectRow, `<div class="availability-compact"><progress`) {
		t.Fatal("availability still renders a bar")
	}
	for _, want := range []string{"score-popover", "score-grade-", "Top score factors", "Availability", "40% weight", "Mining loss", "25% weight", "P95 delay", "20% weight", "50.00%", "mining-loss-bar-track", "mining-loss-bar loss-10"} {
		if !strings.Contains(measuredRow, want) {
			t.Errorf("score popover or availability-adjusted loss missing %q: %s", want, measuredRow)
		}
	}
}

func TestDashboardShowsGroupSpecificUpdateTags(t *testing.T) {
	observedAt := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	pools := []model.Pool{
		{ID: "pplns", Name: "PPLNS", Category: "shared", Products: []string{"PPLNS"}},
		{ID: "unchecked", Name: "Unchecked", Category: "shared"},
	}
	observations := []model.Observation{{
		ObservedAt: observedAt, PoolID: "pplns", Vantage: "unknown", BlockID: "000000000000000000000000000000000000000000000000000000000000abcd",
		Eligible: true, Arrived: true, OffsetMS: 10,
	}}
	handler, err := (Server{Pools: pools, Load: func() ([]model.Observation, error) { return observations, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	body := response.Body.String()
	for _, want := range []string{
		`class="section-update-tag"`,
		`Updated <time datetime="` + observedAt.Format(time.RFC3339) + `" data-relative-time`,
		observedAt.Format("02 Jan 15:04 UTC"),
		"No updates in 30 days",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard missing group update tag %q", want)
		}
	}
	footnoteStart := strings.Index(body, `<p class="dashboard-footnote"`)
	if footnoteStart < 0 {
		t.Fatal("dashboard footnote missing")
	}
	footnoteEnd := strings.Index(body[footnoteStart:], `</p>`)
	if footnoteEnd < 0 {
		t.Fatal("dashboard footnote missing")
	}
	footnote := body[footnoteStart : footnoteStart+footnoteEnd]
	if !strings.Contains(footnote, `Updated <time datetime="`+observedAt.Format(time.RFC3339)+`" data-relative-time`) {
		t.Fatalf("dashboard footnote does not use the latest observation time: %s", footnote)
	}
	for _, want := range []string{
		"Latest block",
		"0000000000…000000abcd",
		`title="000000000000000000000000000000000000000000000000000000000000abcd"`,
	} {
		if !strings.Contains(footnote, want) {
			t.Fatalf("dashboard footnote does not show latest block %q: %s", want, footnote)
		}
	}
}

func TestDashboardDisplaysWholeScoreButSortsByFractionalValue(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	pool := model.Pool{ID: "fractional", Name: "Fractional", Category: "shared"}
	observations := []model.Observation{
		{ObservedAt: now.Add(-time.Minute), PoolID: pool.ID, Vantage: "unknown", BlockID: "one", Eligible: true, Arrived: true, OffsetMS: 80},
		{ObservedAt: now, PoolID: pool.ID, Vantage: "unknown", BlockID: "two", Eligible: true, Arrived: true, OffsetMS: 500},
	}
	handler, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	row := renderedPoolRow(t, response.Body.String(), pool.ID)
	for _, want := range []string{`data-sort-score="97.647100"`, `aria-label="Overall performance score 98 out of 100"`, `>98</span>`} {
		if !strings.Contains(row, want) {
			t.Errorf("fractional score rendering missing %q: %s", want, row)
		}
	}
}

func TestMiningLossClassUsesLossScale(t *testing.T) {
	tests := []struct {
		value *float64
		want  string
	}{
		{nil, "loss-none"},
		{ptrFloat(0.1), "loss-1"},
		{ptrFloat(0.5), "loss-3"},
		{ptrFloat(1), "loss-5"},
		{ptrFloat(2.5), "loss-7"},
		{ptrFloat(5), "loss-9"},
		{ptrFloat(25), "loss-10"},
	}
	for _, test := range tests {
		if got := miningLossClass(test.value); got != test.want {
			t.Errorf("miningLossClass(%v)=%q, want %q", test.value, got, test.want)
		}
	}
}

func ptrFloat(value float64) *float64 { return &value }

func TestFormatMiningLossUsesPointOnePercentFloor(t *testing.T) {
	for _, value := range []float64{0, 0.0033, 0.0999} {
		if got := formatMiningLoss(value); got != "<0.1%" {
			t.Errorf("formatMiningLoss(%v)=%q, want <0.1%%", value, got)
		}
	}
	if got := formatMiningLoss(0.1); got != "0.10%" {
		t.Errorf("formatMiningLoss(0.1)=%q, want 0.10%%", got)
	}
}
