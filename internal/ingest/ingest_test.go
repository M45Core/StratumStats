package ingest

import (
	"bytes"
	"compress/gzip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

func testPool() model.Pool {
	return model.Pool{ID: "pool", Name: "Pool", ProbeStatus: "compatible", Endpoints: []model.Endpoint{{Host: "pool.example", Port: 3333}}}
}

func testEnvelope(now time.Time) Envelope {
	duration := 12.5
	return Envelope{
		SchemaVersion: EnvelopeVersion, BatchID: "batch-1", RunID: "run-1",
		AgentVersion: "0.1.0", ConfigRevision: "sha256:" + strings.Repeat("a", 64),
		Region: "lax", Vantage: "us-west", MachineID: "machine-1",
		StartedAt: now.Add(-time.Minute), SentAt: now,
		Observations: []model.Observation{{
			Version: model.ObservationVersion, ObservationID: "run-1/1", RunID: "run-1",
			RecordType: model.RecordTypeProtocol, ObservedAt: now.Add(-30 * time.Second),
			PoolID: "pool", Endpoint: "pool.example:3333", ProtocolMethod: model.ProtocolConnect,
			DurationMS: &duration, ResponseStatus: model.ProtocolStatusOK,
		}},
	}
}

func signedRequest(t *testing.T, envelope Envelope, secret []byte, now time.Time) *http.Request {
	t.Helper()
	var body bytes.Buffer
	compressed := gzip.NewWriter(&body)
	if err := json.NewEncoder(compressed).Encode(envelope); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	timestamp := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(timestamp + "\n"))
	_, _ = mac.Write(body.Bytes())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(body.Bytes()))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("X-StratumStats-Key-ID", "current")
	request.Header.Set("X-StratumStats-Timestamp", timestamp)
	request.Header.Set("X-StratumStats-Signature", hex.EncodeToString(mac.Sum(nil)))
	return request
}

func TestReceiverAcceptsAuthenticatedBatchAndSetsProvenance(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	var appended []model.Observation
	receiver := Receiver{
		Pools: []model.Pool{testPool()}, Keys: map[string][]byte{"current": []byte("secret")},
		Now:    func() time.Time { return now },
		Append: func(observations []model.Observation) error { appended = append(appended, observations...); return nil },
	}
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedRequest(t, testEnvelope(now), []byte("secret"), now))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(appended) != 1 {
		t.Fatalf("appended=%d", len(appended))
	}
	got := appended[0]
	if got.Source != RemoteSource || got.Vantage != "us-west" || got.MachineID != "machine-1" ||
		got.AgentVersion != "0.1.0" || got.ConfigRevision == "" {
		t.Fatalf("provenance=%+v", got)
	}
}

func TestReceiverRejectsBadSignatureWithoutAppend(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	called := false
	receiver := Receiver{
		Pools: []model.Pool{testPool()}, Keys: map[string][]byte{"current": []byte("secret")},
		Now:    func() time.Time { return now },
		Append: func([]model.Observation) error { called = true; return nil },
	}
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedRequest(t, testEnvelope(now), []byte("wrong"), now))
	if response.Code != http.StatusUnauthorized || called {
		t.Fatalf("status=%d append=%t", response.Code, called)
	}
}

func TestReceiverRejectsWholeBatchWhenOneObservationIsInvalid(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	envelope := testEnvelope(now)
	invalid := envelope.Observations[0]
	invalid.ObservationID = "run-1/2"
	invalid.PoolID = "unknown"
	envelope.Observations = append(envelope.Observations, invalid)
	called := false
	receiver := Receiver{
		Pools: []model.Pool{testPool()}, Keys: map[string][]byte{"current": []byte("secret")},
		Now:    func() time.Time { return now },
		Append: func([]model.Observation) error { called = true; return nil },
	}
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedRequest(t, envelope, []byte("secret"), now))
	if response.Code != http.StatusUnprocessableEntity || called {
		t.Fatalf("status=%d append=%t body=%s", response.Code, called, response.Body.String())
	}
}

func TestReceiverRejectsStaleAuthentication(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	receiver := Receiver{
		Pools: []model.Pool{testPool()}, Keys: map[string][]byte{"current": []byte("secret")},
		Now:    func() time.Time { return now },
		Append: func([]model.Observation) error { return nil },
	}
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedRequest(t, testEnvelope(now), []byte("secret"), now.Add(-6*time.Minute)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestBuildProbeConfigIncludesOnlyCompatiblePoolsAndIsStable(t *testing.T) {
	pools := []model.Pool{
		testPool(),
		{ID: "private", ProbeStatus: "requires_credentials", Endpoints: []model.Endpoint{{Host: "private.example", Port: 1}}},
	}
	first, err := BuildProbeConfig(pools)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildProbeConfig(pools)
	if err != nil {
		t.Fatal(err)
	}
	if first.ConfigRevision != second.ConfigRevision || len(first.Pools) != 1 || first.Pools[0].ID != "pool" {
		t.Fatalf("config=%+v second revision=%q", first, second.ConfigRevision)
	}
}
