package web

import (
	"github.com/M45Core/StratumStats/internal/model"
	"testing"
)

func TestScoreTiesRankByLatency(t *testing.T) {
	number := func(v float64) *float64 { return &v }
	pool := func(name string, score, median, p95 *float64) dashboardPool {
		return dashboardPool{SortName: name, PoolReport: model.PoolReport{OverallScore: score, MedianMS: median, P95MS: p95}}
	}
	pools := []dashboardPool{
		pool("Atlas", number(100), number(189), number(189)),
		pool("CKPool", number(100), number(236), number(236)),
		pool("Solo EU", number(100), number(85), number(85)),
		pool("Solo US 3333", number(100), number(1), number(1)),
		pool("Solo US 4444", number(100), number(0), number(0)),
		pool("A lower fractional score", number(99.9999), number(0), number(0)),
		pool("A missing median", number(100), nil, number(0)),
		pool("A missing P95", number(100), number(85), nil),
		pool("A higher P95", number(100), number(85), number(90)),
		pool("Z equal latency", number(100), number(85), number(85)),
		pool("A unscored", nil, number(0), number(0)),
	}
	sortByOverallScore(pools)
	want := []string{"Solo US 4444", "Solo US 3333", "Solo EU", "Z equal latency", "A higher P95", "A missing P95", "Atlas", "CKPool", "A missing median", "A lower fractional score", "A unscored"}
	for i, name := range want {
		if pools[i].SortName != name {
			t.Fatalf("position %d = %q, want %q", i, pools[i].SortName, name)
		}
	}
}
