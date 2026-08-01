package report

import (
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestComputeReportsAvailabilityAndLatency(t *testing.T) {
	pools := []model.Pool{{ID: "fast", Name: "Fast"}, {ID: "slow", Name: "Slow"}}
	var obs []model.Observation
	for i := 0; i < 40; i++ {
		block := string(rune('a' + i))
		obs = append(obs, model.Observation{PoolID: "fast", BlockID: block, Eligible: true, Arrived: true, OffsetMS: 20, TLS: true})
		obs = append(obs, model.Observation{PoolID: "slow", BlockID: block, Eligible: true, Arrived: i < 30, OffsetMS: 900})
	}
	s := Compute(pools, obs, time.Unix(0, 0))
	if len(s.Reports) != 2 || s.Reports[0].PoolID != "fast" {
		t.Fatalf("unexpected order: %+v", s.Reports)
	}
	if s.Reports[0].Availability <= s.Reports[1].Availability {
		t.Fatal("availability should reflect misses")
	}
	if s.Reports[0].MedianMS == nil || s.Reports[1].MedianMS == nil || *s.Reports[0].MedianMS >= *s.Reports[1].MedianMS {
		t.Fatal("median latency not reported")
	}

	if s.Reports[0].Confidence != "moderate" {
		t.Fatalf("confidence = %q", s.Reports[0].Confidence)
	}
}

func TestReportsSortByPoolName(t *testing.T) {
	pools := []model.Pool{{ID: "new", Name: "Zulu"}, {ID: "known", Name: "Alpha"}}
	var obs []model.Observation
	obs = append(obs, model.Observation{PoolID: "new", BlockID: "one", Eligible: true, Arrived: true, TLS: true})
	for i := 0; i < 30; i++ {
		obs = append(obs, model.Observation{PoolID: "known", BlockID: string(rune(i + 1)), Eligible: true, Arrived: true, OffsetMS: 1000})
	}
	s := Compute(pools, obs, time.Now())
	if s.Reports[0].PoolID != "known" {
		t.Fatalf("reports not sorted by pool name: %+v", s.Reports)
	}
}
