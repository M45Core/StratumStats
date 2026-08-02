package report

import (
	"math"
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestComputeReportsAvailabilityAndLatency(t *testing.T) {
	pools := []model.Pool{{ID: "fast", Name: "Fast"}, {ID: "slow", Name: "Slow"}}
	var obs []model.Observation
	for i := 0; i < 40; i++ {
		block := string(rune('a' + i))
		obs = append(obs, model.Observation{PoolID: "fast", BlockID: block, Eligible: true, Arrived: true, OffsetMS: 20, TLS: true})
		obs = append(obs, model.Observation{PoolID: "slow", BlockID: block, Eligible: true, Arrived: i < 30, OffsetMS: 900})
	}
	s := Compute(pools, obs, time.Unix(0, 0))
	if len(s.Reports) != 2 || s.Reports[0].PoolID != "fast" {
		t.Fatalf("unexpected order: %+v", s.Reports)
	}
	if s.Reports[0].Availability <= s.Reports[1].Availability {
		t.Fatal("availability should reflect misses")
	}
	if s.Reports[0].Availability != 100 || s.Reports[1].Availability != 75 {
		t.Fatalf("availability = %.1f/%.1f, want observed rates 100.0/75.0", s.Reports[0].Availability, s.Reports[1].Availability)
	}
	if s.Reports[0].MedianMS == nil || s.Reports[1].MedianMS == nil || *s.Reports[0].MedianMS >= *s.Reports[1].MedianMS {
		t.Fatal("median latency not reported")
	}

	if s.Reports[0].Blocks != 40 {
		t.Fatalf("eligible blocks = %d", s.Reports[0].Blocks)
	}
	if s.BlocksObserved != 40 || s.EligiblePoolSamples != 80 || s.TemplateDeliveries != 70 {
		t.Fatalf("snapshot counts = blocks:%d eligible:%d delivered:%d, want 40/80/70", s.BlocksObserved, s.EligiblePoolSamples, s.TemplateDeliveries)
	}
}

func TestComputeDeduplicatesArrivalsByVantageAndBlock(t *testing.T) {
	pool := model.Pool{ID: "pool", Name: "Pool"}
	observations := []model.Observation{
		{PoolID: pool.ID, Vantage: "west", BlockID: "one", Eligible: true, Arrived: true, OffsetMS: 12},
		{PoolID: pool.ID, Vantage: "west", BlockID: "one", Eligible: true, Arrived: true, OffsetMS: 11},
		{PoolID: pool.ID, Vantage: "west", BlockID: "one", Eligible: true, Arrived: true, OffsetMS: 10},
		{PoolID: pool.ID, Vantage: "east", BlockID: "one", Eligible: true, Arrived: true, OffsetMS: 20},
		{PoolID: pool.ID, Vantage: "west", BlockID: "two", Eligible: true},
		{PoolID: pool.ID, Vantage: "west", BlockID: "three"},
		{PoolID: "removed-pool", Vantage: "west", BlockID: "four", Eligible: true, Arrived: true, OffsetMS: 5},
	}
	snapshot := Compute([]model.Pool{pool}, observations, time.Now())
	report := snapshot.Reports[0]
	if report.Blocks != 3 || report.Arrivals != 2 {
		t.Fatalf("templates=%d/%d, want 2/3", report.Arrivals, report.Blocks)
	}
	if math.IsNaN(report.Availability) || report.Availability < 0 || report.Availability > 100 {
		t.Fatalf("invalid availability %v", report.Availability)
	}
	if report.Availability != 66.7 {
		t.Fatalf("availability = %.1f, want observed rate 2/3 = 66.7", report.Availability)
	}
	if report.MedianMS == nil || *report.MedianMS != 10 {
		t.Fatalf("median=%v, want earliest deduplicated offsets [10,20]", report.MedianMS)
	}
	if snapshot.BlocksObserved != 4 || snapshot.EligiblePoolSamples != 3 || snapshot.TemplateDeliveries != 2 {
		t.Fatalf("snapshot counts = blocks:%d eligible:%d delivered:%d, want 4/3/2", snapshot.BlocksObserved, snapshot.EligiblePoolSamples, snapshot.TemplateDeliveries)
	}
}

func TestComputeDeduplicatesRetriedObservationIDs(t *testing.T) {
	pools := []model.Pool{{ID: "pool", Name: "Pool"}}
	duration := 12.0
	record := model.Observation{Version: model.ObservationVersion, ObservationID: "run/1", RecordType: model.RecordTypeProtocol, PoolID: "pool", ProtocolMethod: model.ProtocolConnect, ResponseStatus: model.ProtocolStatusOK, DurationMS: &duration}
	snapshot := Compute(pools, []model.Observation{record, record}, time.Now())
	if got := snapshot.Reports[0].ConnectTiming.Attempts; got != 1 {
		t.Fatalf("attempts=%d, want 1", got)
	}
}

func TestComputeVantageFiltersTimingButRetainsGlobalCoinbaseEvidence(t *testing.T) {
	fee := 1.25
	pools := []model.Pool{{ID: "pool", Name: "Pool"}}
	observations := []model.Observation{
		{PoolID: "pool", Vantage: "us-west", BlockID: "west", ObservedAt: time.Unix(1, 0), Eligible: true, Arrived: true, OffsetMS: 10},
		{PoolID: "pool", Vantage: "us-east", BlockID: "east", ObservedAt: time.Unix(2, 0), Eligible: true, Arrived: true, OffsetMS: 90, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee},
	}
	snapshot := ComputeVantage(pools, observations, "us-west", time.Now())
	got := snapshot.Reports[0]
	if got.Blocks != 1 || got.MedianMS == nil || *got.MedianMS != 10 {
		t.Fatalf("regional report=%+v", got)
	}
	if got.WorkerAddressStatus != "always_observed" || got.LatestPoolFeePct == nil || *got.LatestPoolFeePct != fee {
		t.Fatalf("global evidence not retained: %+v", got)
	}
}

func TestComputeIgnoresReopenedWindowsForSameBlock(t *testing.T) {
	pools := []model.Pool{{ID: "first", Name: "First"}, {ID: "late", Name: "Late"}}
	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	observations := []model.Observation{
		{PoolID: "first", Vantage: "west", BlockID: "block", ObservedAt: started, Eligible: true, Arrived: true, OffsetMS: 0},
		{PoolID: "late", Vantage: "west", BlockID: "block", ObservedAt: started, Eligible: true, Arrived: true, OffsetMS: 100},
		{PoolID: "first", Vantage: "west", BlockID: "block", ObservedAt: started.Add(20 * time.Second), Eligible: true, Arrived: true, OffsetMS: 200},
		{PoolID: "late", Vantage: "west", BlockID: "block", ObservedAt: started.Add(20 * time.Second), Eligible: true, Arrived: true, OffsetMS: 0},
	}
	snapshot := Compute(pools, observations, time.Now())
	if snapshot.BlocksObserved != 1 || snapshot.EligiblePoolSamples != 2 || snapshot.TemplateDeliveries != 2 {
		t.Fatalf("snapshot counts = blocks:%d eligible:%d delivered:%d, want 1/2/2", snapshot.BlocksObserved, snapshot.EligiblePoolSamples, snapshot.TemplateDeliveries)
	}
	if snapshot.Reports[0].MedianMS == nil || *snapshot.Reports[0].MedianMS != 0 {
		t.Fatalf("first median = %v, want 0", snapshot.Reports[0].MedianMS)
	}
	if snapshot.Reports[1].MedianMS == nil || *snapshot.Reports[1].MedianMS != 100 {
		t.Fatalf("late median = %v, want 100; reopened zero must be ignored", snapshot.Reports[1].MedianMS)
	}
}

func TestReportsSortByPoolName(t *testing.T) {
	pools := []model.Pool{{ID: "new", Name: "Zulu"}, {ID: "known", Name: "Alpha"}}
	var obs []model.Observation
	obs = append(obs, model.Observation{PoolID: "new", BlockID: "one", Eligible: true, Arrived: true, TLS: true})
	for i := 0; i < 30; i++ {
		obs = append(obs, model.Observation{PoolID: "known", BlockID: string(rune(i + 1)), Eligible: true, Arrived: true, OffsetMS: 1000})
	}
	s := Compute(pools, obs, time.Now())
	if s.Reports[0].PoolID != "known" {
		t.Fatalf("reports not sorted by pool name: %+v", s.Reports)
	}
}
