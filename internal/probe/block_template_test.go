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
