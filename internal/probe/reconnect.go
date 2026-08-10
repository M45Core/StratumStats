package probe

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
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
		if !shouldRetry(err) {
			return fmt.Errorf("not retrying after permanent rejection: %w", err)
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

func shouldRetry(err error) bool {
	return !errors.Is(err, errPoolRejected)
}
