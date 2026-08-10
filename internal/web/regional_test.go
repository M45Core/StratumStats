package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/ingest"
	"github.com/M45Core/StratumStats/internal/model"
)

func TestReportsFilterByVantageAndRejectUnknownValues(t *testing.T) {
	pool := model.Pool{ID: "pool", Name: "Pool"}
	now := time.Now().UTC().Add(-time.Minute)
	observations := []model.Observation{
		{PoolID: "pool", Vantage: "us-west", BlockID: "west", ObservedAt: now.Add(-time.Second), Eligible: true, Arrived: true, OffsetMS: 10},
		{PoolID: "pool", Vantage: "us-east", BlockID: "east", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 90},
		{PoolID: "pool", Vantage: "europe", BlockID: "europe", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 50},
	}
	handler, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/reports?vantage=us-west", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var snapshot model.Snapshot
	if err := json.Unmarshal(response.Body.Bytes(), &snapshot); err != nil {
		t.Fatal(err)
	}
	if snapshot.BlocksObserved != 1 || snapshot.Reports[0].MedianMS == nil || *snapshot.Reports[0].MedianMS != 10 {
		t.Fatalf("snapshot=%+v", snapshot)
	}

	unknown := httptest.NewRecorder()
	handler.ServeHTTP(unknown, httptest.NewRequest("GET", "/api/v1/reports?vantage=moon", nil))
	if unknown.Code != http.StatusBadRequest {
		t.Fatalf("unknown status=%d", unknown.Code)
	}

	combined := httptest.NewRecorder()
	handler.ServeHTTP(combined, httptest.NewRequest("GET", "/api/v1/reports?vantage=us-all", nil))
	var combinedSnapshot model.Snapshot
	if err := json.Unmarshal(combined.Body.Bytes(), &combinedSnapshot); err != nil {
		t.Fatal(err)
	}
	if combinedSnapshot.BlocksObserved != 2 {
		t.Fatalf("combined blocks=%d, want 2", combinedSnapshot.BlocksObserved)
	}

	europe := httptest.NewRecorder()
	handler.ServeHTTP(europe, httptest.NewRequest("GET", "/api/v1/reports?vantage=europe", nil))
	var europeSnapshot model.Snapshot
	if err := json.Unmarshal(europe.Body.Bytes(), &europeSnapshot); err != nil {
		t.Fatal(err)
	}
	if europeSnapshot.BlocksObserved != 1 || europeSnapshot.Reports[0].MedianMS == nil || *europeSnapshot.Reports[0].MedianMS != 50 {
		t.Fatalf("europe snapshot=%+v", europeSnapshot)
	}
}

func TestVantageStatusReportsLatestRunHealth(t *testing.T) {
	now := time.Now().UTC()
	started := now.Add(-time.Minute)
	observations := []model.Observation{
		{Version: model.ObservationVersion, Source: ingest.RemoteSource, Vantage: "us-west", RecordType: model.RecordTypeProtocol, ObservedAt: now.Add(-30 * time.Second)},
		{Version: model.ObservationVersion, Source: ingest.RemoteSource, Vantage: "us-west", RecordType: model.RecordTypeProbeRun, ObservedAt: now, RunStartedAt: &started, RunStatus: "partial", ConfigRevision: "sha256:test", DroppedObservations: 2},
	}
	statuses := buildVantageStatuses(observations, now)
	west := statuses.Vantages[0]
	if west.ID != "us-west" || west.ProtocolAttempts != 1 || !west.Incomplete || west.DroppedObservations != 2 || !west.Stale {
		t.Fatalf("west=%+v", west)
	}
}

