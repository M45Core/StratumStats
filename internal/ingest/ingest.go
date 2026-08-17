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
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

const (
	BlockEnvelopeVersion = 2
	maxCompressedBytes   = 256 << 10
	maxDecompressedBytes = 1 << 20
	maxRequestClockSkew  = 5 * time.Minute
	RemoteSource         = model.SourceRemoteScheduled
)

var RegionVantages = productionRegionVantages()

func productionRegionVantages() map[string]string {
	regions := model.ProductionRegions()
	vantages := make(map[string]string, len(regions))
	for _, region := range regions {
		vantages[region.Region] = region.Vantage
	}
	return vantages
}

type Envelope struct {
	SchemaVersion    int                `json:"schema_version"`
	BatchID          string             `json:"batch_id"`
	ConfigRevision   string             `json:"config_revision"`
	Region           string             `json:"region"`
	FilterContinents bool               `json:"filter_continents,omitempty"`
	Sample           *model.BlockSample `json:"sample"`
}

type acceptedResponse struct {
	BatchID  string `json:"batch_id"`
	Accepted int    `json:"accepted"`
}

type Receiver struct {
	Pools          []model.Pool
	PoolRevisions  map[string][]model.Pool
	RevisionExpiry map[string]time.Time
	Keys           map[string][]byte
	Append         func([]model.Observation) error
	Now            func() time.Time
	Replays        *ReplayGuard
}

// ReplayGuard prevents a valid, captured batch from being appended repeatedly
// during the lifetime of its otherwise-valid HMAC timestamp.
type ReplayGuard struct {
	mu      sync.Mutex
	batches map[string]time.Time
}

func NewReplayGuard() *ReplayGuard {
	return &ReplayGuard{batches: make(map[string]time.Time)}
}

func (guard *ReplayGuard) claim(key string, now, expiresAt time.Time) bool {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	for batch, expiry := range guard.batches {
		if now.After(expiry) {
			delete(guard.batches, batch)
		}
	}
	if _, exists := guard.batches[key]; exists {
		return false
	}
	guard.batches[key] = expiresAt
	return true
}

func (guard *ReplayGuard) release(key string) {
	guard.mu.Lock()
	defer guard.mu.Unlock()
	delete(guard.batches, key)
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
	replayKey := request.Header.Get("X-StratumStats-Key-ID") + "\n" + envelope.BatchID
	authTimestamp, _ := strconv.ParseInt(request.Header.Get("X-StratumStats-Timestamp"), 10, 64)
	replayExpiresAt := time.Unix(authTimestamp, 0).Add(maxRequestClockSkew)
	if receiver.Replays != nil && !receiver.Replays.claim(replayKey, now, replayExpiresAt) {
		http.Error(w, "duplicate batch", http.StatusConflict)
		return
	}
	if err := receiver.Append(observations); err != nil {
		if receiver.Replays != nil {
			receiver.Replays.release(replayKey)
		}
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
	limited := &io.LimitedReader{R: compressed, N: maxDecompressedBytes + 1}
	var envelope Envelope
	decoder := json.NewDecoder(limited)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&envelope); err != nil {
		_, drainErr := io.Copy(io.Discard, limited)
		if limited.N == 0 {
			return Envelope{}, http.StatusRequestEntityTooLarge, errors.New("request too large")
		}
		if drainErr != nil {
			return Envelope{}, http.StatusBadRequest, errors.New("cannot decompress body")
		}
		return Envelope{}, http.StatusBadRequest, fmt.Errorf("invalid envelope: %w", err)
	}
	trailingErr := decoder.Decode(&struct{}{})
	if limited.N == 0 {
		return Envelope{}, http.StatusRequestEntityTooLarge, errors.New("request too large")
	}
	if !errors.Is(trailingErr, io.EOF) {
		if errors.Is(trailingErr, gzip.ErrChecksum) || errors.Is(trailingErr, gzip.ErrHeader) || errors.Is(trailingErr, io.ErrUnexpectedEOF) {
			return Envelope{}, http.StatusBadRequest, errors.New("cannot decompress body")
		}
		return Envelope{}, http.StatusBadRequest, errors.New("invalid trailing data")
	}
	return envelope, 0, nil
}

