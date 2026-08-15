package ingest

import (
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestAllowedTargetsAcceptsCurrentAndUnexpiredPreviousRevision(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	current := []model.Pool{{ID: "current", Endpoints: []model.Endpoint{{Host: "current.example", Port: 3333}}}}
	previous := []model.Pool{{ID: "previous", Endpoints: []model.Endpoint{{Host: "previous.example", Port: 3333}}}}
	receiver := Receiver{
		PoolRevisions:  map[string][]model.Pool{"current-revision": current, "previous-revision": previous},
		RevisionExpiry: map[string]time.Time{"previous-revision": now.Add(time.Hour)},
	}
	pools, _, err := receiver.allowedTargets("previous-revision", now)
	if err != nil || !pools["previous"] {
		t.Fatalf("previous revision pools=%v err=%v", pools, err)
	}
	pools, _, err = receiver.allowedTargets("current-revision", now.Add(24*time.Hour))
	if err != nil || !pools["current"] {
		t.Fatalf("current revision pools=%v err=%v", pools, err)
	}
	if _, _, err := receiver.allowedTargets("previous-revision", now.Add(time.Hour)); err == nil {
		t.Fatal("expired previous revision accepted")
	}
	if _, _, err := receiver.allowedTargets("unknown", now); err == nil {
		t.Fatal("unknown revision accepted")
	}
}
