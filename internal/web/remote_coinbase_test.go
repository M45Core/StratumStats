package web

import (
	"bytes"
	"compress/gzip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/ingest"
	"github.com/M45Core/StratumStats/internal/model"
)

func coinbaseOutput(value uint64, script []byte) string {
	var encodedValue [8]byte
	binary.LittleEndian.PutUint64(encodedValue[:], value)
	return hex.EncodeToString(encodedValue[:]) + hex.EncodeToString([]byte{byte(len(script))}) + hex.EncodeToString(script)
}

func TestRemoteCoinbaseSourcePopulatesDisplayedDashboardData(t *testing.T) {
	const (
		coinbaseTotalSats = uint64(312_500_000)
		workerPayoutSats  = uint64(306_250_000)
		poolFeeSats       = uint64(6_250_000)
	)
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	receivedAt := now.Add(-30 * time.Second)
	workerScript, _ := hex.DecodeString("76a914111111111111111111111111111111111111111188ac")
	workerHash := sha256.Sum256(workerScript)
	nonWorkerScript := []byte{0x51}
	source := &model.CoinbaseSource{
		Coinbase1:   "0100000001" + strings.Repeat("00", 32) + "ffffffff0c03a1bb0d",
		Coinbase2:   "ffffffff02" + coinbaseOutput(workerPayoutSats, workerScript) + coinbaseOutput(poolFeeSats, nonWorkerScript) + "00000000",
		ExtraNonce1: "01020304", ExtraNonce2Size: 4,
		WorkerScriptSHA256: hex.EncodeToString(workerHash[:]),
	}
	pool := model.Pool{
		ID: "solo", Name: "Solo", Category: "solo",
		Endpoints: []model.Endpoint{{Host: "solo.example", Port: 3333}},
	}
	blockID := strings.Repeat("a", 64)
	envelope := ingest.Envelope{
		SchemaVersion:  ingest.BlockEnvelopeVersion,
		BatchID:        "lax-" + blockID,
		ConfigRevision: "sha256:" + strings.Repeat("a", 64),
		Region:         "lax",
		Sample: &model.BlockSample{BlockID: blockID, EndpointSamples: []model.ForwardedEndpointSample{{
			PoolID: "solo", Endpoint: "solo.example:3333", ReceivedAt: &receivedAt, Coinbase: source,
		}}},
	}
	var compressed bytes.Buffer
	gzipWriter := gzip.NewWriter(&compressed)
	if err := json.NewEncoder(gzipWriter).Encode(envelope); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	secret := []byte("remote-coinbase-test-secret")
	timestamp := strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(timestamp + "\n"))
	_, _ = mac.Write(compressed.Bytes())
	request := httptest.NewRequest(http.MethodPost, "/api/v1/ingest", bytes.NewReader(compressed.Bytes()))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("X-StratumStats-Key-ID", "test")
	request.Header.Set("X-StratumStats-Timestamp", timestamp)
	request.Header.Set("X-StratumStats-Signature", hex.EncodeToString(mac.Sum(nil)))

	var observations []model.Observation
	receiver := ingest.Receiver{
		Pools: []model.Pool{pool}, Keys: map[string][]byte{"test": secret}, Now: func() time.Time { return now },
		Append: func(values []model.Observation) error { observations = append(observations, values...); return nil },
	}
	ingestResponse := httptest.NewRecorder()
	receiver.ServeHTTP(ingestResponse, request)
	if ingestResponse.Code != http.StatusAccepted || len(observations) != 1 {
		t.Fatalf("ingest status=%d body=%s observations=%+v", ingestResponse.Code, ingestResponse.Body.String(), observations)
	}
	persisted, err := json.Marshal(observations[0])
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(persisted, []byte(`"coinbase1"`)) || bytes.Contains(persisted, []byte(`"worker_script_sha256"`)) {
		t.Fatalf("raw coinbase source reached storage: %s", persisted)
	}

	handler, err := (Server{Pools: []model.Pool{pool}, Load: func() ([]model.Observation, error) { return observations, nil }, Demo: true}).Handler()
	if err != nil {
		t.Fatal(err)
	}
	payload := dashboardPayload(t, handler, "/dashboard-data?vantage=us-west")
	if payload.Snapshot.LatestBlockHeight != 900_000 || len(payload.NormalPools) != 1 {
		t.Fatalf("height=%d paid pools=%+v", payload.Snapshot.LatestBlockHeight, payload.NormalPools)
	}
	got := payload.NormalPools[0]
	if got.LatestPoolFeePct == nil || *got.LatestPoolFeePct != 2 || got.CoinbaseSamples != 1 ||
		got.LatestCoinbaseTotalSats != coinbaseTotalSats || got.LatestCoinbaseOutputCount != 2 || len(got.LatestPayoutDestinations) != 1 ||
		got.LatestPayoutDestinations[0].ValueSats != poolFeeSats {
		t.Fatalf("dashboard pool=%+v", got)
	}
}
