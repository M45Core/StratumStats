package ingest

import (
	"bytes"
	"compress/gzip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func testCoinbaseSource() *model.CoinbaseSource {
	workerScript, _ := hex.DecodeString("76a914111111111111111111111111111111111111111188ac")
	workerHash := sha256.Sum256(workerScript)
	return &model.CoinbaseSource{
		Coinbase1:   "0100000001" + strings.Repeat("00", 32) + "ffffffff0c03a1bb0d",
		Coinbase2:   "ffffffff01" + "205fa01200000000" + "19" + hex.EncodeToString(workerScript) + "00000000",
		ExtraNonce1: "01020304", ExtraNonce2Size: 4,
		WorkerScriptSHA256: hex.EncodeToString(workerHash[:]),
	}
}

func testBlockEnvelope(now time.Time) (Envelope, []model.Pool) {
	blockID := strings.Repeat("a", 64)
	first, second := now.Add(-30*time.Second), now.Add(-30*time.Second+25_500*time.Microsecond)
	connect := &model.ProtocolSample{ObservedAt: now.Add(-time.Minute), DurationMS: 12.5, ResponseStatus: model.ProtocolStatusOK}
	pools := []model.Pool{{
		ID: "pool", Name: "Pool",
		Endpoints: []model.Endpoint{{Host: "one.example", Port: 3333}, {Host: "two.example", Port: 443, TLS: true}},
	}}
	sample := &model.BlockSample{
		BlockID: blockID,
		EndpointSamples: []model.ForwardedEndpointSample{
			{PoolID: "pool", Endpoint: "one.example:3333", ReceivedAt: &first, Setup: &model.EndpointSetup{Connect: connect}, Coinbase: testCoinbaseSource()},
			{PoolID: "pool", Endpoint: "two.example:443", TLS: true, ReceivedAt: &second},
		},
	}
	return Envelope{
		SchemaVersion: BlockEnvelopeVersion, BatchID: "lax-" + blockID,
		ConfigRevision: "sha256:" + strings.Repeat("a", 64), Region: "lax", Sample: sample,
	}, pools
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

func testReceiver(now time.Time, pools []model.Pool, appended *[]model.Observation) Receiver {
	return Receiver{
		Pools: pools, Keys: map[string][]byte{"current": []byte("secret")},
		Now: func() time.Time { return now },
		Append: func(observations []model.Observation) error {
			*appended = append(*appended, observations...)
			return nil
		},
	}
}

func TestRegionVantagesMatchesDeployedNodes(t *testing.T) {
	want := map[string]string{
		"iad": "us-east", "fra": "europe", "lax": "us-west", "nrt": "japan", "sin": "singapore",
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

func TestReceiverAcceptsOneAtomicBlockSample(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	envelope, pools := testBlockEnvelope(now)
	var appended []model.Observation
	receiver := testReceiver(now, pools, &appended)
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedRequest(t, envelope, []byte("secret"), now))
	if response.Code != http.StatusAccepted || len(appended) != 1 {
		t.Fatalf("status=%d body=%s appended=%+v", response.Code, response.Body.String(), appended)
	}
	got := appended[0]
	if got.RecordType != model.RecordTypeBlockSample || got.Source != RemoteSource || got.Vantage != "us-west" ||
		got.RunID != envelope.BatchID || got.BlockID != envelope.Sample.BlockID || got.BlockHeight != 900_000 ||
		len(got.EndpointSamples) != 2 || len(got.EligibleEndpoints) != 2 {
		t.Fatalf("block sample=%+v", got)
	}
	if got.EndpointSamples[0].Coinbase == nil || !got.EndpointSamples[0].Coinbase.WorkerWalletInCoinbase || got.EndpointSamples[0].Coinbase.EstimatedPoolFeePct == nil || *got.EndpointSamples[0].Coinbase.EstimatedPoolFeePct != 0 {
		t.Fatalf("coinbase evidence=%+v", got.EndpointSamples[0].Coinbase)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"coinbase1"`)) || bytes.Contains(encoded, []byte(`"worker_script_sha256"`)) {
		t.Fatalf("persisted sample retained forwarded coinbase source: %s", encoded)
	}
	if got.EndpointSamples[0].Setup == nil || got.EndpointSamples[0].Setup.Connect == nil || got.EndpointSamples[0].Setup.Subscribe != nil {
		t.Fatalf("optional setup=%+v", got.EndpointSamples[0].Setup)
	}
	if got.EndpointSamples[0].OffsetMS == nil || *got.EndpointSamples[0].OffsetMS != 0 ||
		got.EndpointSamples[1].OffsetMS == nil || *got.EndpointSamples[1].OffsetMS != 25.5 {
		t.Fatalf("central offsets=%+v", got.EndpointSamples)
	}
	stored, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	for _, transient := range []string{`"received_at"`, `"job"`, `"coinbase1"`, `"machine_id"`, `"agent_version"`} {
		if bytes.Contains(stored, []byte(transient)) {
			t.Fatalf("transient field %s reached storage: %s", transient, stored)
		}
	}
}

func TestReceiverAcceptsSingleArrivalBlockSample(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	envelope, pools := testBlockEnvelope(now)
	envelope.Sample.EndpointSamples = envelope.Sample.EndpointSamples[:1]
	var appended []model.Observation
	receiver := testReceiver(now, pools, &appended)
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedRequest(t, envelope, []byte("secret"), now))
	if response.Code != http.StatusAccepted || len(appended) != 1 ||
		len(appended[0].EndpointSamples) != 1 || len(appended[0].EligibleEndpoints) != 2 {
		t.Fatalf("status=%d body=%s appended=%+v", response.Code, response.Body.String(), appended)
	}
}

func TestReceiverKeepsArrivalWhenCoinbaseCannotBeDecoded(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	envelope, pools := testBlockEnvelope(now)
	envelope.Sample.EndpointSamples[0].Coinbase.Coinbase1 = "not-hex"
	var appended []model.Observation
	receiver := testReceiver(now, pools, &appended)
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedRequest(t, envelope, []byte("secret"), now))
	if response.Code != http.StatusAccepted || len(appended) != 1 {
		t.Fatalf("status=%d body=%s appended=%+v", response.Code, response.Body.String(), appended)
	}
	if appended[0].BlockHeight != 0 || appended[0].EndpointSamples[0].OffsetMS == nil || appended[0].EndpointSamples[0].Coinbase != nil {
		t.Fatalf("malformed coinbase affected arrival sample: %+v", appended[0])
	}
}

func TestReceiverUsesFilteredRosterForAtomicBlockSample(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	envelope, pools := testBlockEnvelope(now)
	envelope.FilterContinents = true
	pools[0].Endpoints[0].Continent = "north-america"
	pools[0].Endpoints = append(pools[0].Endpoints, model.Endpoint{Host: "europe.example", Port: 3333, Continent: "europe"})
	var appended []model.Observation
	receiver := testReceiver(now, pools, &appended)
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedRequest(t, envelope, []byte("secret"), now))
	if response.Code != http.StatusAccepted || len(appended) != 1 || len(appended[0].EligibleEndpoints) != 2 {
		t.Fatalf("status=%d body=%s appended=%+v", response.Code, response.Body.String(), appended)
	}
	for _, endpoint := range appended[0].EligibleEndpoints {
		if endpoint.Endpoint == "europe.example:3333" {
			t.Fatalf("filtered endpoint reached roster: %+v", appended[0].EligibleEndpoints)
		}
	}
}

func TestReceiverRejectsUnknownOrEmptyEndpointSamples(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	for name, mutate := range map[string]func(*Envelope){
		"unknown": func(envelope *Envelope) {
			envelope.Sample.EndpointSamples = append(envelope.Sample.EndpointSamples, model.ForwardedEndpointSample{PoolID: "pool", Endpoint: "missing.example:3333"})
		},
		"empty": func(envelope *Envelope) {
			envelope.Sample.EndpointSamples[0].ReceivedAt = nil
			envelope.Sample.EndpointSamples[0].Setup = nil
		},
	} {
		t.Run(name, func(t *testing.T) {
			envelope, pools := testBlockEnvelope(now)
			mutate(&envelope)
			var appended []model.Observation
			receiver := testReceiver(now, pools, &appended)
			response := httptest.NewRecorder()
			receiver.ServeHTTP(response, signedRequest(t, envelope, []byte("secret"), now))
			if response.Code != http.StatusUnprocessableEntity || len(appended) != 0 {
				t.Fatalf("status=%d body=%s appended=%+v", response.Code, response.Body.String(), appended)
			}
		})
	}
}

func TestReceiverRejectsLegacyEnvelopeFields(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	envelope, pools := testBlockEnvelope(now)
	raw, err := json.Marshal(envelope)
	if err != nil {
		t.Fatal(err)
	}
	raw = bytes.Replace(raw, []byte(`"sample":`), []byte(`"run_id":"legacy","sample":`), 1)
	var body bytes.Buffer
	compressed := gzip.NewWriter(&body)
	_, _ = compressed.Write(raw)
	if err := compressed.Close(); err != nil {
		t.Fatal(err)
	}
	var appended []model.Observation
	receiver := testReceiver(now, pools, &appended)
	response := httptest.NewRecorder()
	receiver.ServeHTTP(response, signedBodyRequest(body.Bytes(), []byte("secret"), now))
	if response.Code != http.StatusBadRequest || len(appended) != 0 {
		t.Fatalf("status=%d body=%s appended=%+v", response.Code, response.Body.String(), appended)
	}
}

func TestReceiverRejectsOversizedRequests(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	receiver := Receiver{
		Keys: map[string][]byte{"current": []byte("secret")}, Now: func() time.Time { return now },
		Append: func([]model.Observation) error { return nil },
	}
	t.Run("compressed", func(t *testing.T) {
		response := httptest.NewRecorder()
		receiver.ServeHTTP(response, signedBodyRequest(make([]byte, maxCompressedBytes+1), []byte("secret"), now))
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})
	t.Run("decompressed", func(t *testing.T) {
		var body bytes.Buffer
		compressed := gzip.NewWriter(&body)
		_, _ = compressed.Write(make([]byte, maxDecompressedBytes+1))
		if err := compressed.Close(); err != nil {
			t.Fatal(err)
		}
		response := httptest.NewRecorder()
		receiver.ServeHTTP(response, signedBodyRequest(body.Bytes(), []byte("secret"), now))
		if response.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
		}
	})
}

func TestReceiverRejectsBadOrStaleAuthentication(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	envelope, pools := testBlockEnvelope(now)
	for _, test := range []struct {
		name     string
		secret   []byte
		signedAt time.Time
	}{
		{name: "bad-signature", secret: []byte("wrong"), signedAt: now},
		{name: "stale", secret: []byte("secret"), signedAt: now.Add(-6 * time.Minute)},
	} {
		t.Run(test.name, func(t *testing.T) {
			var appended []model.Observation
			receiver := testReceiver(now, pools, &appended)
			response := httptest.NewRecorder()
			receiver.ServeHTTP(response, signedRequest(t, envelope, test.secret, test.signedAt))
			if response.Code != http.StatusUnauthorized || len(appended) != 0 {
				t.Fatalf("status=%d appended=%+v", response.Code, appended)
			}
		})
	}
}

func TestReceiverRejectsReplayedBlock(t *testing.T) {
	now := time.Unix(1_800_000_000, 0).UTC()
	envelope, pools := testBlockEnvelope(now)
	appends := 0
	receiver := Receiver{
		Pools: pools, Keys: map[string][]byte{"current": []byte("secret")}, Now: func() time.Time { return now },
		Replays: NewReplayGuard(), Append: func([]model.Observation) error { appends++; return nil },
	}
	first := httptest.NewRecorder()
	receiver.ServeHTTP(first, signedRequest(t, envelope, []byte("secret"), now))
	second := httptest.NewRecorder()
	receiver.ServeHTTP(second, signedRequest(t, envelope, []byte("secret"), now))
	if first.Code != http.StatusAccepted || second.Code != http.StatusConflict || appends != 1 {
		t.Fatalf("first=%d second=%d appends=%d", first.Code, second.Code, appends)
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
	if first.ConfigRevision != second.ConfigRevision || len(first.Pools) != 1 || first.Pools[0].ID != "pool" || first.Pools[0].Endpoints[0].Continent != "europe" {
		t.Fatalf("config=%+v second revision=%q", first, second.ConfigRevision)
	}
}

func BenchmarkDecodeEnvelope(b *testing.B) {
	now := time.Date(2026, time.August, 15, 12, 0, 0, 0, time.UTC)
	envelope, _ := testBlockEnvelope(now)
	first := *envelope.Sample.EndpointSamples[0].ReceivedAt
	for index := range 100 {
		at := first.Add(time.Duration(index) * time.Millisecond)
		envelope.Sample.EndpointSamples = append(envelope.Sample.EndpointSamples, model.ForwardedEndpointSample{
			PoolID: "pool", Endpoint: fmt.Sprintf("pool-%d.example:3333", index), ReceivedAt: &at,
		})
	}
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	if err := json.NewEncoder(writer).Encode(envelope); err != nil {
		b.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		b.Fatal(err)
	}
	raw := compressed.Bytes()
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))
	for b.Loop() {
		if _, _, err := decodeEnvelope(raw); err != nil {
			b.Fatal(err)
		}
	}
}