func (receiver Receiver) validate(envelope Envelope, now time.Time) ([]model.Observation, error) {
	if envelope.SchemaVersion != BlockEnvelopeVersion {
		return nil, errors.New("unsupported envelope version")
	}
	return receiver.validateBlockEnvelope(envelope, now)
}

func (receiver Receiver) validateBlockEnvelope(envelope Envelope, now time.Time) ([]model.Observation, error) {
	if !validID(envelope.BatchID, 128) {
		return nil, errors.New("invalid envelope identity")
	}
	if envelope.Sample == nil {
		return nil, errors.New("missing block sample")
	}
	vantage, ok := RegionVantages[envelope.Region]
	if !ok {
		return nil, errors.New("invalid region or vantage")
	}
	if !validRevision(envelope.ConfigRevision) {
		return nil, errors.New("invalid configuration revision")
	}
	endpoints, err := receiver.allowedBlockTargets(envelope.ConfigRevision, vantage, envelope.FilterContinents, now)
	if err != nil {
		return nil, err
	}
	forwarded := *envelope.Sample
	if !validBlockID(forwarded.BlockID) ||
		envelope.BatchID != envelope.Region+"-"+forwarded.BlockID ||
		len(forwarded.EndpointSamples) == 0 || len(forwarded.EndpointSamples) > len(endpoints) {
		return nil, errors.New("invalid block sample")
	}
	seen := make(map[endpointIdentity]bool, len(forwarded.EndpointSamples))
	arrivals := make(map[endpointIdentity]time.Time, len(forwarded.EndpointSamples))
	var first time.Time
	for index := range forwarded.EndpointSamples {
		endpointSample := forwarded.EndpointSamples[index]
		identity := endpointIdentity{poolID: endpointSample.PoolID, address: endpointSample.Endpoint, tls: endpointSample.TLS}
		if !endpoints[identity] || seen[identity] {
			return nil, fmt.Errorf("invalid endpoint sample %d identity", index)
		}
		seen[identity] = true
		if err := validateEndpointSetup(endpointSample.Setup, endpointSample.TLS, now); err != nil {
			return nil, fmt.Errorf("invalid endpoint sample %d setup: %w", index, err)
		}
		if endpointSample.ReceivedAt == nil && endpointSample.Setup == nil {
			return nil, fmt.Errorf("empty endpoint sample %d", index)
		}
		if endpointSample.ReceivedAt == nil {
			continue
		}
		receivedAt := endpointSample.ReceivedAt.UTC()
		if receivedAt.IsZero() || receivedAt.After(now.Add(time.Second)) {
			return nil, fmt.Errorf("invalid endpoint sample %d receive time", index)
		}
		arrivals[identity] = receivedAt
		if first.IsZero() || receivedAt.Before(first) {
			first = receivedAt
		}
	}
	if len(arrivals) == 0 {
		return nil, errors.New("block sample needs an arrival")
	}
	derived := make([]model.EndpointBlockSample, 0, len(forwarded.EndpointSamples))
	for _, forwardedEndpoint := range forwarded.EndpointSamples {
		identity := endpointIdentity{poolID: forwardedEndpoint.PoolID, address: forwardedEndpoint.Endpoint, tls: forwardedEndpoint.TLS}
		endpointSample := model.EndpointBlockSample{
			PoolID: forwardedEndpoint.PoolID, Endpoint: forwardedEndpoint.Endpoint, TLS: forwardedEndpoint.TLS, Setup: forwardedEndpoint.Setup,
		}
		if receivedAt, ok := arrivals[identity]; ok {
			offset := float64(receivedAt.Sub(first).Microseconds()) / 1000
			endpointSample.OffsetMS = &offset
		}
		if endpointSample.OffsetMS == nil && endpointSample.Setup == nil {
			continue
		}
		derived = append(derived, endpointSample)
	}
	eligible := make([]model.EndpointIdentity, 0, len(endpoints))
	for identity := range endpoints {
		eligible = append(eligible, model.EndpointIdentity{PoolID: identity.poolID, Endpoint: identity.address, TLS: identity.tls})
	}
	sort.Slice(eligible, func(i, j int) bool {
		if eligible[i].PoolID != eligible[j].PoolID {
			return eligible[i].PoolID < eligible[j].PoolID
		}
		if eligible[i].Endpoint != eligible[j].Endpoint {
			return eligible[i].Endpoint < eligible[j].Endpoint
		}
		return !eligible[i].TLS && eligible[j].TLS
	})
	sample := model.Observation{
		Version: model.ObservationVersion, ObservationID: envelope.BatchID,
		Source: RemoteSource, RunID: envelope.BatchID, ConfigRevision: envelope.ConfigRevision,
		RecordType: model.RecordTypeBlockSample, ObservedAt: first.UTC(), Vantage: vantage,
		BlockID:         forwarded.BlockID,
		EndpointSamples: derived, EligibleEndpoints: eligible,
	}
	return []model.Observation{sample}, nil
}

