package web

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestDashboardRefreshSchedulerCoalescesBurst(t *testing.T) {
	var calls atomic.Int32
	refreshed := make(chan struct{})
	scheduler := &dashboardRefreshScheduler{
		delay: 25 * time.Millisecond,
		refresh: func() error {
			if calls.Add(1) == 1 {
				close(refreshed)
			}
			return nil
		},
	}
	for range 20 {
		scheduler.schedule()
	}
	select {
	case <-refreshed:
	case <-time.After(time.Second):
		t.Fatal("dashboard refresh did not run")
	}
	time.Sleep(75 * time.Millisecond)
	if got := calls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
}

func TestDashboardRefreshSchedulerRunsOnceMoreForArrivalDuringRefresh(t *testing.T) {
	var calls atomic.Int32
	started := make(chan int, 2)
	release := make(chan struct{}, 2)
	scheduler := &dashboardRefreshScheduler{
		delay: 10 * time.Millisecond,
		refresh: func() error {
			started <- int(calls.Add(1))
			<-release
			return nil
		},
	}
	scheduler.schedule()
	select {
	case call := <-started:
		if call != 1 {
			t.Fatalf("first refresh call = %d", call)
		}
	case <-time.After(time.Second):
		t.Fatal("first dashboard refresh did not start")
	}
	for range 20 {
		scheduler.schedule()
	}
	release <- struct{}{}
	select {
	case call := <-started:
		if call != 2 {
			t.Fatalf("second refresh call = %d", call)
		}
	case <-time.After(time.Second):
		t.Fatal("follow-up dashboard refresh did not start")
	}
	release <- struct{}{}
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 2 {
		t.Fatalf("refresh calls = %d, want 2", got)
	}
}

func TestDashboardResponseCacheSwapsOnlyAfterCompleteRebuild(t *testing.T) {
	now := time.Now().UTC().Add(-time.Minute)
	pools := []model.Pool{{ID: "pool", Name: "Pool", Category: "shared"}}
	observations := []model.Observation{{PoolID: "pool", Vantage: "unknown", BlockID: "one", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 10}}
	started, release := make(chan struct{}), make(chan struct{})
	blockLoad := false
	snapshots := &snapshotCache{pools: pools, load: func() ([]model.Observation, error) {
		if blockLoad {
			close(started)
			<-release
		}
		return observations, nil
	}}
	responses := &dashboardResponseCache{}
	if err := responses.rebuildFrom(snapshots, pools, false, ""); err != nil {
		t.Fatal(err)
	}
	before, ok := responses.response("unknown\x00plain")
	if !ok || !strings.Contains(string(before.body), `"median_ms":10`) {
		t.Fatalf("initial response=%s", before.body)
	}

	observations = []model.Observation{{PoolID: "pool", Vantage: "unknown", BlockID: "two", ObservedAt: now, Eligible: true, Arrived: true, OffsetMS: 90}}
	blockLoad = true
	snapshots.invalidate()
	done := make(chan error, 1)
	go func() { done <- responses.rebuildFrom(snapshots, pools, false, "") }()
	<-started
	during, ok := responses.response("unknown\x00plain")
	if !ok || during.etag != before.etag {
		t.Fatal("active response changed before rebuild completed")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	after, ok := responses.response("unknown\x00plain")
	if !ok || after.etag == before.etag || !strings.Contains(string(after.body), `"median_ms":90`) {
		t.Fatalf("replacement response=%s", after.body)
	}
}

func TestDashboardResponseCacheKeepsPreviousDataAfterFailedRebuild(t *testing.T) {
	pools := []model.Pool{{ID: "pool", Name: "Pool"}}
	fail := false
	snapshots := &snapshotCache{pools: pools, load: func() ([]model.Observation, error) {
		if fail {
			return nil, errors.New("load failed")
		}
		return nil, nil
	}}
	responses := &dashboardResponseCache{}
	if err := responses.rebuildFrom(snapshots, pools, false, ""); err != nil {
		t.Fatal(err)
	}
	before, _ := responses.response("\x00plain")
	fail = true
	snapshots.invalidate()
	if err := responses.rebuildFrom(snapshots, pools, false, ""); err == nil {
		t.Fatal("failed rebuild returned nil")
	}
	after, ok := responses.response("\x00plain")
	if !ok || after.etag != before.etag {
		t.Fatal("failed rebuild replaced the active response")
	}
}
