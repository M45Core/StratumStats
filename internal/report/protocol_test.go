package report

import (
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestProtocolTimingsRemainSeparateFromBlockSamples(t *testing.T) {
	duration := func(value float64) *float64 { return &value }
	protocol := func(method, status string, value float64) model.Observation {
		return model.Observation{
			Version:        model.ObservationVersion,
			RecordType:     model.RecordTypeProtocol,
			PoolID:         "pool",
			ProtocolMethod: method,
			ResponseStatus: status,
			DurationMS:     duration(value),
		}
	}
	observations := []model.Observation{
		protocol(model.ProtocolConnect, model.ProtocolStatusOK, 10),
		protocol(model.ProtocolConnect, model.ProtocolStatusOK, 20),
		protocol(model.ProtocolConnect, model.ProtocolStatusOK, 30),
		protocol(model.ProtocolConnect, model.ProtocolStatusTimeout, 1000),
		protocol(model.ProtocolPing, model.ProtocolStatusOK, 15),
		protocol(model.ProtocolPing, model.ProtocolStatusUnsupported, 2),
	}

	snapshot := Compute([]model.Pool{{ID: "pool", Name: "Pool"}}, observations, time.Unix(0, 0))
	report := snapshot.Reports[0]
	if report.Blocks != 0 || report.Arrivals != 0 {
		t.Fatalf("protocol records became block samples: %+v", report)
	}
	if report.ConnectTiming.Attempts != 4 || report.ConnectTiming.Successes != 3 || report.ConnectTiming.Timeouts != 1 {
		t.Fatalf("connect timing=%+v", report.ConnectTiming)
	}
	if report.ConnectTiming.MedianMS == nil || *report.ConnectTiming.MedianMS != 20 {
		t.Fatalf("connect median=%v", report.ConnectTiming.MedianMS)
	}
	if report.ConnectTiming.P95MS == nil || *report.ConnectTiming.P95MS != 30 {
		t.Fatalf("connect p95=%v", report.ConnectTiming.P95MS)
	}
	if report.PingTiming.Attempts != 2 || report.PingTiming.Successes != 1 || report.PingTiming.Unsupported != 1 {
		t.Fatalf("ping timing=%+v", report.PingTiming)
	}
}
