package app

import "testing"

func TestRepositoryPoolRegistryLoadsAndConsolidates(t *testing.T) {
	cfg, err := loadConfig("../../config/pools.json")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := len(cfg.Pools), 36; got != want {
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
	for _, canonical := range []string{"2miners_btc_solo", "ckpool", "m45core", "noderunners", "p2pool", "public_pool", "public_pool_pplns", "blitzpool_pplns", "letsmineit_prop", "solohash_co_uk"} {
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
	if got := cfg.Pools[pools["public_pool"]].Category; got != "solo" {
		t.Errorf("Public Pool Solo category = %q, want solo", got)
	}
	if got := cfg.Pools[pools["public_pool"]].Name; got != "Public Pool — Solo" {
		t.Errorf("Public Pool Solo name = %q", got)
	}
	publicPPLNS := cfg.Pools[pools["public_pool_pplns"]]
	if publicPPLNS.Category != "shared" || len(publicPPLNS.Products) != 1 || publicPPLNS.Products[0] != "PPLNS" || publicPPLNS.Endpoints[0].Port != 13333 {
		t.Errorf("Public Pool PPLNS product record is incorrect: %+v", publicPPLNS)
	}
	letsMinePROP := cfg.Pools[pools["letsmineit_prop"]]
	if letsMinePROP.Category != "shared" || len(letsMinePROP.Products) != 1 || letsMinePROP.Products[0] != "PROP" || len(letsMinePROP.Endpoints) != 6 || letsMinePROP.Endpoints[0].Port != 3432 {
		t.Errorf("LetsMine PROP product record is incorrect: %+v", letsMinePROP)
	}
	if got := cfg.Pools[pools["p2pool"]].Status; got != "inactive" {
		t.Errorf("P2Pool status = %q, want inactive", got)
	}
}
