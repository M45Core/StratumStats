// Package ingest defines the authenticated contract used by remote probes.
package ingest

import (
	"bytes"
	"compress/gzip"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

const (
	EnvelopeVersion            = 1
	maxCompressedBytes         = 256 << 10
	maxDecompressedBytes       = 1 << 20
	maxObservations            = 500
	maxRunDuration             = 15 * time.Minute
	maxRequestClockSkew        = 5 * time.Minute
	maxRetainedCoinbaseOutputs = model.MaxRetainedCoinbaseOutputs
	maxRetainedScriptHexLength = model.MaxRetainedCoinbaseScriptBytes * 2
	RemoteSource               = "fly-scheduled"
)

var RegionVantages = map[string]string{
	"lax": "us-west",
	"dfw": "us-central",
	"iad": "us-east",
}

type Envelope struct {
	SchemaVersion  int                 `json:"schema_version"`
	BatchID        string              `json:"batch_id"`
	RunID          string              `json:"run_id"`
	AgentVersion   string              `json:"agent_version"`
	ConfigRevision string              `json:"config_revision"`
	Region         string              `json:"region"`
	Vantage        string              `json:"vantage"`
	MachineID      string              `json:"machine_id"`
	StartedAt      time.Time           `json:"started_at"`
	SentAt         time.Time           `json:"sent_at"`
	Observations   []model.Observation `json:"observations"`
}

type acceptedResponse struct {
	BatchID  string `json:"batch_id"`
	Accepted int    `json:"accepted"`
}

type Receiver struct {
	Pools  []model.Pool
	Keys   map[string][]byte
	Append func([]model.Observation) error
	Now    func() time.Time
}

func (receiver Receiver) ServeHTTP(w http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if receiver.Append == nil {
		http.Error(w, "ingestion unavailable", http.StatusServiceUnavailable)
		return
	}
	now := time.Now().UTC()
	if receiver.Now != nil {
		now = receiver.Now().UTC()
	}
	raw, status, err := receiver.authenticate(request, now)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	envelope, status, err := decodeEnvelope(raw)
	if err != nil {
		http.Error(w, err.Error(), status)
		return
	}
	observations, err := receiver.validate(envelope, now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if err := receiver.Append(observations); err != nil {
		http.Error(w, "append failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(acceptedResponse{BatchID: envelope.BatchID, Accepted: len(observations)})
}

func (receiver Receiver) authenticate(request *http.Request, now time.Time) ([]byte, int, error) {
	contentType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || contentType != "application/json" {
		return nil, http.StatusUnsupportedMediaType, errors.New("application/json content type required")
	}
	if request.Header.Get("Content-Encoding") != "gzip" {
		return nil, http.StatusUnsupportedMediaType, errors.New("gzip content encoding required")
	}
	keyID := request.Header.Get("X-StratumStats-Key-ID")
	secret, ok := receiver.Keys[keyID]
	if !ok || keyID == "" {
		return nil, http.StatusUnauthorized, errors.New("invalid authentication")
	}
	timestampText := request.Header.Get("X-StratumStats-Timestamp")
	timestamp, err := strconv.ParseInt(timestampText, 10, 64)
	if err != nil {
		return nil, http.StatusUnauthorized, errors.New("invalid authentication")
	}
	requestTime := time.Unix(timestamp, 0)
	if delta := now.Sub(requestTime); delta < -maxRequestClockSkew || delta > maxRequestClockSkew {
		return nil, http.StatusUnauthorized, errors.New("stale authentication")
	}
	raw, err := io.ReadAll(io.LimitReader(request.Body, maxCompressedBytes+1))
	if err != nil {
		return nil, http.StatusBadRequest, errors.New("cannot read request")
	}
	if len(raw) > maxCompressedBytes {
		return nil, http.StatusRequestEntityTooLarge, errors.New("compressed request too large")
	}
	provided, err := hex.DecodeString(request.Header.Get("X-StratumStats-Signature"))
	if err != nil || len(provided) != sha256.Size {
		return nil, http.StatusUnauthorized, errors.New("invalid authentication")
	}
	mac := hmac.New(sha256.New, secret)
	_, _ = io.WriteString(mac, timestampText+"\n")
	_, _ = mac.Write(raw)
	if !hmac.Equal(provided, mac.Sum(nil)) {
		return nil, http.StatusUnauthorized, errors.New("invalid authentication")
	}
	return raw, 0, nil
}

func decodeEnvelope(raw []byte) (Envelope, int, error) {
	compressed, err := gzip.NewReader(bytes.NewReader(raw))
	if err != nil {
		return Envelope{}, http.StatusBadRequest, errors.New("invalid gzip body")
	}
	defer compressed.Close()
	body, err := io.ReadAll(io.LimitReader(compressed, maxDecompressedBytes+1))
	if err != nil {
		return Envelope{}, http.StatusBadRequest, errors.New("cannot decompress body")
	}
	if len(body) > maxDecompressedBytes {
		return Envelope{}, http.StatusRequestEntityTooLarge, errors.New("request too large")
	}
	var envelope Envelope
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		return Envelope{}, http.StatusBadRequest, fmt.Errorf("invalid envelope: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return Envelope{}, http.StatusBadRequest, errors.New("invalid trailing data")
	}
	return envelope, 0, nil
}

func (receiver Receiver) validate(envelope Envelope, now time.Time) ([]model.Observation, error) {
	if envelope.SchemaVersion != EnvelopeVersion {
		return nil, errors.New("unsupported envelope version")
	}
	if !validID(envelope.BatchID, 128) || !validID(envelope.RunID, 128) ||
		!validID(envelope.MachineID, 128) || !validVersion(envelope.AgentVersion) {
		return nil, errors.New("invalid envelope identity")
	}
	vantage, ok := RegionVantages[envelope.Region]
	if !ok || envelope.Vantage != vantage {
		return nil, errors.New("invalid region or vantage")
	}
	if !validRevision(envelope.ConfigRevision) {
		return nil, errors.New("invalid configuration revision")
	}
	if envelope.StartedAt.IsZero() || envelope.SentAt.IsZero() ||
		envelope.SentAt.Before(envelope.StartedAt) ||
		envelope.SentAt.Sub(envelope.StartedAt) > maxRunDuration ||
		envelope.SentAt.After(now.Add(maxRequestClockSkew)) {
		return nil, errors.New("invalid run interval")
	}
	if len(envelope.Observations) == 0 || len(envelope.Observations) > maxObservations {
		return nil, errors.New("invalid observation count")
	}
	pools, endpoints := receiver.allowedTargets()
	seen := make(map[string]bool, len(envelope.Observations))
	observations := make([]model.Observation, len(envelope.Observations))
	for index, original := range envelope.Observations {
		observation := original
		if observation.Version != model.ObservationVersion ||
			!validID(observation.ObservationID, 192) ||
			observation.RunID != envelope.RunID ||
			seen[observation.ObservationID] {
			return nil, fmt.Errorf("invalid observation %d identity", index)
		}
		seen[observation.ObservationID] = true
		if observation.ObservedAt.Before(envelope.StartedAt) ||
			observation.ObservedAt.After(envelope.SentAt.Add(time.Second)) {
			return nil, fmt.Errorf("observation %d is outside run interval", index)
		}
		if err := validateObservation(observation, pools, endpoints); err != nil {
			return nil, fmt.Errorf("invalid observation %d: %w", index, err)
		}
		if observation.RecordType == model.RecordTypeProbeRun && !observation.RunStartedAt.Equal(envelope.StartedAt) {
			return nil, fmt.Errorf("observation %d has mismatched run start", index)
		}
		observation.Source = RemoteSource
		observation.Vantage = vantage
		observation.RunID = envelope.RunID
		observation.MachineID = envelope.MachineID
		observation.AgentVersion = envelope.AgentVersion
		observation.ConfigRevision = envelope.ConfigRevision
		observations[index] = observation
	}
	return observations, nil
}

type endpointIdentity struct {
	poolID, address string
	tls             bool
}

func (receiver Receiver) allowedTargets() (map[string]bool, map[endpointIdentity]bool) {
	pools := make(map[string]bool)
	endpoints := make(map[endpointIdentity]bool)
	for _, pool := range receiver.Pools {
		if pool.ProbeStatus != "compatible" {
			continue
		}
		pools[pool.ID] = true
		for _, endpoint := range pool.Endpoints {
			address := net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))
			endpoints[endpointIdentity{poolID: pool.ID, address: address, tls: endpoint.TLS}] = true
		}
	}
	return pools, endpoints
}

func validateObservation(observation model.Observation, pools map[string]bool, endpoints map[endpointIdentity]bool) error {
	if observation.ObservedAt.IsZero() {
		return errors.New("missing observation time")
	}
	if observation.DurationMS != nil && (!finite(*observation.DurationMS) || *observation.DurationMS < 0) {
		return errors.New("invalid duration")
	}
	if !finite(observation.OffsetMS) || observation.OffsetMS < 0 {
		return errors.New("invalid offset")
	}
	if err := validateCoinbaseObservation(observation); err != nil {
		return err
	}
	switch observation.RecordType {
	case model.RecordTypeProtocol:
		if !pools[observation.PoolID] {
			return errors.New("unknown pool")
		}
		if !endpoints[endpointIdentity{poolID: observation.PoolID, address: observation.Endpoint, tls: observation.TLS}] {
			return errors.New("unknown endpoint")
		}
		if !oneOf(observation.ProtocolMethod, model.ProtocolConnect, model.ProtocolTLSHandshake, model.ProtocolSubscribe, model.ProtocolAuthorize, model.ProtocolPing) ||
			!oneOf(observation.ResponseStatus, model.ProtocolStatusOK, model.ProtocolStatusRejected, model.ProtocolStatusUnsupported, model.ProtocolStatusTimeout, model.ProtocolStatusError) ||
			observation.DurationMS == nil || observation.BlockID != "" {
			return errors.New("invalid protocol result")
		}
	case model.RecordTypeProbeRun:
		if observation.PoolID != "" || observation.BlockID != "" ||
			observation.RunStartedAt == nil || observation.RunStartedAt.IsZero() ||
			!oneOf(observation.RunStatus, "ok", "partial", "error") ||
			observation.ConfiguredEndpoints < 0 || observation.SuccessfulSessions < 0 ||
			observation.AcceptedBlocks < 0 || observation.UploadedObservations < 0 ||
			observation.DroppedObservations < 0 {
			return errors.New("invalid probe run")
		}
	case "":
		if !pools[observation.PoolID] || observation.BlockID == "" || !observation.Eligible ||
			(!observation.Arrived && observation.OffsetMS != 0) {
			return errors.New("invalid block observation")
		}
	default:
		return errors.New("unknown record type")
	}
	return nil
}

func validateCoinbaseObservation(observation model.Observation) error {
	hasEvidence := observation.CoinbaseAnalyzed || observation.WorkerWalletInCoinbase || observation.CoinbaseTotalSats != 0 ||
		observation.WorkerPayoutSats != 0 || len(observation.CoinbaseOutputs) != 0 || observation.CoinbaseOutputCount != 0 ||
		observation.CoinbaseOutputsTruncated || observation.CoinbaseOmittedSats != 0 || observation.EstimatedPoolFeePct != nil
	if observation.RecordType != "" {
		if hasEvidence {
			return errors.New("coinbase evidence on non-block record")
		}
		return nil
	}
	if !observation.CoinbaseAnalyzed {
		if hasEvidence {
			return errors.New("coinbase fields without decoded transaction")
		}
		return nil
	}
	if !observation.Arrived || observation.CoinbaseTotalSats == 0 || observation.CoinbaseOutputCount < 1 || observation.CoinbaseOutputCount > 10000 ||
		len(observation.CoinbaseOutputs) > maxRetainedCoinbaseOutputs || observation.CoinbaseOutputCount < len(observation.CoinbaseOutputs) {
		return errors.New("invalid decoded coinbase summary")
	}
	if observation.CoinbaseOutputsTruncated != (observation.CoinbaseOmittedSats > 0) {
		return errors.New("invalid omitted coinbase value")
	}
	var retainedTotal uint64
	for _, output := range observation.CoinbaseOutputs {
		if output.Worker {
			return errors.New("worker destination must be omitted")
		}
		if output.ValueSats == 0 || len(output.ScriptPubKey) > maxRetainedScriptHexLength || len(output.Address) > 90 || !validCoinbaseScriptType(output.ScriptType) {
			return errors.New("invalid retained coinbase output")
		}
		if output.ScriptPubKey != "" {
			if _, err := hex.DecodeString(output.ScriptPubKey); err != nil {
				return errors.New("invalid retained script")
			}
		}
		if output.ScriptPubKeyTruncated && len(output.ScriptPubKey) != maxRetainedScriptHexLength {
			return errors.New("invalid truncated script")
		}
		for _, character := range output.Address {
			if !((character >= 0x30 && character <= 0x39) || (character >= 0x41 && character <= 0x5a) || (character >= 0x61 && character <= 0x7a)) {
				return errors.New("invalid output address")
			}
		}
		if output.ValueSats > observation.CoinbaseTotalSats-retainedTotal {
			return errors.New("coinbase output total exceeds transaction")
		}
		retainedTotal += output.ValueSats
	}
	if observation.WorkerPayoutSats > observation.CoinbaseTotalSats || retainedTotal > observation.CoinbaseTotalSats-observation.WorkerPayoutSats ||
		observation.CoinbaseOmittedSats > observation.CoinbaseTotalSats-observation.WorkerPayoutSats-retainedTotal ||
		retainedTotal+observation.WorkerPayoutSats+observation.CoinbaseOmittedSats != observation.CoinbaseTotalSats {
		return errors.New("coinbase output totals do not balance")
	}
	if observation.WorkerWalletInCoinbase != (observation.WorkerPayoutSats > 0) {
		return errors.New("worker payout does not match aggregate status")
	}
	if observation.WorkerWalletInCoinbase {
		if observation.EstimatedPoolFeePct == nil || !finite(*observation.EstimatedPoolFeePct) || *observation.EstimatedPoolFeePct < 0 || *observation.EstimatedPoolFeePct > 100 {
			return errors.New("invalid estimated pool fee")
		}
		expected := 100 * float64(observation.CoinbaseTotalSats-observation.WorkerPayoutSats) / float64(observation.CoinbaseTotalSats)
		if math.Abs(*observation.EstimatedPoolFeePct-expected) > 0.000001 {
			return errors.New("estimated pool fee does not match outputs")
		}
	} else if observation.EstimatedPoolFeePct != nil {
		return errors.New("estimated pool fee without worker output")
	}
	return nil
}

func validCoinbaseScriptType(value string) bool {
	if oneOf(value, "p2pkh", "p2sh", "p2wpkh", "p2wsh", "p2tr", "p2pk", "op_return", "unknown") {
		return true
	}
	if !strings.HasPrefix(value, "witness_v") {
		return false
	}
	version, err := strconv.Atoi(strings.TrimPrefix(value, "witness_v"))
	return err == nil && version >= 1 && version <= 16
}

func validID(value string, max int) bool {
	if value == "" || len(value) > max {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("-_./:", character) {
			continue
		}
		return false
	}
	return true
}

func validVersion(value string) bool {
	return value != "" && len(value) <= 64 && !strings.ContainsAny(value, "\r\n\t ")
}

func validRevision(value string) bool {
	if !strings.HasPrefix(value, "sha256:") || len(value) != len("sha256:")+sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func oneOf(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}

type ProbeConfig struct {
	SchemaVersion  int         `json:"schema_version"`
	ConfigRevision string      `json:"config_revision"`
	Pools          []ProbePool `json:"pools"`
}

type ProbePool struct {
	ID        string          `json:"id"`
	Endpoints []ProbeEndpoint `json:"endpoints"`
}

type ProbeEndpoint struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	TLS  bool   `json:"tls"`
}

func BuildProbeConfig(pools []model.Pool) (ProbeConfig, error) {
	config := ProbeConfig{SchemaVersion: 1}
	for _, pool := range pools {
		if pool.ProbeStatus == "compatible" && len(pool.Endpoints) > 0 {
			probePool := ProbePool{ID: pool.ID}
			for _, endpoint := range pool.Endpoints {
				probePool.Endpoints = append(probePool.Endpoints, ProbeEndpoint{Host: endpoint.Host, Port: endpoint.Port, TLS: endpoint.TLS})
			}
			config.Pools = append(config.Pools, probePool)
		}
	}
	encoded, err := json.Marshal(config)
	if err != nil {
		return ProbeConfig{}, err
	}
	sum := sha256.Sum256(encoded)
	config.ConfigRevision = "sha256:" + hex.EncodeToString(sum[:])
	return config, nil
}
