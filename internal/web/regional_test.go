package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/ingest"
	"github.com/M45Core/StratumStats/internal/model"
)

func TestDashboardDataFiltersByVantageAndCombinesUS(t *testing.T) {
	pool := model.Pool{ID: "pool", Name: "Pool", Category: "shared"}
	now := time.Now().UTC().Add(-time.Minute)
	observations := []model.Observation{{PoolID: "pool", Vantage: "us-west", BlockID: "west", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 10}, {PoolID: "pool", Vantage: "us-east", BlockID: "east", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 90}, {PoolID: "pool", Vantage: "europe", BlockID: "europe", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 50}}
	h, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	west := dashboardPayload(t, h, "/dashboard-data?vantage=us-west")
	if west.OtherPools[0].MedianMS == nil || *west.OtherPools[0].MedianMS != 10 {
		t.Fatalf("west=%+v", west.OtherPools)
	}
	combined := dashboardPayload(t, h, "/dashboard-data?vantage=us-all")
	if combined.SelectedVantage != "us-all" || combined.OtherPools[0].Blocks != 2 || !combined.OtherPools[0].CombinedVantage {
		t.Fatalf("combined=%+v", combined)
	}
	unknown := httptest.NewRecorder()
	h.ServeHTTP(unknown, httptest.NewRequest(http.MethodGet, "/dashboard-data?vantage=moon", nil))
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown status=%d", unknown.Code)
	}
}

func TestDashboardDefaultsUseAvailableMeasurements(t *testing.T) {
	now := time.Now().UTC()
	pool := model.Pool{ID: "pool", Name: "Pool", Category: "shared"}
	regional := []model.Observation{{PoolID: "pool", Vantage: "us-west", BlockID: "west", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 10}}
	h, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return regional, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	if got := dashboardPayload(t, h, "/dashboard-data"); got.SelectedVantage != "us-all" || !got.ShowUSCombined {
		t.Fatalf("default=%+v", got)
	}
	local := []model.Observation{{PoolID: "pool", Vantage: "unknown", BlockID: "local", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 10}}
	h, err = (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return local, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	if got := dashboardPayload(t, h, "/dashboard-data"); got.SelectedVantage != "unknown" {
		t.Fatalf("local default=%q", got.SelectedVantage)
	}
}

func TestVantageStatusReportsLatestRunHealth(t *testing.T) {
	now := time.Now().UTC()
	started := now.Add(-time.Minute)
	observations := []model.Observation{{Version: model.ObservationVersion, Source: ingest.RemoteSource, Vantage: "us-west", RecordType: model.RecordTypeProtocol, ObservedAt: now.Add(-30 * time.Second)}, {Version: model.ObservationVersion, Source: ingest.RemoteSource, Vantage: "us-west", RecordType: model.RecordTypeProbeRun, ObservedAt: now, RunStartedAt: &started, RunStatus: "partial", ConfigRevision: "sha256:test", DroppedObservations: 2}}
	west := buildVantageStatuses(observations, now).Vantages[0]
	if west.ID != "us-west" || west.ProtocolAttempts != 1 || !west.Incomplete || west.DroppedObservations != 2 || !west.Stale {
		t.Fatalf("west=%+v", west)
	}
}

func TestSnapshotAndJSONAreBuiltOnlyAtStartupUntilInvalidated(t *testing.T) {
	loads := 0
	h, err := (Server{Pools: []model.Pool{{ID: "pool", Name: "Pool"}}, Load: func() ([]model.Observation, error) { loads++; return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	if loads != 1 {
		t.Fatalf("startup loads=%d", loads)
	}
	for i := 0; i < 3; i++ {
		dashboardPayload(t, h, "/dashboard-data")
	}
	if loads != 1 {
		t.Fatalf("request loads=%d, want 1", loads)
	}
}

func TestOldPublicJSONRoutesAreRemoved(t *testing.T) {
	h, err := (Server{Pools: []model.Pool{{ID: "pool", Name: "Pool"}}, Load: func() ([]model.Observation, error) { return nil, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/api/v1/reports", "/api/v1/vantages", "/api/v1/pools", "/api/v1/methodology"} {
		response := httptest.NewRecorder()
		h.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		if response.Code != http.StatusNotFound {
			t.Errorf("%s status=%d", path, response.Code)
		}
	}
}
