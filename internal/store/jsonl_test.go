package store

import (
	"bytes"
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
	want := []model.Observation{{Version: model.ObservationVersion, ObservedAt: time.Unix(123, 0).UTC(), Vantage: "test", BlockID: "sample", PoolID: "pool", Eligible: true, Arrived: true, OffsetMS: 12.5}}
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

func TestLoadSinceDiscardsOlderObservations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	cutoff := time.Date(2026, 7, 11, 12, 0, 0, 0, time.UTC)
	observations := []model.Observation{
		{Version: model.ObservationVersion, ObservationID: "old", ObservedAt: cutoff.Add(-time.Nanosecond)},
		{Version: model.ObservationVersion, ObservationID: "boundary", ObservedAt: cutoff},
		{Version: model.ObservationVersion, ObservationID: "recent", ObservedAt: cutoff.Add(time.Hour)},
	}
	if err := Append(path, observations); err != nil {
		t.Fatal(err)
	}
	got, err := LoadSince(path, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ObservationID != "boundary" || got[1].ObservationID != "recent" {
		t.Fatalf("retained observations=%+v, want boundary and recent", got)
	}
}

func TestLoadIgnoresOlderObservationVersions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations-v9.jsonl")
	observations := []model.Observation{
		{Version: model.ObservationVersion - 1, ObservationID: "old-schema", ObservedAt: time.Now()},
		{Version: model.ObservationVersion, ObservationID: "current", ObservedAt: time.Now()},
	}
	if err := Append(path, observations); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ObservationID != "current" {
		t.Fatalf("loaded observations = %+v, want current schema only", got)
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

func TestAppenderCompactsAtomicallyToRetentionCutoff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	cutoff := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	observations := []model.Observation{
		{Version: model.ObservationVersion, ObservationID: "old", ObservedAt: cutoff.Add(-time.Nanosecond)},
		{Version: model.ObservationVersion - 1, ObservationID: "old-schema", ObservedAt: cutoff.Add(time.Minute)},
		{Version: model.ObservationVersion, ObservationID: "boundary", ObservedAt: cutoff},
		{Version: model.ObservationVersion, ObservationID: "recent", ObservedAt: cutoff.Add(time.Hour)},
	}
	if err := Append(path, observations); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0640); err != nil {
		t.Fatal(err)
	}
	appender := &Appender{Path: path}
	result, err := appender.CompactBefore(cutoff, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Compacted || result.Removed != 2 || result.Retained != 2 {
		t.Fatalf("compaction result=%+v, want compacted with 2 removed and 2 retained", result)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ObservationID != "boundary" || got[1].ObservationID != "recent" {
		t.Fatalf("compacted observations=%+v", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0640 {
		t.Fatalf("compacted permissions=%04o, want 0640", info.Mode().Perm())
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".observations.jsonl.compact-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files=%v err=%v", matches, err)
	}
}

func TestAppenderCompactionWaitsForOldestTrigger(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	now := time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC)
	if err := Append(path, []model.Observation{{Version: model.ObservationVersion, ObservationID: "stale", ObservedAt: now.Add(-31 * 24 * time.Hour)}}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&Appender{Path: path}).CompactBefore(now.Add(-30*24*time.Hour), now.Add(-37*24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if result.Compacted || !bytes.Equal(before, after) {
		t.Fatalf("premature compaction result=%+v before=%q after=%q", result, before, after)
	}
}

func TestAppenderCompactionPreservesOriginalOnInvalidJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "observations.jsonl")
	old := model.Observation{Version: model.ObservationVersion, ObservationID: "old", ObservedAt: time.Unix(1, 0).UTC()}
	if err := Append(path, []model.Observation{old}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("not-json\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result, err := (&Appender{Path: path}).CompactBefore(time.Unix(2, 0).UTC(), time.Unix(2, 0).UTC())
	if err == nil || result.Compacted {
		t.Fatalf("invalid JSON compaction result=%+v err=%v", result, err)
	}
	after, readErr := os.ReadFile(path)
	if readErr != nil || !bytes.Equal(before, after) {
		t.Fatalf("original changed after failure: readErr=%v before=%q after=%q", readErr, before, after)
	}
}
