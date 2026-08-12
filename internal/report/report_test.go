package report

import (
	"math"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestComputeReportsAvailabilityAndLatency(t *testing.T) {
	pools := []model.Pool{{ID: "fast", Name: "Fast"}, {ID: "slow", Name: "Slow"}}
	var obs []model.Observation
	for i := 0; i < 40; i++ {
		block := string(rune('a' + i))
		obs = append(obs, model.Observation{PoolID: "fast", BlockID: block, Eligible: true, Arrived: true, OffsetMS: 20, TLS: true})
		obs = append(obs, model.Observation{PoolID: "slow", BlockID: block, Eligible: true, Arrived: i < 30, OffsetMS: 900})
	}
	s := Compute(pools, obs, time.Time{})
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
	if s.Reports[0].EstimatedMiningLossPct == nil || *s.Reports[0].EstimatedMiningLossPct != 0.0033 || s.Reports[1].EstimatedMiningLossPct == nil || *s.Reports[1].EstimatedMiningLossPct != 25.1125 {
		t.Fatalf("estimated mining loss = %v/%v, want 0.0033/25.1125", s.Reports[0].EstimatedMiningLossPct, s.Reports[1].EstimatedMiningLossPct)
	}

	if s.Reports[0].Blocks != 40 {
		t.Fatalf("eligible blocks = %d", s.Reports[0].Blocks)
	}
	if s.BlocksObserved != 40 || s.EligibleEndpointSamples != 80 || s.TemplateDeliveries != 70 {
		t.Fatalf("snapshot counts = blocks:%d eligible:%d delivered:%d, want 40/80/70", s.BlocksObserved, s.EligibleEndpointSamples, s.TemplateDeliveries)
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
	snapshot := Compute([]model.Pool{pool}, observations, time.Time{})
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
	if snapshot.BlocksObserved != 4 || snapshot.EligibleEndpointSamples != 3 || snapshot.TemplateDeliveries != 2 {
		t.Fatalf("snapshot counts = blocks:%d eligible:%d delivered:%d, want 4/3/2", snapshot.BlocksObserved, snapshot.EligibleEndpointSamples, snapshot.TemplateDeliveries)
	}
}

func TestComputePublishesLatestEligibleBlockID(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	pool := model.Pool{ID: "pool", Name: "Pool"}
	observations := []model.Observation{
		{PoolID: pool.ID, Vantage: "west", BlockID: "older-block", ObservedAt: now.Add(-time.Minute), Eligible: true, Arrived: true},
		{PoolID: pool.ID, Vantage: "west", BlockID: "latest-block", BlockHeight: 900_000, ObservedAt: now, Eligible: true, Arrived: true},
		{PoolID: pool.ID, Vantage: "west", BlockID: "ineligible-block", ObservedAt: now.Add(time.Minute)},
		{PoolID: "removed", Vantage: "west", BlockID: "unknown-pool-block", ObservedAt: now.Add(2 * time.Minute), Eligible: true},
	}

	snapshot := Compute([]model.Pool{pool}, observations, now.Add(3*time.Minute))
	if snapshot.LatestBlockID != "latest-block" {
		t.Fatalf("latest block ID=%q, want latest-block", snapshot.LatestBlockID)
	}
	if snapshot.LatestBlockHeight != 900_000 {
		t.Fatalf("latest block height=%d, want 900000", snapshot.LatestBlockHeight)
	}
}

func TestComputeDeduplicatesRetriedObservationIDs(t *testing.T) {
	pools := []model.Pool{{ID: "pool", Name: "Pool"}}
	duration := 12.0
	record := model.Observation{Version: model.ObservationVersion, ObservationID: "run/1", RecordType: model.RecordTypeProtocol, PoolID: "pool", ProtocolMethod: model.ProtocolConnect, ResponseStatus: model.ProtocolStatusOK, DurationMS: &duration}
	snapshot := Compute(pools, []model.Observation{record, record}, time.Time{})
	if got := snapshot.Reports[0].ConnectTiming.Attempts; got != 1 {
		t.Fatalf("attempts=%d, want 1", got)
	}
}

func TestComputeScoresOnlyCompletedLosslessScheduledRuns(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Minute)
	pools := []model.Pool{{ID: "fast", Name: "Fast"}, {ID: "slow", Name: "Slow"}}
	block := func(runID, blockID, poolID string, arrived bool) model.Observation {
		return model.Observation{
			Version: model.ObservationVersion, Source: model.SourceRemoteScheduled,
			ObservationID: runID + "/" + blockID + "/" + poolID, RunID: runID,
			Vantage: "us-west", BlockID: blockID, PoolID: poolID,
			ObservedAt: now.Add(-30 * time.Second), Eligible: true, Arrived: arrived,
		}
	}
	run := func(runID, status string, dropped int) model.Observation {
		return model.Observation{
			Version: model.ObservationVersion, Source: model.SourceRemoteScheduled,
			ObservationID: runID + "/summary", RunID: runID, Vantage: "us-west",
			RecordType: model.RecordTypeProbeRun, ObservedAt: now, RunStartedAt: &started,
			RunStatus: status, DroppedObservations: dropped,
		}
	}
	observations := []model.Observation{
		block("complete", "baseline", "fast", true),
		block("complete", "baseline", "slow", true),
		block("complete", "accepted", "fast", true),
		block("complete", "accepted", "slow", false),
		run("complete", "ok", 0),
		block("partial", "partial", "fast", true),
		block("partial", "partial", "slow", false),
		run("partial", "partial", 1),
		block("dropped", "dropped", "fast", true),
		block("dropped", "dropped", "slow", false),
		run("dropped", "ok", 1),
		block("interrupted", "interrupted", "fast", true),
		block("interrupted", "interrupted", "slow", false),
	}

	snapshot := Compute(pools, observations, now)
	if snapshot.BlocksObserved != 2 || snapshot.EligibleEndpointSamples != 4 || snapshot.TemplateDeliveries != 3 {
		t.Fatalf("snapshot counts = blocks:%d eligible:%d delivered:%d, want 2/4/3", snapshot.BlocksObserved, snapshot.EligibleEndpointSamples, snapshot.TemplateDeliveries)
	}
	if snapshot.Reports[0].Blocks != 2 || snapshot.Reports[0].Availability != 100 {
		t.Fatalf("fast report included an incomplete run: %+v", snapshot.Reports[0])
	}
	if snapshot.Reports[1].Blocks != 2 || snapshot.Reports[1].Availability != 50 {
		t.Fatalf("slow report lost the completed run's real miss: %+v", snapshot.Reports[1])
	}
	if snapshot.Reports[0].OverallScore == nil || snapshot.Reports[1].OverallScore == nil || *snapshot.Reports[0].OverallScore <= *snapshot.Reports[1].OverallScore {
		t.Fatalf("completed availability miss was not reflected in scores: fast=%v slow=%v", snapshot.Reports[0].OverallScore, snapshot.Reports[1].OverallScore)
	}
}

func TestComputeExcludesBroadRegionalMissesButKeepsLocalizedOutages(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	started := now.Add(-time.Minute)
	pools := make([]model.Pool, 0, 8)
	for index := 0; index < 8; index++ {
		poolID := "pool-" + string(rune('a'+index))
		endpoints := []model.Endpoint{{Host: poolID + ".example", Port: 3333}}
		if index == 0 {
			endpoints = append(endpoints,
				model.Endpoint{Host: poolID + "-two.example", Port: 3333},
				model.Endpoint{Host: poolID + "-three.example", Port: 3333},
			)
		}
		pools = append(pools, model.Pool{ID: poolID, Name: poolID, Endpoints: endpoints})
	}
	var observations []model.Observation
	addCohort := func(runID, blockID string, misses map[string]bool) {
		observedAt := now.Add(-30 * time.Second)
		for _, pool := range pools {
			for _, endpoint := range pool.Endpoints {
				address := endpointAddress(endpoint)
				key := endpointReportKey(pool.ID, address, endpoint.TLS)
				observations = append(observations, model.Observation{
					Version: model.ObservationVersion, Source: model.SourceRemoteScheduled,
					ObservationID: runID + "/" + key, RunID: runID, Vantage: "us-east",
					BlockID: blockID, PoolID: pool.ID, Endpoint: address, ObservedAt: observedAt,
					Eligible: true, Arrived: !misses[key], TLS: endpoint.TLS,
				})
			}
		}
		observations = append(observations, model.Observation{
			Version: model.ObservationVersion, Source: model.SourceRemoteScheduled,
			ObservationID: runID + "/summary", RunID: runID, Vantage: "us-east",
			RecordType: model.RecordTypeProbeRun, ObservedAt: now, RunStartedAt: &started,
			RunStatus: "ok", DroppedObservations: 0,
		})
	}
	addCohort("healthy", "healthy", nil)
	broadMisses := make(map[string]bool)
	for index := 0; index < 5; index++ {
		pool := pools[index]
		broadMisses[endpointReportKey(pool.ID, endpointAddress(pool.Endpoints[0]), false)] = true
	}
	addCohort("regional-failure", "regional-failure", broadMisses)
	localizedMisses := make(map[string]bool)
	for _, endpoint := range pools[0].Endpoints {
		localizedMisses[endpointReportKey(pools[0].ID, endpointAddress(endpoint), false)] = true
	}
	addCohort("pool-outage", "pool-outage", localizedMisses)

	snapshot := Compute(pools, observations, now)
	if snapshot.ExcludedRegionalCohorts != 1 {
		t.Fatalf("excluded regional cohorts=%d, want 1", snapshot.ExcludedRegionalCohorts)
	}
	if snapshot.BlocksObserved != 2 || snapshot.EligibleEndpointSamples != 20 || snapshot.TemplateDeliveries != 17 {
		t.Fatalf("included counts = blocks:%d eligible:%d delivered:%d, want 2/20/17", snapshot.BlocksObserved, snapshot.EligibleEndpointSamples, snapshot.TemplateDeliveries)
	}
	for _, report := range snapshot.Reports {
		if report.Blocks != 2 {
			t.Fatalf("%s blocks=%d, broad regional cohort affected scoring", report.Endpoint, report.Blocks)
		}
		if report.PoolID == pools[0].ID && report.Availability != 50 {
			t.Fatalf("localized outage availability=%.1f, want 50.0", report.Availability)
		}
		if report.PoolID != pools[0].ID && report.Availability != 100 {
			t.Fatalf("healthy endpoint %s availability=%.1f, want 100.0", report.Endpoint, report.Availability)
		}
		if report.OverallScore == nil {
			t.Fatalf("%s has no score after healthy cohorts", report.Endpoint)
		}
	}
	if *snapshot.Reports[0].OverallScore >= *snapshot.Reports[3].OverallScore {
		t.Fatalf("localized outage did not lower score: affected=%v healthy=%v", snapshot.Reports[0].OverallScore, snapshot.Reports[3].OverallScore)
	}
}

func TestComputeVantageFiltersTimingButRetainsGlobalCoinbaseEvidence(t *testing.T) {
	fee := 1.25
	pools := []model.Pool{{ID: "pool", Name: "Pool", Category: "solo"}}
	observations := []model.Observation{
		{PoolID: "pool", Vantage: "us-west", BlockID: "west", ObservedAt: time.Unix(1, 0), Eligible: true, Arrived: true, OffsetMS: 10},
		{PoolID: "pool", Vantage: "us-east", BlockID: "east", ObservedAt: time.Unix(2, 0), Eligible: true, Arrived: true, OffsetMS: 90, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee},
	}
	snapshot := ComputeVantage(pools, observations, "us-west", time.Unix(3, 0))
	got := snapshot.Reports[0]
	if got.Blocks != 1 || got.MedianMS == nil || *got.MedianMS != 10 {
		t.Fatalf("regional report=%+v", got)
	}
	if got.WorkerAddressStatus != "always_observed" || got.LatestPoolFeePct == nil || *got.LatestPoolFeePct != fee {
		t.Fatalf("global evidence not retained: %+v", got)
	}
}

func TestComputeVantagesAggregatesRegionalLatencyPerBlock(t *testing.T) {
	now := time.Date(2026, 8, 11, 12, 0, 0, 0, time.UTC)
	pool := model.Pool{ID: "pool", Name: "Pool"}
	block := func(vantage, blockID string, at time.Time, arrived bool, offset float64) model.Observation {
		return model.Observation{
			PoolID: pool.ID, Vantage: vantage, BlockID: blockID, ObservedAt: at,
			Eligible: true, Arrived: arrived, OffsetMS: offset,
		}
	}
	observations := []model.Observation{
		block("us-west", "one", now.Add(-3*time.Hour), true, 10),
		block("us-central", "one", now.Add(-3*time.Hour+time.Millisecond), true, 100),
		block("us-east", "one", now.Add(-3*time.Hour+2*time.Millisecond), true, 1000),
		block("us-west", "two", now.Add(-2*time.Hour), true, 20),
		block("us-central", "two", now.Add(-2*time.Hour+time.Millisecond), true, 200),
		block("us-east", "two", now.Add(-2*time.Hour+2*time.Millisecond), true, 2000),
		block("us-west", "three", now.Add(-time.Hour), true, 30),
		block("us-central", "three", now.Add(-time.Hour+time.Millisecond), false, 0),
	}
	vantages := map[string]bool{"us-west": true, "us-central": true, "us-east": true}

	combined := ComputeVantages([]model.Pool{pool}, observations, vantages, now).Reports[0]
	if combined.Blocks != 3 || combined.Arrivals != 3 || combined.EligibleChecks != 8 || combined.DeliveryChecks != 7 {
		t.Fatalf("combined blocks=%d arrivals=%d eligible checks=%d delivery checks=%d, want 3/3/8/7", combined.Blocks, combined.Arrivals, combined.EligibleChecks, combined.DeliveryChecks)
	}
	if combined.Availability != 88.9 {
		t.Fatalf("combined availability=%.1f, want equally weighted regional rate 88.9", combined.Availability)
	}
	if combined.MedianMS == nil || *combined.MedianMS != 100 || combined.P95MS == nil || *combined.P95MS != 200 {
		t.Fatalf("combined median/p95=%v/%v, want 100/200 from per-block medians", combined.MedianMS, combined.P95MS)
	}
	if got := combined.TemplateLatencyHistory; len(got) != 3 || got[0].Value != 100 || got[1].Value != 200 || got[2].Value != 30 {
		t.Fatalf("combined history=%+v, want one median point per block", got)
	}

	west := ComputeVantage([]model.Pool{pool}, observations, "us-west", now).Reports[0]
	if west.Blocks != 3 || west.EligibleChecks != 3 || west.MedianMS == nil || *west.MedianMS != 20 || len(west.TemplateLatencyHistory) != 3 {
		t.Fatalf("individual vantage changed: %+v", west)
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
	snapshot := Compute(pools, observations, started.Add(time.Hour))
	if snapshot.BlocksObserved != 1 || snapshot.EligibleEndpointSamples != 2 || snapshot.TemplateDeliveries != 2 {
		t.Fatalf("snapshot counts = blocks:%d eligible:%d delivered:%d, want 1/2/2", snapshot.BlocksObserved, snapshot.EligibleEndpointSamples, snapshot.TemplateDeliveries)
	}
	if snapshot.Reports[0].MedianMS == nil || *snapshot.Reports[0].MedianMS != 0 {
		t.Fatalf("first median = %v, want 0", snapshot.Reports[0].MedianMS)
	}
	if snapshot.Reports[1].MedianMS == nil || *snapshot.Reports[1].MedianMS != 100 {
		t.Fatalf("late median = %v, want 100; reopened zero must be ignored", snapshot.Reports[1].MedianMS)
	}
}

func TestComputeLatencyUsesRolling24Hours(t *testing.T) {
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	fee := 1.25
	oldDuration, recentDuration, futureDuration := 1_000.0, 20.0, 9_000.0
	observations := []model.Observation{
		{PoolID: "pool", Vantage: "west", BlockID: "old", ObservedAt: now.Add(-24*time.Hour - time.Minute), Eligible: true, Arrived: true, OffsetMS: 5_000, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee},
		{PoolID: "pool", Vantage: "west", BlockID: "boundary", ObservedAt: now.Add(-24 * time.Hour), Eligible: true, Arrived: true, OffsetMS: 30},
		{PoolID: "pool", Vantage: "west", BlockID: "recent-one", ObservedAt: now.Add(-2 * time.Hour), Eligible: true, Arrived: true, OffsetMS: 50},
		{PoolID: "pool", Vantage: "west", BlockID: "recent-two", ObservedAt: now.Add(-time.Hour), Eligible: true, Arrived: true, OffsetMS: 70},
		{PoolID: "pool", Vantage: "west", BlockID: "future", ObservedAt: now.Add(time.Second), Eligible: true, Arrived: true, OffsetMS: 9_000},
		{PoolID: "pool", RecordType: model.RecordTypeProtocol, ProtocolMethod: model.ProtocolConnect, ResponseStatus: model.ProtocolStatusOK, DurationMS: &oldDuration, ObservedAt: now.Add(-24*time.Hour - time.Minute)},
		{PoolID: "pool", RecordType: model.RecordTypeProtocol, ProtocolMethod: model.ProtocolConnect, ResponseStatus: model.ProtocolStatusOK, DurationMS: &recentDuration, ObservedAt: now.Add(-time.Hour)},
		{PoolID: "pool", RecordType: model.RecordTypeProtocol, ProtocolMethod: model.ProtocolConnect, ResponseStatus: model.ProtocolStatusOK, DurationMS: &futureDuration, ObservedAt: now.Add(time.Second)},
	}

	snapshot := Compute([]model.Pool{{ID: "pool", Name: "Pool", Category: "solo"}}, observations, now)
	if snapshot.LatencyWindowHours != 24 {
		t.Fatalf("latency window=%d hours, want 24", snapshot.LatencyWindowHours)
	}
	got := snapshot.Reports[0]
	if got.MedianMS == nil || *got.MedianMS != 50 || got.P95MS == nil || *got.P95MS != 70 {
		t.Fatalf("24-hour latency median/p95=%v/%v, want 50/70", got.MedianMS, got.P95MS)
	}
	if got.EstimatedMiningLossPct == nil || *got.EstimatedMiningLossPct != 0.0083 {
		t.Fatalf("estimated mining loss=%v, want 0.0083", got.EstimatedMiningLossPct)
	}
	if len(got.TemplateLatencyHistory) != 3 || got.TemplateLatencyHistory[0].Value != 30 || got.TemplateLatencyHistory[2].Value != 70 {
		t.Fatalf("24-hour latency history=%+v", got.TemplateLatencyHistory)
	}
	if got.ConnectTiming.Attempts != 1 || got.ConnectTiming.MedianMS == nil || *got.ConnectTiming.MedianMS != 20 {
		t.Fatalf("24-hour protocol timing=%+v", got.ConnectTiming)
	}
	if got.Blocks != 4 || got.Arrivals != 4 || got.Availability != 100 {
		t.Fatalf("30-day availability included future data: blocks=%d arrivals=%d availability=%.1f", got.Blocks, got.Arrivals, got.Availability)
	}
	if got.WorkerAddressStatus != "always_observed" || got.LatestPoolFeePct == nil || *got.LatestPoolFeePct != fee {
		t.Fatalf("older payout evidence was discarded: %+v", got)
	}

	stale := Compute([]model.Pool{{ID: "stale", Name: "Stale"}}, []model.Observation{{
		PoolID: "stale", BlockID: "old", ObservedAt: now.Add(-24*time.Hour - time.Nanosecond), Eligible: true, Arrived: true, OffsetMS: 10,
	}}, now).Reports[0]
	if stale.MedianMS != nil || stale.P95MS != nil || stale.EstimatedMiningLossPct != nil || len(stale.TemplateLatencyHistory) != 0 {
		t.Fatalf("stale-only latency was published: %+v", stale)
	}
}

func TestComputeDropsAllObservationsAfterThirtyDays(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	duration := 20.0
	fee := 1.0
	old := now.Add(-RetentionWindow - time.Nanosecond)
	boundary := now.Add(-RetentionWindow)
	observations := []model.Observation{
		{ObservationID: "old-block", ObservedAt: old, PoolID: "pool", BlockID: "old", Eligible: true, Arrived: true, OffsetMS: 10, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee},
		{ObservationID: "old-protocol", ObservedAt: old, PoolID: "pool", RecordType: model.RecordTypeProtocol, ProtocolMethod: model.ProtocolConnect, ResponseStatus: model.ProtocolStatusOK, DurationMS: &duration},
		{ObservationID: "boundary-block", ObservedAt: boundary, PoolID: "pool", BlockID: "boundary", Eligible: true, Arrived: true, OffsetMS: 20, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true, EstimatedPoolFeePct: &fee},
		{ObservationID: "future", ObservedAt: now.Add(time.Nanosecond), PoolID: "pool", BlockID: "future", Eligible: true, Arrived: true},
	}
	snapshot := Compute([]model.Pool{{ID: "pool", Name: "Pool", Category: "solo"}}, observations, now)
	got := snapshot.Reports[0]
	if snapshot.RetentionWindowDays != 30 || snapshot.BlocksObserved != 1 || got.Blocks != 1 || got.CoinbaseSamples != 1 {
		t.Fatalf("retained snapshot/report=%+v/%+v, want only 30-day boundary observation", snapshot, got)
	}
	if got.LastObservedAt == nil || !got.LastObservedAt.Equal(boundary) {
		t.Fatalf("last observed at=%v, want retained boundary %v", got.LastObservedAt, boundary)
	}
	if got.ConnectTiming.Attempts != 0 || got.MedianMS != nil {
		t.Fatalf("old protocol or 30-day latency leaked into 24-hour metrics: %+v", got)
	}
}

func TestReportsSortByPoolName(t *testing.T) {
	pools := []model.Pool{{ID: "new", Name: "Zulu"}, {ID: "known", Name: "Alpha"}}
	var obs []model.Observation
	obs = append(obs, model.Observation{PoolID: "new", BlockID: "one", Eligible: true, Arrived: true, TLS: true})
	for i := 0; i < 30; i++ {
		obs = append(obs, model.Observation{PoolID: "known", BlockID: string(rune(i + 1)), Eligible: true, Arrived: true, OffsetMS: 1000})
	}
	s := Compute(pools, obs, time.Time{})
	if s.Reports[0].PoolID != "known" {
		t.Fatalf("reports not sorted by pool name: %+v", s.Reports)
	}
}
