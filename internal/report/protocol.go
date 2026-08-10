package report

import (
	"sort"

	"github.com/M45Core/StratumStats/internal/model"
)

type timingAccumulator struct {
	attempts          int
	durations         []float64
	rejected          int
	unsupported       int
	timeouts          int
	errors            int
	certificateErrors int
}

func addProtocolObservation(a *accumulator, o model.Observation) {
	if o.ProtocolMethod == model.ProtocolTLSHandshake && o.ResponseStatus == model.ProtocolStatusOK {
		a.tls = true
	}
	if a.timings == nil {
		a.timings = map[string]*timingAccumulator{}
	}
	t := a.timings[o.ProtocolMethod]
	if t == nil {
		t = &timingAccumulator{}
		a.timings[o.ProtocolMethod] = t
	}
	t.attempts++
	switch o.ResponseStatus {
	case model.ProtocolStatusOK:
		if o.DurationMS != nil && *o.DurationMS >= 0 {
			t.durations = append(t.durations, *o.DurationMS)
		} else {
			t.errors++
		}
	case model.ProtocolStatusRejected:
		t.rejected++
	case model.ProtocolStatusUnsupported:
		t.unsupported++
	case model.ProtocolStatusTimeout:
		t.timeouts++
	default:
		t.errors++
		if o.ProtocolMethod == model.ProtocolTLSHandshake && o.ErrorCategory == model.ProtocolErrorTLSCertificateInvalid {
			t.certificateErrors++
		}
	}
}

func timingStats(a *accumulator, method string) model.TimingStats {
	stats := model.TimingStats{}
	t := a.timings[method]
	if t == nil {
		return stats
	}
	stats.Attempts = t.attempts
	stats.Successes = len(t.durations)
	stats.Rejected = t.rejected
	stats.Unsupported = t.unsupported
	stats.Timeouts = t.timeouts
	stats.Errors = t.errors
	stats.CertificateErrors = t.certificateErrors
	if len(t.durations) > 0 {
		sort.Float64s(t.durations)
		median := round(percentile(t.durations, .5), 1)
		p95 := round(percentile(t.durations, .95), 1)
		stats.MedianMS = &median
		stats.P95MS = &p95
	}
	return stats
}
