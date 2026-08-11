package report

import (
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestComputeReportsConfiguredEndpointsIndependently(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	pool := model.Pool{ID: "pool", Name: "Pool", Endpoints: []model.Endpoint{
		{Host: "plain.example", Port: 3333},
		{Host: "secure.example", Port: 443, TLS: true, Region: "Europe"},
	}}
	observations := []model.Observation{
		{Version: model.ObservationVersion, ObservedAt: now.Add(-time.Minute), Vantage: "us-west", BlockID: "block", PoolID: pool.ID, Endpoint: "plain.example:3333", Eligible: true, Arrived: true, OffsetMS: 25},
		{Version: model.ObservationVersion, ObservedAt: now.Add(-time.Minute), Vantage: "us-west", BlockID: "block", PoolID: pool.ID, Endpoint: "secure.example:443", TLS: true, Eligible: true},
	}

	snapshot := Compute([]model.Pool{pool}, observations, now)
	if len(snapshot.Reports) != 2 || snapshot.EligibleEndpointSamples != 2 || snapshot.TemplateDeliveries != 1 {
		t.Fatalf("snapshot = %+v, want two endpoint samples and one delivery", snapshot)
	}
	plain, secure := snapshot.Reports[0], snapshot.Reports[1]
	if plain.Endpoint != "plain.example:3333" || plain.EndpointTLS || plain.Availability != 100 || plain.Arrivals != 1 {
		t.Fatalf("plain endpoint report = %+v", plain)
	}
	if secure.Endpoint != "secure.example:443" || !secure.EndpointTLS || secure.EndpointRegion != "Europe" || secure.Availability != 0 || secure.Arrivals != 0 {
		t.Fatalf("TLS endpoint report = %+v", secure)
	}
}

func TestComputeIgnoresPoolOnlyV9ObservationForMultiEndpointPool(t *testing.T) {
	now := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	pool := model.Pool{ID: "pool", Name: "Pool", Endpoints: []model.Endpoint{
		{Host: "one.example", Port: 3333}, {Host: "two.example", Port: 3333},
	}}
	snapshot := Compute([]model.Pool{pool}, []model.Observation{{
		Version: model.ObservationVersion, ObservedAt: now, Vantage: "us-west", BlockID: "block", PoolID: pool.ID, Eligible: true, Arrived: true,
	}}, now)
	if snapshot.EligibleEndpointSamples != 0 {
		t.Fatalf("pool-only observation was ambiguously assigned: %+v", snapshot)
	}
}
