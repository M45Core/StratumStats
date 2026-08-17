package web

import (
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func BenchmarkDashboardRebuild(b *testing.B) {
	const (
		poolCount     = 25
		blocksPerNode = 48
	)
	now := time.Date(2026, time.August, 16, 12, 0, 0, 0, time.UTC)
	pools := make([]model.Pool, 0, poolCount)
	observations := make([]model.Observation, 0, poolCount*2*len(vantageOrder)*blocksPerNode)
	for poolIndex := range poolCount {
		poolID := fmt.Sprintf("pool-%02d", poolIndex)
		endpoints := []model.Endpoint{
			{Host: poolID + ".example", Port: 3333},
			{Host: poolID + ".example", Port: 443, TLS: true},
		}
		pools = append(pools, model.Pool{ID: poolID, Name: "Pool " + poolID, Category: "shared", Endpoints: endpoints})
		for _, vantage := range vantageOrder {
			for blockIndex := range blocksPerNode {
				observedAt := now.Add(-time.Duration(blocksPerNode-blockIndex) * 10 * time.Minute)
				for _, endpoint := range endpoints {
					observations = append(observations, model.Observation{
						Version: model.ObservationVersion, ObservedAt: observedAt,
						Vantage: vantage, BlockID: fmt.Sprintf("block-%03d", blockIndex),
						PoolID: poolID, Endpoint: net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port)),
						Eligible: true, Arrived: true, OffsetMS: float64(poolIndex*10 + blockIndex), TLS: endpoint.TLS,
					})
				}
			}
		}
	}
	snapshots := &snapshotCache{pools: pools, load: func() ([]model.Observation, error) { return observations, nil }}
	responses := &dashboardResponseCache{}
	b.ReportAllocs()
	b.ReportMetric(float64(len(observations)), "observations/op")
	b.ResetTimer()
	for b.Loop() {
		snapshots.invalidate()
		if err := responses.rebuildFrom(snapshots, pools, false, ""); err != nil {
			b.Fatal(err)
		}
	}
}
