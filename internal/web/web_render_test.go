package web

import (
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestDashboardViewModelCarriesFractionalMetricsAndRegionUpdateTime(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	measurementAt := now.Add(-30 * time.Second)
	pool := model.Pool{ID: "fractional", Name: "Fractional", Category: "shared"}
	started := measurementAt.Add(-time.Minute)
	observations := []model.Observation{
		{Source: model.SourceRemoteScheduled, RunID: "run", ObservationID: "run/block", ObservedAt: measurementAt, Vantage: "us-east", BlockID: "one", BlockHeight: 900_000, PoolID: pool.ID, Eligible: true, Arrived: true, OffsetMS: 12.75},
		{Source: model.SourceRemoteScheduled, RunID: "run", ObservationID: "run/summary", RecordType: model.RecordTypeProbeRun, ObservedAt: now, Vantage: "us-east", RunStartedAt: &started, RunStatus: "ok"},
	}
	h, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	payload := dashboardPayload(t, h, "/dashboard-data?vantage=us-east")
	if len(payload.OtherPools) != 1 {
		t.Fatalf("other=%+v", payload.OtherPools)
	}
	got := payload.OtherPools[0]
	if got.MedianMS == nil || *got.MedianMS != 12.8 || got.SortName != "Fractional " || got.RowID == "" {
		t.Fatalf("pool=%+v", got)
	}
	if payload.DataUpdatedAt == nil || !payload.DataUpdatedAt.Equal(now) {
		t.Fatalf("region update=%v, want %v", payload.DataUpdatedAt, now)
	}
	if payload.Snapshot.LatestBlockHeight != 900_000 {
		t.Fatalf("latest block height=%d, want 900000", payload.Snapshot.LatestBlockHeight)
	}
}
