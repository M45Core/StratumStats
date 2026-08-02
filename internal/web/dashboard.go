package web

import (
	"fmt"
	"sort"

	"github.com/proofofmike/stratumstats/internal/model"
)

type dashboardPool struct {
	model.PoolReport
	LatencyClass string
	UnsafeReason string
}

type dashboardPage struct {
	Snapshot           model.Snapshot
	Demo               bool
	NormalPools        []dashboardPool
	UnsafePools        []dashboardPool
	PoolCount          int
	BlocksObserved     int
	PoolsWithBlockData int
	UnsafeCount        int
}

func buildDashboardPage(snapshot model.Snapshot, demo bool) dashboardPage {
	page := dashboardPage{
		Snapshot:       snapshot,
		Demo:           demo,
		PoolCount:      len(snapshot.Reports),
		BlocksObserved: snapshot.BlocksObserved,
	}
	for _, report := range snapshot.Reports {
		if report.MedianMS != nil {
			page.PoolsWithBlockData++
		}
		pool := dashboardPool{PoolReport: report, LatencyClass: latencyClass(report.MedianMS)}
		switch report.WorkerAddressStatus {
		case "always_observed":
			// Positive worker-address evidence is required for the main list.
		case "not_observed":
			pool.UnsafeReason = fmt.Sprintf("worker address absent in %d decoded jobs", report.CoinbaseSamples)
		case "varied":
			pool.UnsafeReason = fmt.Sprintf("worker address absent in some of %d decoded jobs", report.CoinbaseSamples)
		default:
			pool.UnsafeReason = "worker address not yet verified"
		}
		if pool.UnsafeReason != "" {
			page.UnsafePools = append(page.UnsafePools, pool)
		} else {
			page.NormalPools = append(page.NormalPools, pool)
		}
	}
	sortByTemplateLatency(page.NormalPools)
	sortByTemplateLatency(page.UnsafePools)
	page.UnsafeCount = len(page.UnsafePools)
	return page
}

func sortByTemplateLatency(pools []dashboardPool) {
	sort.SliceStable(pools, func(i, j int) bool {
		left, right := pools[i], pools[j]
		if left.MedianMS == nil || right.MedianMS == nil {
			if left.MedianMS == nil && right.MedianMS == nil {
				return left.PoolName < right.PoolName
			}
			return left.MedianMS != nil
		}
		if *left.MedianMS == *right.MedianMS {
			return left.PoolName < right.PoolName
		}
		return *left.MedianMS < *right.MedianMS
	})
}

func latencyClass(value *float64) string {
	if value == nil {
		return "latency-none"
	}
	thresholds := [...]float64{100, 200, 300, 500, 750, 1000, 1500, 2500, 4000}
	for index, threshold := range thresholds {
		if *value <= threshold {
			return fmt.Sprintf("latency-%d", index+1)
		}
	}
	return "latency-10"
}
