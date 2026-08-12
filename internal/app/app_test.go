package app

import (
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
	"github.com/M45Core/StratumStats/internal/store"
)

func TestRepositoryPoolRegistryIsWellFormed(t *testing.T) {
	cfg, err := loadConfig("../../config/pools.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Pools) == 0 {
		t.Fatal("pool registry is empty")
	}

	poolIDs := make(map[string]struct{}, len(cfg.Pools))
	for _, pool := range cfg.Pools {
		if pool.ID == "" || pool.Name == "" {
			t.Errorf("pool must have a non-empty ID and name: %+v", pool)
		}
		if _, exists := poolIDs[pool.ID]; exists {
			t.Errorf("duplicate pool ID %q", pool.ID)
		}
		poolIDs[pool.ID] = struct{}{}

		website, err := url.Parse(pool.Website)
		if err != nil || website.Scheme != "https" || website.Host == "" ||
			(website.Path != "" && website.Path != "/") || website.RawQuery != "" || website.Fragment != "" {
			t.Errorf("pool %q website is not an HTTPS site homepage: %q", pool.ID, pool.Website)
		}

		switch pool.Category {
		case "solo", "shared", "hybrid", "decentralized":
		default:
			t.Errorf("pool %q has unsupported category %q", pool.ID, pool.Category)
		}
		switch pool.Status {
		case "active", "inactive":
		default:
			t.Errorf("pool %q has unsupported status %q", pool.ID, pool.Status)
		}
		if len(pool.Products) == 0 {
			t.Errorf("pool %q has no payout product", pool.ID)
		}
		if len(pool.Endpoints) == 0 {
			t.Errorf("pool %q has no endpoints", pool.ID)
		}

		endpoints := make(map[string]struct{}, len(pool.Endpoints))
		for _, endpoint := range pool.Endpoints {
			if endpoint.Host == "" || strings.Contains(endpoint.Host, "://") {
				t.Errorf("pool %q has invalid endpoint host %q", pool.ID, endpoint.Host)
			}
			if endpoint.Port < 1 || endpoint.Port > 65535 {
				t.Errorf("pool %q has invalid endpoint port %d", pool.ID, endpoint.Port)
			}
			key := endpoint.Host + ":" + strconv.Itoa(endpoint.Port)
			if _, exists := endpoints[key]; exists {
				t.Errorf("pool %q has duplicate endpoint %s:%d", pool.ID, endpoint.Host, endpoint.Port)
			}
			endpoints[key] = struct{}{}
		}
	}
}

func TestObservationCompactionUsesWeeklyStartupGrace(t *testing.T) {
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	appender := &store.Appender{Path: path}
	if err := appender.Append([]model.Observation{
		{Version: model.ObservationVersion, ObservationID: "stale", ObservedAt: now.Add(-31 * 24 * time.Hour)},
		{Version: model.ObservationVersion, ObservationID: "recent", ObservedAt: now.Add(-time.Hour)},
	}); err != nil {
		t.Fatal(err)
	}
	startup, err := compactObservations(appender, now, true)
	if err != nil {
		t.Fatal(err)
	}
	if startup.Compacted {
		t.Fatalf("startup compacted a file less than one week beyond retention: %+v", startup)
	}
	weekly, err := compactObservations(appender, now, false)
	if err != nil {
		t.Fatal(err)
	}
	if !weekly.Compacted || weekly.Removed != 1 || weekly.Retained != 1 {
		t.Fatalf("weekly compaction=%+v, want one stale record removed", weekly)
	}
}
