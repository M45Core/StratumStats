package probe

import (
	"context"
	"log"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

// watch keeps a pool endpoint represented through ordinary transient network
// failures. Backoff is bounded so a recovered endpoint rejoins promptly.
func watch(ctx context.Context, poolID string, endpoint model.Endpoint, out chan<- event) error {
	backoff := time.Second
	for {
		err := watchSession(ctx, poolID, endpoint, out)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		log.Printf("probe %s %s:%d disconnected: %v; retrying in %s", poolID, endpoint.Host, endpoint.Port, err, backoff)
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		backoff *= 2
		if backoff > time.Minute {
			backoff = time.Minute
		}
	}
}
