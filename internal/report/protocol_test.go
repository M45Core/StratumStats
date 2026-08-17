package report

import (
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
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
	}

	snapshot := Compute([]model.Pool{{ID: "pool", Name: "Pool"}}, observations, time.Time{})
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
}

func TestTLSCertificateValidationFailuresRemainVisible(t *testing.T) {
	duration := 12.5
	now := time.Now().UTC()
	observations := []model.Observation{
		{ObservedAt: now.Add(-2 * time.Minute), PoolID: "pool", RecordType: model.RecordTypeProtocol, ProtocolMethod: model.ProtocolTLSHandshake, ResponseStatus: model.ProtocolStatusOK, DurationMS: &duration},
		{ObservedAt: now.Add(-time.Minute), PoolID: "pool", RecordType: model.RecordTypeProtocol, ProtocolMethod: model.ProtocolTLSHandshake, ResponseStatus: model.ProtocolStatusError, ErrorCategory: model.ProtocolErrorTLSCertificateInvalid, DurationMS: &duration},
	}
	got := Compute([]model.Pool{{ID: "pool", Name: "Pool"}}, observations, now).Reports[0]
	if got.TLSTiming.Attempts != 2 || got.TLSTiming.Successes != 1 || got.TLSTiming.Errors != 1 || got.TLSTiming.CertificateErrors != 1 {
		t.Fatalf("TLS timing=%+v", got.TLSTiming)
	}
	if !got.TLSObserved {
		t.Fatal("successful TLS handshake was not retained alongside the certificate error")
	}
}
