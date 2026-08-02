package web

import (
	"fmt"

	"github.com/proofofmike/stratumstats/internal/model"
)

type dashboardPool struct {
	model.PoolReport
	LatencyClass string
	UnsafeReason string
}

type dashboardPage struct {
	Snapshot             model.Snapshot
	Demo                 bool
	NormalPools          []dashboardPool
	UnsafePools          []dashboardPool
	PoolCount            int
	BlockCount           int
	TemplateObservations int
	UnsafeCount          int
}

func buildDashboardPage(snapshot model.Snapshot, demo bool) dashboardPage {
	page := dashboardPage{Snapshot: snapshot, Demo: demo, PoolCount: len(snapshot.Reports)}
	for _, pool := range snapshot.Reports {
		if pool.Blocks > page.BlockCount {
			page.BlockCount = pool.Blocks
		}
		page.TemplateObservations += pool.Arrivals
	}
	for _, report := range snapshot.Reports {
		pool := dashboardPool{PoolReport: report, LatencyClass: latencyClass(report.MedianMS)}
		switch report.WorkerAddressStatus {
		case "not_observed":
			pool.UnsafeReason = fmt.Sprintf("worker address absent in %d decoded jobs", report.CoinbaseSamples)
		case "varied":
			pool.UnsafeReason = fmt.Sprintf("worker address absent in some of %d decoded jobs", report.CoinbaseSamples)
		}
		if pool.UnsafeReason != "" {
			page.UnsafePools = append(page.UnsafePools, pool)
		} else {
			page.NormalPools = append(page.NormalPools, pool)
		}
	}
	page.UnsafeCount = len(page.UnsafePools)
	return page
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
