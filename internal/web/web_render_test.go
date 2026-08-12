package web

import (
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestDashboardViewModelCarriesFractionalMetricsAndUpdateTimes(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute).Truncate(time.Second)
	pool := model.Pool{ID: "fractional", Name: "Fractional", Category: "shared"}
	observations := []model.Observation{{ObservedAt: now, Vantage: "unknown", BlockID: "one", PoolID: pool.ID, Eligible: true, Arrived: true, OffsetMS: 12.75}}
	h, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	payload := dashboardPayload(t, h, "/dashboard-data?vantage=unknown")
	if len(payload.OtherPools) != 1 {
		t.Fatalf("other=%+v", payload.OtherPools)
	}
	got := payload.OtherPools[0]
	if got.MedianMS == nil || *got.MedianMS != 12.8 || got.SortName != "Fractional " || got.RowID == "" {
		t.Fatalf("pool=%+v", got)
	}
	if payload.DataUpdatedAt == nil || !payload.DataUpdatedAt.Equal(now) || payload.OtherPoolsUpdatedAt == nil || !payload.OtherPoolsUpdatedAt.Equal(now) {
		t.Fatalf("updates=%v %v", payload.DataUpdatedAt, payload.OtherPoolsUpdatedAt)
	}
}