func TestSnapshotCacheLoadsOnceWithinTTL(t *testing.T) {
	loads := 0
	handler, err := (Server{
		Pools: []model.Pool{{ID: "pool", Name: "Pool"}},
		Load: func() ([]model.Observation, error) {
			loads++
			return nil, nil
		},
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest("GET", "/api/v1/reports", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("status=%d", response.Code)
		}
	}
	if loads != 1 {
		t.Fatalf("loads=%d, want 1", loads)
	}
}

func TestDashboardRendersRegionalSelectionAndStatus(t *testing.T) {
	now := time.Now().UTC()
	started := now.Add(-time.Minute)
	observations := []model.Observation{
		{Version: model.ObservationVersion, Source: ingest.RemoteSource, Vantage: "us-west", RecordType: model.RecordTypeProbeRun, ObservedAt: now, RunStartedAt: &started, RunStatus: "ok"},
		{Version: model.ObservationVersion, Source: ingest.RemoteSource, Vantage: "europe", RecordType: model.RecordTypeProbeRun, ObservedAt: now.Add(-3 * time.Hour), RunStartedAt: &started, RunStatus: "ok"},
	}
	handler, err := (Server{
		Pools: []model.Pool{{ID: "pool", Name: "Pool"}},
		Load:  func() ([]model.Observation, error) { return observations, nil },
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/?vantage=us-west", nil))
	body := response.Body.String()
	for _, expected := range []string{`aria-current="page">West`, "US West", "last checked", "last checked"} {
		if !strings.Contains(body, expected) {
			t.Errorf("dashboard missing %q", expected)
		}
	}
	for _, unexpected := range []string{"vantage=us-central", "vantage=us-east", "vantage=europe"} {
		if strings.Contains(body, unexpected) {
			t.Errorf("dashboard renders unavailable region %q", unexpected)
		}
	}
}

func TestDashboardDefaultsToUSCombined(t *testing.T) {
	now := time.Now().UTC()
	started := now.Add(-time.Minute)
	observations := []model.Observation{
		{PoolID: "pool", Vantage: "us-west", BlockID: "west", ObservedAt: now.Add(-time.Second), Eligible: true, Arrived: true, OffsetMS: 10},
		{PoolID: "pool", Vantage: "us-east", BlockID: "east", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 90},
		{PoolID: "pool", Vantage: "europe", BlockID: "europe", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 50},
		{Version: model.ObservationVersion, Source: ingest.RemoteSource, Vantage: "us-west", RecordType: model.RecordTypeProbeRun, ObservedAt: now, RunStartedAt: &started, RunStatus: "ok"},
		{Version: model.ObservationVersion, Source: ingest.RemoteSource, Vantage: "us-east", RecordType: model.RecordTypeProbeRun, ObservedAt: now, RunStartedAt: &started, RunStatus: "ok"},
		{Version: model.ObservationVersion, Source: ingest.RemoteSource, Vantage: "europe", RecordType: model.RecordTypeProbeRun, ObservedAt: now, RunStartedAt: &started, RunStatus: "ok"},
	}
	handler, err := (Server{
		Pools: []model.Pool{{ID: "pool", Name: "Pool"}},
		Load:  func() ([]model.Observation, error) { return observations, nil },
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	body := response.Body.String()
	for _, expected := range []string{`aria-current="page">US combined`, "US combined", "vantage=europe", "Bitcoin blocks observed</span><strong>2</strong>"} {
		if !strings.Contains(body, expected) {
			t.Errorf("dashboard missing %q", expected)
		}
	}
	if strings.Contains(body, "All data") {
		t.Error("dashboard still renders the all-data selector")
	}
}

func TestDashboardDefaultsToLocalWithoutRegionalProbeData(t *testing.T) {
	now := time.Now().UTC()
	observations := []model.Observation{{PoolID: "pool", Vantage: "unknown", BlockID: "local", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 25}}
	handler, err := (Server{
		Pools: []model.Pool{{ID: "pool", Name: "Pool"}},
		Load:  func() ([]model.Observation, error) { return observations, nil },
	}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("GET", "/", nil))
	body := response.Body.String()
	for _, expected := range []string{`aria-current="page">Local`, "locally collected data", "Bitcoin blocks observed</span><strong>1</strong>"} {
		if !strings.Contains(body, expected) {
			t.Errorf("dashboard missing %q", expected)
		}
	}
	for _, unexpected := range []string{"US combined", "vantage=us-west", "vantage=europe"} {
		if strings.Contains(body, unexpected) {
			t.Errorf("dashboard renders unavailable view %q", unexpected)
		}
	}
}
