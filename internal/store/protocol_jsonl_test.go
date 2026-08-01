package store

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func TestProtocolObservationJSONLRoundTrip(t *testing.T) {
	duration := 12.75
	want := model.Observation{
		Version:        model.ObservationVersion,
		RecordType:     model.RecordTypeProtocol,
		ObservedAt:     time.Unix(456, 0).UTC(),
		Vantage:        "test",
		PoolID:         "pool",
		Endpoint:       "pool.example:3333",
		ProtocolMethod: model.ProtocolSubscribe,
		DurationMS:     &duration,
		ResponseStatus: model.ProtocolStatusOK,
	}
	path := filepath.Join(t.TempDir(), "protocol.jsonl")
	if err := Append(path, []model.Observation{want}); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ProtocolMethod != want.ProtocolMethod || got[0].DurationMS == nil || *got[0].DurationMS != duration {
		t.Fatalf("got %+v", got)
	}
}
