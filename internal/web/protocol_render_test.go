package web

import (
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestProtocolTimingsAndTLSPenaltyReachDashboardData(t *testing.T) {
	observedAt := time.Now().UTC().Add(-time.Minute)
	duration := 12.6
	pool := model.Pool{ID: "test", Name: "Test Pool", Endpoints: []model.Endpoint{{Host: "test.example", Port: 443, TLS: true}}}
	observations := []model.Observation{
		{ObservedAt: observedAt, Vantage: "us-east", BlockID: "block", PoolID: "test", Endpoint: "test.example:443", TLS: true, Eligible: true, Arrived: true, OffsetMS: 42.6},
		{ObservedAt: observedAt, Vantage: "us-east", RecordType: model.RecordTypeProtocol, PoolID: "test", Endpoint: "test.example:443", TLS: true, ProtocolMethod: model.ProtocolSubscribe, ResponseStatus: model.ProtocolStatusOK, DurationMS: &duration},
		{ObservedAt: observedAt, Vantage: "us-east", RecordType: model.RecordTypeProtocol, PoolID: "test", Endpoint: "test.example:443", TLS: true, ProtocolMethod: model.ProtocolTLSHandshake, ResponseStatus: model.ProtocolStatusError, ErrorCategory: model.ProtocolErrorTLSCertificateInvalid, DurationMS: &duration},
	}
	h, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	payload := dashboardPayload(t, h, "/dashboard-data?transport=tls")
	if len(payload.NormalPools)+len(payload.OtherPools)+len(payload.PPLNSPools)+len(payload.NoRecentDataPools) == 0 {
		t.Fatal("TLS report missing")
	}
	var got dashboardPool
	for _, group := range [][]dashboardPool{payload.NormalPools, payload.OtherPools, payload.PPLNSPools, payload.NoRecentDataPools} {
		if len(group) > 0 {
			got = group[0]
			break
		}
	}
	if got.MedianMS == nil || *got.MedianMS != 42.6 || got.SubscribeTiming.MedianMS == nil || got.TLSTiming.CertificateErrors != 1 || got.TLSCertificatePenalty != 10 {
		t.Fatalf("report=%+v", got.PoolReport)
	}
}
