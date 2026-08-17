package report

import (
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestAtomicBlockSampleExpandsOnlyInsideReportEngine(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	pools := []model.Pool{{
		ID: "pool", Name: "Pool",
		Endpoints: []model.Endpoint{{Host: "one.example", Port: 3333}, {Host: "two.example", Port: 443, TLS: true}},
	}}
	offset := 0.0
	connect := &model.ProtocolSample{ObservedAt: now.Add(-time.Minute), DurationMS: 12.5, ResponseStatus: model.ProtocolStatusOK}
	sample := model.Observation{
		Version: model.ObservationVersion, ObservationID: "lax-block", RunID: "lax-block",
		Source: model.SourceRemoteScheduled, ConfigRevision: "sha256:test",
		RecordType: model.RecordTypeBlockSample, ObservedAt: now.Add(-30 * time.Second), Vantage: "us-west",
		BlockID: "block", BlockHeight: 900_000,
		EndpointSamples: []model.EndpointBlockSample{{
			PoolID: "pool", Endpoint: "one.example:3333", OffsetMS: &offset,
			Setup: &model.EndpointSetup{Connect: connect},
		}},
		EligibleEndpoints: []model.EndpointIdentity{
			{PoolID: "pool", Endpoint: "one.example:3333"},
			{PoolID: "pool", Endpoint: "two.example:443", TLS: true},
		},
	}

	prepared := Prepare(pools, []model.Observation{sample}, now)
	if len(prepared.Observations()) != 4 {
		t.Fatalf("expanded records=%d, want two endpoints, one setup, and one completion", len(prepared.Observations()))
	}
	snapshot := prepared.ComputeVantage("us-west")
	if snapshot.BlocksObserved != 1 || snapshot.LatestBlockHeight != 900_000 || len(snapshot.Reports) != 2 {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	plain, secure := snapshot.Reports[0], snapshot.Reports[1]
	if plain.Endpoint != "one.example:3333" || plain.MedianMS == nil || *plain.MedianMS != 0 ||
		plain.Availability != 100 || plain.ConnectTiming.Attempts != 1 || plain.ConnectTiming.MedianMS == nil || *plain.ConnectTiming.MedianMS != 12.5 {
		t.Fatalf("plain report=%+v", plain)
	}
	if secure.Endpoint != "two.example:443" || secure.MedianMS != nil || secure.Availability != 0 || secure.ConnectTiming.Attempts != 0 {
		t.Fatalf("secure report=%+v", secure)
	}
}
