package app

import "testing"

func TestRepositoryPoolRegistryLoadsAndConsolidates(t *testing.T) {
	cfg, err := loadConfig("../../config/pools.json")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(cfg.Pools), 33; got != want {
		t.Fatalf("pool count = %d, want %d", got, want)
	}

	pools := make(map[string]int, len(cfg.Pools))
	for i, pool := range cfg.Pools {
		pools[pool.ID] = i
	}
	for _, removed := range []string{"2miners_asia", "2miners_eu", "2miners_us", "ca_solohash", "ckpool_solo", "de_solohash", "demand", "uk_solohash"} {
		if _, ok := pools[removed]; ok {
			t.Errorf("obsolete or duplicate pool %q remains", removed)
		}
	}
	for _, canonical := range []string{"2miners_btc_solo", "ckpool", "solohash_co_uk"} {
		if _, ok := pools[canonical]; !ok {
			t.Errorf("canonical pool %q is missing", canonical)
		}
	}

	m45 := cfg.Pools[pools["m45core"]]
	for _, endpoint := range m45.Endpoints {
		if endpoint.Host == "eu.m45core.com" {
			t.Error("retired M45 EU endpoint remains")
		}
		if endpoint.Host == "tinyminer.m45core.com" && endpoint.Port != 3334 && endpoint.Port != 4334 {
			t.Errorf("stale TinyMiner port remains: %d", endpoint.Port)
		}
	}
	if got := cfg.Pools[pools["noderunners"]].Category; got != "solo" {
		t.Errorf("Noderunners category = %q, want solo", got)
	}
	if got := cfg.Pools[pools["public_pool"]].Category; got != "hybrid" {
		t.Errorf("Public Pool category = %q, want hybrid", got)
	}
	if got := cfg.Pools[pools["p2pool"]].Status; got != "inactive" {
		t.Errorf("P2Pool status = %q, want inactive", got)
	}
}
