package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestAppendAndLoad(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private-data")
	path := filepath.Join(directory, "observations.jsonl")
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
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if got := directoryInfo.Mode().Perm(); got != 0700 {
		t.Fatalf("data directory permissions = %04o, want 0700", got)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := fileInfo.Mode().Perm(); got != 0600 {
		t.Fatalf("observation permissions = %04o, want 0600", got)
	}
}

func TestAppendNeverStoresWorkerDestination(t *testing.T) {
	const workerAddress = "12ZEw5Hcv1hTb6YUQJ69y1V7uhcoDz92PH"
	const workerScript = "76a914111111111111111111111111111111111111111188ac"
	const publicAddress = "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	observation := model.Observation{CoinbaseOutputs: []model.CoinbaseOutput{
		{ValueSats: 99, ScriptPubKey: workerScript, Address: workerAddress, ScriptType: "p2pkh", Worker: true},
		{ValueSats: 1, ScriptPubKey: "51", Address: publicAddress, ScriptType: "unknown"},
	}}
	if err := Append(path, []model.Observation{observation}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{workerAddress, workerScript, "\"worker\":true"} {
		if strings.Contains(string(raw), private) {
			t.Fatalf("JSONL exposed private worker destination %q: %s", private, raw)
		}
	}
	if !strings.Contains(string(raw), publicAddress) {
		t.Fatalf("JSONL omitted public non-worker destination: %s", raw)
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