func validateEndpointSetup(setup *model.EndpointSetup, tlsEndpoint bool, sentAt time.Time) error {
	if setup == nil {
		return nil
	}
	if setup.Connect == nil && setup.TLS == nil && setup.Subscribe == nil && setup.Authorize == nil {
		return errors.New("empty setup")
	}
	if !tlsEndpoint && setup.TLS != nil {
		return errors.New("TLS result on plain endpoint")
	}
	for _, sample := range []*model.ProtocolSample{setup.Connect, setup.TLS, setup.Subscribe, setup.Authorize} {
		if sample == nil {
			continue
		}
		if sample.ObservedAt.IsZero() || sample.ObservedAt.After(sentAt.Add(time.Second)) ||
			!finite(sample.DurationMS) || sample.DurationMS < 0 ||
			!oneOf(sample.ResponseStatus, model.ProtocolStatusOK, model.ProtocolStatusRejected, model.ProtocolStatusTimeout, model.ProtocolStatusError) {
			return errors.New("invalid protocol result")
		}
		if sample.ErrorCategory != "" && !validID(sample.ErrorCategory, 128) {
			return errors.New("invalid error category")
		}
	}
	return nil
}

type endpointIdentity struct {
	poolID, address string
	tls             bool
}

func (receiver Receiver) allowedConfiguration(revision string, now time.Time) ([]model.Pool, error) {
	configured := receiver.Pools
	if len(receiver.PoolRevisions) > 0 {
		var ok bool
		configured, ok = receiver.PoolRevisions[revision]
		expiresAt := receiver.RevisionExpiry[revision]
		if !ok || (!expiresAt.IsZero() && !now.Before(expiresAt)) {
			return nil, errors.New("unknown or expired configuration revision")
		}
	}
	return configured, nil
}

func targetEndpoints(configured []model.Pool, continent string) map[endpointIdentity]bool {
	endpoints := make(map[endpointIdentity]bool)
	for _, pool := range configured {
		for _, endpoint := range pool.Endpoints {
			endpointContinent := model.EndpointContinent(endpoint)
			if continent != "" && endpointContinent != "" && endpointContinent != continent {
				continue
			}
			address := net.JoinHostPort(endpoint.Host, strconv.Itoa(endpoint.Port))
			endpoints[endpointIdentity{poolID: pool.ID, address: address, tls: endpoint.TLS}] = true
		}
	}
	return endpoints
}

func (receiver Receiver) allowedBlockTargets(revision, vantage string, filterContinents bool, now time.Time) (map[endpointIdentity]bool, error) {
	configured, err := receiver.allowedConfiguration(revision, now)
	if err != nil {
		return nil, err
	}
	continent := ""
	if filterContinents {
		continent = model.VantageContinent(vantage)
	}
	return targetEndpoints(configured, continent), nil
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

func validBlockID(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32
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
	Host      string `json:"host"`
	Port      int    `json:"port"`
	TLS       bool   `json:"tls"`
	Continent string `json:"continent,omitempty"`
}

func BuildProbeConfig(pools []model.Pool) (ProbeConfig, error) {
	config := ProbeConfig{SchemaVersion: 1}
	for _, pool := range pools {
		if len(pool.Endpoints) > 0 {
			probePool := ProbePool{ID: pool.ID}
			for _, endpoint := range pool.Endpoints {
				probePool.Endpoints = append(probePool.Endpoints, ProbeEndpoint{Host: endpoint.Host, Port: endpoint.Port, TLS: endpoint.TLS, Continent: model.EndpointContinent(endpoint)})
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
