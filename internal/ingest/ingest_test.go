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

	"github.com/M45Core/StratumStats/internal/model"
)

func testPool() model.Pool {
	return model.Pool{ID: "pool", Name: "Pool", Endpoints: []model.Endpoint{{Host: "pool.example", Port: 3333}}}
}

func TestRegionVantagesMatchesDeployedNodes(t *testing.T) {
	want := map[string]string{
		"iad": "us-east",
		"fra": "europe",
		"lax": "us-west",
		"nrt": "japan",
		"sin": "singapore",
	}
	if len(RegionVantages) != len(want) {
		t.Fatalf("RegionVantages=%v", RegionVantages)
	}
	for region, vantage := range want {
		if RegionVantages[region] != vantage {
			t.Errorf("RegionVantages[%q]=%q, want %q", region, RegionVantages[region], vantage)
		}
	}
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
	return signedBodyRequest(body.Bytes(), secret, now)
}

func signedBodyRequest(body, secret []byte, now time.Time) *http.Request {
	timestamp := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(timestamp + "\n"))
	_, _ = mac.Write(body)
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("X-StratumStats-Key-ID", "current")
	request.Header.Set("X-StratumStats-Timestamp", timestamp)
	request.Header.Set("X-StratumStats-Signature", hex.EncodeToString(mac.Sum(nil)))
	return request
}

func TestReceiverRejectsOversizedCompressedRequest(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	receiver := Receiver{
		Keys:   map[string][]byte{"current": []byte("secret")},
		Now:    func() time.Time { return now },
		Append: func([]model.Observation) error { return nil },
	}
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedBodyRequest(make([]byte, maxCompressedBytes+1), []byte("secret"), now))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestReceiverRejectsOversizedDecompressedRequest(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	var body bytes.Buffer
	compressed := gzip.NewWriter(&body)
	if _, err := compressed.Write(make([]byte, maxDecompressedBytes+1)); err != nil {
		t.Fatal(err)
	}
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	receiver := Receiver{
		Keys:   map[string][]byte{"current": []byte("secret")},
		Now:    func() time.Time { return now },
		Append: func([]model.Observation) error { return nil },
	}
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedBodyRequest(body.Bytes(), []byte("secret"), now))
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
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

func TestReceiverAppendsProbeRunMarkerAfterMeasurements(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	envelope := testEnvelope(now)
	started := envelope.StartedAt
	summary := model.Observation{
		Version: model.ObservationVersion, ObservationID: "run-1/summary", RunID: "run-1",
		RecordType: model.RecordTypeProbeRun, ObservedAt: now, RunStartedAt: &started,
		RunStatus: "ok", ConfiguredEndpoints: 1, SuccessfulSessions: 1,
	}
	envelope.Observations = append([]model.Observation{summary}, envelope.Observations...)
	var appended []model.Observation
	receiver := Receiver{
		Pools: []model.Pool{testPool()}, Keys: map[string][]byte{"current": []byte("secret")},
		Now:    func() time.Time { return now },
		Append: func(observations []model.Observation) error { appended = append(appended, observations...); return nil },
	}
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedRequest(t, envelope, []byte("secret"), now))
	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if len(appended) != 2 || appended[0].RecordType != model.RecordTypeProtocol || appended[1].RecordType != model.RecordTypeProbeRun {
		t.Fatalf("append order=%+v, want measurements before run marker", appended)
	}
}

func TestReceiverAcceptsGermanyProbe(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	var appended []model.Observation
	receiver := Receiver{
		Pools: []model.Pool{testPool()}, Keys: map[string][]byte{"current": []byte("secret")},
		Now:    func() time.Time { return now },
		Append: func(observations []model.Observation) error { appended = append(appended, observations...); return nil },
	}
	envelope := testEnvelope(now)
	envelope.Region, envelope.Vantage = "fra", "europe"
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedRequest(t, envelope, []byte("secret"), now))
	if response.Code != http.StatusAccepted || len(appended) != 1 || appended[0].Vantage != "europe" {
		t.Fatalf("status=%d observations=%+v", response.Code, appended)
	}
}

func TestReceiverAcceptsIADProbe(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	var appended []model.Observation
	receiver := Receiver{
		Pools: []model.Pool{testPool()}, Keys: map[string][]byte{"current": []byte("secret")},
		Now:    func() time.Time { return now },
		Append: func(observations []model.Observation) error { appended = append(appended, observations...); return nil },
	}
	envelope := testEnvelope(now)
	envelope.Region, envelope.Vantage = "iad", "us-east"
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedRequest(t, envelope, []byte("secret"), now))
	if response.Code != http.StatusAccepted || len(appended) != 1 || appended[0].Vantage != "us-east" {
		t.Fatalf("status=%d observations=%+v", response.Code, appended)
	}
}

