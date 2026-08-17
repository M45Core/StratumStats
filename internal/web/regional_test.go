package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/ingest"
	"github.com/M45Core/StratumStats/internal/model"
)

func TestDashboardDataFiltersByVantage(t *testing.T) {
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
	combined := httptest.NewRecorder()
	h.ServeHTTP(combined, httptest.NewRequest(http.MethodGet, "/dashboard-data?vantage=us-all", nil))
	if combined.Code != http.StatusBadRequest {
		t.Fatalf("retired combined status=%d", combined.Code)
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
	regional := []model.Observation{{PoolID: "pool", Vantage: "us-east", BlockID: "east", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 10}}
	h, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return regional, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	if got := dashboardPayload(t, h, "/dashboard-data"); got.SelectedVantage != "us-east" {
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
	started := now.Add(-time.Minute)
	asia := []model.Observation{
		{Source: ingest.RemoteSource, PoolID: "pool", Vantage: "japan", BlockID: "asia", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 10},
		{Source: ingest.RemoteSource, Vantage: "japan", RecordType: model.RecordTypeProbeRun, ObservedAt: now, RunStartedAt: &started, RunStatus: "ok"},
	}
	h, err = (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return asia, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	if got := dashboardPayload(t, h, "/dashboard-data"); got.SelectedVantage != "japan" {
		t.Fatalf("Asia default=%q", got.SelectedVantage)
	}
}

func TestVantageStatusReportsLatestRunHealth(t *testing.T) {
	now := time.Now().UTC()
	started := now.Add(-time.Minute)
	observations := []model.Observation{{Version: model.ObservationVersion, Source: ingest.RemoteSource, Vantage: "us-west", RecordType: model.RecordTypeProtocol, ObservedAt: now.Add(-30 * time.Second)}, {Version: model.ObservationVersion, Source: ingest.RemoteSource, Vantage: "us-west", RecordType: model.RecordTypeProbeRun, ObservedAt: now, RunStartedAt: &started, RunStatus: "ok", ConfigRevision: "sha256:test", DroppedObservations: 2}}
	statuses := buildVantageStatuses(observations, now, "sha256:current").Vantages
	var west vantageStatus
	for _, status := range statuses {
		if status.ID == "us-west" {
			west = status
			break
		}
	}
	if west.ConfigCurrent || west.ID != "us-west" || west.Label != "US West · Los Angeles" || west.ProtocolAttempts != 1 || !west.Incomplete || west.DroppedObservations != 2 || !west.Stale {
		t.Fatalf("west=%+v", west)
	}
	current := buildVantageStatuses(observations, now, "sha256:test").Vantages
	for _, status := range current {
		if status.ID == "us-west" && !status.ConfigCurrent {
			t.Fatalf("current revision not recognized: %+v", status)
		}
	}
}

func TestRegionalNodeOrderAndLabels(t *testing.T) {
	want := []struct {
		id    string
		label string
	}{
		{id: "us-east", label: "US East · Ashburn"},
		{id: "europe", label: "Europe · Frankfurt"},
		{id: "us-west", label: "US West · Los Angeles"},
		{id: "japan", label: "Japan · Tokyo"},
		{id: "singapore", label: "Southeast Asia · Singapore"},
	}
	if len(vantageOrder) != len(want) {
		t.Fatalf("vantageOrder=%v", vantageOrder)
	}
	for index, expected := range want {
		if vantageOrder[index] != expected.id || vantageLabels[expected.id] != expected.label {
			t.Fatalf("vantage %d = %q/%q, want %q/%q", index, vantageOrder[index], vantageLabels[vantageOrder[index]], expected.id, expected.label)
		}
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
