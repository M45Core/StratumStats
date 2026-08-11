package probe

import (
	"testing"
	"time"
)

func TestFirstValidCoinbaseOnlyTemplateCountsAsArrival(t *testing.T) {
	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	block := &activeBlock{
		arrivals: map[string]time.Time{},
		empty:    map[string]bool{},
		tls:      map[string]bool{},
		invalid:  map[string]bool{},
		payout:   map[string]event{},
	}

	recordBlockEvent(block, event{poolID: "pool", at: started, verified: true})
	recordBlockEvent(block, event{poolID: "pool", at: started.Add(2 * time.Second), verified: true, hasTransactions: true})

	if got := block.arrivals["pool"]; !got.Equal(started) {
		t.Fatalf("arrival = %v, want first valid template at %v", got, started)
	}
	if !block.empty["pool"] {
		t.Fatal("coinbase-only first template was not retained as raw evidence")
	}
}

func TestInvalidTemplateDoesNotBecomeArrival(t *testing.T) {
	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	block := &activeBlock{
		arrivals: map[string]time.Time{},
		empty:    map[string]bool{},
		tls:      map[string]bool{},
		invalid:  map[string]bool{},
		payout:   map[string]event{},
	}

	recordBlockEvent(block, event{poolID: "pool", at: started, hasTransactions: true})
	recordBlockEvent(block, event{poolID: "pool", at: started.Add(time.Second), verified: true, hasTransactions: true})

	if got := block.arrivals["pool"]; !got.Equal(started.Add(time.Second)) {
		t.Fatalf("arrival = %v, want first valid template", got)
	}
	if !block.invalid["pool"] {
		t.Fatal("invalid template evidence was not retained")
	}
}

func TestFinalizedBlockCannotBeReopenedByLateJob(t *testing.T) {
	blocks := map[string]*activeBlock{}
	completed := map[string]bool{}
	configured := map[string]endpointTarget{
		"first": {poolID: "first", address: "first.example:3333"},
		"late":  {poolID: "late", address: "late.example:3333"},
	}
	started := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)

	first := activeBlockForEvent(blocks, completed, configured, event{poolID: "first", connectionID: "first", prevHash: "block", at: started})
	if first == nil {
		t.Fatal("initial block event did not open a window")
	}
	completed["block"] = true
	delete(blocks, "block")
	if late := activeBlockForEvent(blocks, completed, configured, event{poolID: "late", connectionID: "late", prevHash: "block", at: started.Add(20 * time.Second)}); late != nil {
		t.Fatal("late job reopened a finalized block window")
	}
}

func TestEveryConfiguredEndpointRemainsEligibleWhileDisconnected(t *testing.T) {
	configured := map[string]endpointTarget{
		"plain": {poolID: "pool", address: "pool.example:3333"},
		"tls":   {poolID: "pool", address: "pool.example:443", tls: true},
	}
	block := activeBlockForEvent(map[string]*activeBlock{}, map[string]bool{}, configured, event{poolID: "other", prevHash: "block", at: time.Now()})
	if len(block.eligible) != 2 || block.eligible["plain"].address == "" || !block.eligible["tls"].tls {
		t.Fatalf("eligible endpoints = %+v, want every configured endpoint", block.eligible)
	}
}
