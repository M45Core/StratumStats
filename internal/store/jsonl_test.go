package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestAppendAndLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	want := []model.Observation{{Version: 1, ObservedAt: time.Unix(123, 0).UTC(), Vantage: "test", BlockID: "sample", PoolID: "pool", Eligible: true, Arrived: true, OffsetMS: 12.5}}
	if err := Append(path, want); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PoolID != want[0].PoolID || got[0].OffsetMS != want[0].OffsetMS {
		t.Fatalf("got %+v", got)
	}
}

func TestLoadMissingFile(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "missing.jsonl"))
	if err != nil || len(got) != 0 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestAppenderSerializesConcurrentBatches(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	appender := &Appender{Path: path}
	const batches = 20
	errs := make(chan error, batches)
	for i := 0; i < batches; i++ {
		i := i
		go func() {
			errs <- appender.Append([]model.Observation{{Version: model.ObservationVersion, ObservationID: string(rune('a' + i)), PoolID: "pool"}})
		}()
	}
	for i := 0; i < batches; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != batches {
		t.Fatalf("records=%d, want %d", len(got), batches)
	}
}