func TestReceiverAcceptsAsiaProbes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	for _, test := range []struct {
		region  string
		vantage string
	}{
		{region: "nrt", vantage: "japan"},
		{region: "sin", vantage: "singapore"},
	} {
		t.Run(test.region, func(t *testing.T) {
			var appended []model.Observation
			receiver := Receiver{
				Pools: []model.Pool{testPool()}, Keys: map[string][]byte{"current": []byte("secret")},
				Now:    func() time.Time { return now },
				Append: func(observations []model.Observation) error { appended = append(appended, observations...); return nil },
			}
			envelope := testEnvelope(now)
			envelope.Region, envelope.Vantage = test.region, test.vantage
			response := httptest.NewRecorder()
			receiver.ServeHTTP(response, signedRequest(t, envelope, []byte("secret"), now))
			if response.Code != http.StatusAccepted || len(appended) != 1 || appended[0].Vantage != test.vantage {
				t.Fatalf("status=%d observations=%+v", response.Code, appended)
			}
		})
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

func TestReceiverRejectsReplayedBatchWithoutAppendingAgain(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	appends := 0
	receiver := Receiver{
		Pools: []model.Pool{testPool()}, Keys: map[string][]byte{"current": []byte("secret")},
		Now:     func() time.Time { return now },
		Replays: NewReplayGuard(),
		Append:  func([]model.Observation) error { appends++; return nil },
	}
	first := httptest.NewRecorder()
	receiver.ServeHTTP(first, signedRequest(t, testEnvelope(now), []byte("secret"), now))
	second := httptest.NewRecorder()
	receiver.ServeHTTP(second, signedRequest(t, testEnvelope(now), []byte("secret"), now))
	if first.Code != http.StatusAccepted || second.Code != http.StatusConflict || appends != 1 {
		t.Fatalf("first=%d second=%d appends=%d", first.Code, second.Code, appends)
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

func TestBuildProbeConfigIncludesConfiguredPoolsAndIsStable(t *testing.T) {
	pools := []model.Pool{
		{ID: "pool", Name: "Pool", Endpoints: []model.Endpoint{{Host: "pool.example", Port: 3333, Region: "Europe"}}},
		{ID: "empty"},
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
	if got := first.Pools[0].Endpoints[0].Continent; got != "europe" {
		t.Fatalf("continent = %q, want europe", got)
	}
}

func TestValidateCoinbaseObservationRequiresBalancedBoundedEvidence(t *testing.T) {
	fee := 1.5
	valid := model.Observation{
		Arrived: true, CoinbaseAnalyzed: true, WorkerWalletInCoinbase: true,
		CoinbaseTotalSats: 10_000, WorkerPayoutSats: 9_850, EstimatedPoolFeePct: &fee,
		CoinbaseOutputCount: 3,
		CoinbaseOutputs: []model.CoinbaseOutput{
			{ValueSats: 150, ScriptPubKey: "0014751e76e8199196d454941c45d1b3a323f1433bd6", Address: "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4", ScriptType: "p2wpkh"},
		},
	}
	if err := validateCoinbaseObservation(valid); err != nil {
		t.Fatalf("valid coinbase evidence rejected: %v", err)
	}

	privateDestination := valid
	privateDestination.CoinbaseOutputs = append([]model.CoinbaseOutput{{
		ValueSats: 9_850, ScriptPubKey: "76a914111111111111111111111111111111111111111188ac",
		Address: "12ZEw5Hcv1hTb6YUQJ69y1V7uhcoDz92PH", ScriptType: "p2pkh", Worker: true,
	}}, privateDestination.CoinbaseOutputs...)
	if err := validateCoinbaseObservation(privateDestination); err == nil {
		t.Fatal("retained private worker destination accepted")
	}

	zeroFee := 0.0
	allWorker := valid
	allWorker.CoinbaseTotalSats = 9_850
	allWorker.WorkerPayoutSats = 9_850
	allWorker.EstimatedPoolFeePct = &zeroFee
	allWorker.CoinbaseOutputCount = 1
	allWorker.CoinbaseOutputs = nil
	if err := validateCoinbaseObservation(allWorker); err != nil {
		t.Fatalf("private-only coinbase evidence rejected: %v", err)
	}

	unbalanced := valid
	unbalanced.CoinbaseTotalSats--
	if err := validateCoinbaseObservation(unbalanced); err == nil {
		t.Fatal("unbalanced coinbase evidence accepted")
	}

	tooMany := valid
	tooMany.CoinbaseOutputs = make([]model.CoinbaseOutput, maxRetainedCoinbaseOutputs+1)
	if err := validateCoinbaseObservation(tooMany); err == nil {
		t.Fatal("oversized retained output list accepted")
	}
}
