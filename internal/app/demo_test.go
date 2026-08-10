package app

import (
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/report"
)

func TestDemoUsesSyntheticNamesWideLatencyRangeAndSparseOutages(t *testing.T) {
	cfg, err := loadConfig("../../config/pools.json")
	if err != nil {
		t.Fatal(err)
	}
	pools := demoPools(cfg.Pools)
	if len(pools) != len(cfg.Pools) {
		t.Fatalf("demo pools=%d, want %d", len(pools), len(cfg.Pools))
	}
	for index, pool := range pools {
		if pool.Name == cfg.Pools[index].Name || pool.Operator != "" || pool.Website != "" || !strings.HasPrefix(pool.ID, "demo-pool-") {
			t.Fatalf("pool %d retains production identity: %+v", index, pool)
		}
		for _, endpoint := range pool.Endpoints {
			if !strings.HasSuffix(endpoint.Host, ".example.invalid") {
				t.Fatalf("demo endpoint is not synthetic: %q", endpoint.Host)
			}
		}
	}

	observations := demoData(pools)
	for _, observation := range observations {
		if observation.RecordType == "" && observation.Arrived && (observation.OffsetMS < 10 || observation.OffsetMS > 10_000) {
			t.Fatalf("demo block delay %.1f ms is outside 10–10,000 ms", observation.OffsetMS)
		}
	}
	snapshot := report.Compute(pools, observations, time.Now().UTC())
	issues := 0
	minimumMedian, maximumMedian := 100_000.0, 0.0
	for _, pool := range snapshot.Reports {
		if pool.Availability < 100 {
			issues++
		}
		if pool.MedianMS == nil {
			t.Fatalf("demo pool lacks latency: %s", pool.PoolName)
		}
		if *pool.MedianMS < minimumMedian {
			minimumMedian = *pool.MedianMS
		}
		if *pool.MedianMS > maximumMedian {
			maximumMedian = *pool.MedianMS
		}
	}
	if issues != 2 {
		t.Fatalf("availability issues=%d, want exactly 2", issues)
	}
	if minimumMedian < 9 || minimumMedian > 11 || maximumMedian < 9_500 || maximumMedian > 10_500 {
		t.Fatalf("demo median range=%.1f–%.1f ms, want roughly 10–10,000 ms", minimumMedian, maximumMedian)
	}
}
