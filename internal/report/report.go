// Package report computes deterministic pool telemetry reports without a composite score.
package report

import (
	"math"
	"sort"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

const MethodologyVersion = "2026-08-01.13"

type feeSample struct {
	at    time.Time
	order int
	value float64
}

type coinbaseSample struct {
	observation model.Observation
	order       int
}

type accumulator struct {
	pool     model.Pool
	blocks   map[string]bool
	offsets  map[string]float64
	tls      bool
	coinbase map[string]coinbaseSample
	timings  map[string]*timingAccumulator
}

// Compute applies only objective probe measurements. Operator size, fees,
// sponsorships, and subjective reputation are deliberately excluded.
func Compute(pools []model.Pool, observations []model.Observation, now time.Time) model.Snapshot {
	acc := make(map[string]*accumulator, len(pools))
	observedBlocks := make(map[string]bool)
	eligiblePoolSamples := make(map[string]bool)
	templateDeliveries := make(map[string]bool)
	canonicalWindows := make(map[string]time.Time)
	for _, p := range pools {
		acc[p.ID] = &accumulator{pool: p, blocks: map[string]bool{}, offsets: map[string]float64{}, coinbase: map[string]coinbaseSample{}}
	}
	for _, o := range observations {
		if acc[o.PoolID] == nil || o.RecordType == model.RecordTypeProtocol || o.ProtocolMethod != "" || !o.Eligible || o.BlockID == "" {
			continue
		}
		key := blockWindowKey(o)
		if old, exists := canonicalWindows[key]; !exists || o.ObservedAt.Before(old) {
			canonicalWindows[key] = o.ObservedAt
		}
	}
	for order, o := range observations {
		protocolRecord := o.RecordType == model.RecordTypeProtocol || o.ProtocolMethod != ""
		if !protocolRecord && o.BlockID != "" {
			observedBlocks[o.BlockID] = true
		}
		a := acc[o.PoolID]
		if a == nil {
			continue
		}
		if protocolRecord {
			addProtocolObservation(a, o)
			continue
		}
		if !o.Eligible || o.BlockID == "" {
			continue
		}
		if start := canonicalWindows[blockWindowKey(o)]; !o.ObservedAt.Equal(start) {
			continue
		}
		key := observationKey(o)
		globalKey := o.PoolID + "\x00" + key
		eligiblePoolSamples[globalKey] = true
		a.blocks[key] = true
		if o.Arrived && o.OffsetMS >= 0 {
			templateDeliveries[globalKey] = true
			if old, exists := a.offsets[key]; !exists || o.OffsetMS < old {
				a.offsets[key] = o.OffsetMS
			}
		}
		if o.Arrived && o.CoinbaseAnalyzed {
			old, exists := a.coinbase[key]
			if !exists || o.ObservedAt.After(old.observation.ObservedAt) || (o.ObservedAt.Equal(old.observation.ObservedAt) && order > old.order) {
				a.coinbase[key] = coinbaseSample{observation: o, order: order}
			}
		}
		if o.TLS {
			a.tls = true
		}
	}

	reports := make([]model.PoolReport, 0, len(acc))
	for _, a := range acc {
		reports = append(reports, build(a))
	}
	sort.SliceStable(reports, func(i, j int) bool {
		return reports[i].PoolName < reports[j].PoolName
	})
	return model.Snapshot{
		GeneratedAt:         now.UTC(),
		Methodology:         MethodologyVersion,
		BlocksObserved:      len(observedBlocks),
		EligiblePoolSamples: len(eligiblePoolSamples),
		TemplateDeliveries:  len(templateDeliveries),
		Reports:             reports,
		Disclosure: []string{
			"Reports use automated observations only; no pool pays or applies for placement.",
			"Latency is relative within the same block and vantage, reducing geographic bias.",
			"Eligible block and protocol-attempt counts are published directly with their measurements.",
			"The probe uses pseudonymous miner credentials, but a pool can still observe its source IP.",
			"Observed pool fee is inferred only when a decoded coinbase output matches the generated worker script; optional donations or splits may be included.",
		},
	}
}

func build(a *accumulator) model.PoolReport {
	blocks, arrivals := len(a.blocks), len(a.offsets)
	availability := 0.0
	if blocks > 0 {
		if arrivals > blocks {
			arrivals = blocks
		}
		availability = 100 * float64(arrivals) / float64(blocks)
	}
	var median, p95 *float64
	medValue, p95Value := 0.0, 0.0
	if arrivals > 0 {
		offsets := make([]float64, 0, arrivals)
		for _, offset := range a.offsets {
			offsets = append(offsets, offset)
		}
		sort.Float64s(offsets)
		medValue = percentile(offsets, .5)
		p95Value = percentile(offsets, .95)
		median, p95 = ptr(round(medValue, 1)), ptr(round(p95Value, 1))
	}
	coinbaseSamples, workerAddressMatches := len(a.coinbase), 0
	feeSamples := make([]feeSample, 0, coinbaseSamples)
	for _, sample := range a.coinbase {
		o := sample.observation
		if o.WorkerWalletInCoinbase {
			workerAddressMatches++
		}
		if o.EstimatedPoolFeePct != nil && *o.EstimatedPoolFeePct >= 0 && *o.EstimatedPoolFeePct <= 100 {
			feeSamples = append(feeSamples, feeSample{at: o.ObservedAt, order: sample.order, value: *o.EstimatedPoolFeePct})
		}
	}
	workerAddressObservedPct := (*float64)(nil)
	workerAddressStatus := "unknown"
	if coinbaseSamples > 0 {
		value := round(100*float64(workerAddressMatches)/float64(coinbaseSamples), 1)
		workerAddressObservedPct = &value
		switch {
		case workerAddressMatches == coinbaseSamples:
			workerAddressStatus = "always_observed"
		case workerAddressMatches == 0:
			workerAddressStatus = "not_observed"
		default:
			workerAddressStatus = "varied"
		}
	}
	latestPoolFeePct := (*float64)(nil)
	previousPoolFeePct := (*float64)(nil)
	poolFeeChanges := 0
	poolFeeLastChangedAt := (*time.Time)(nil)
	if len(feeSamples) > 0 {
		sort.SliceStable(feeSamples, func(i, j int) bool {
			if feeSamples[i].at.Equal(feeSamples[j].at) {
				return feeSamples[i].order < feeSamples[j].order
			}
			return feeSamples[i].at.Before(feeSamples[j].at)
		})
		distinct := make([]float64, 0, len(feeSamples))
		for _, sample := range feeSamples {
			value := round(sample.value, 2)
			if len(distinct) == 0 || distinct[len(distinct)-1] != value {
				if len(distinct) > 0 {
					poolFeeChanges++
					if !sample.at.IsZero() {
						changedAt := sample.at.UTC()
						poolFeeLastChangedAt = &changedAt
					}
				}
				distinct = append(distinct, value)
			}
		}
		latestValue := distinct[len(distinct)-1]
		latestPoolFeePct = &latestValue
		if len(distinct) > 1 {
			previousValue := distinct[len(distinct)-2]
			previousPoolFeePct = &previousValue
		}
	}
	return model.PoolReport{
		PoolID: a.pool.ID, PoolName: a.pool.Name, Category: a.pool.Category, Sources: a.pool.Sources,
		Blocks: blocks, Arrivals: arrivals, MedianMS: median, P95MS: p95,
		Availability: round(availability, 1), TLSObserved: a.tls,
		ConnectTiming: timingStats(a, model.ProtocolConnect), TLSTiming: timingStats(a, model.ProtocolTLSHandshake),
		SubscribeTiming: timingStats(a, model.ProtocolSubscribe), AuthorizeTiming: timingStats(a, model.ProtocolAuthorize), PingTiming: timingStats(a, model.ProtocolPing),
		CoinbaseSamples: coinbaseSamples, WorkerAddressObservedPct: workerAddressObservedPct, WorkerAddressStatus: workerAddressStatus,
		LatestPoolFeePct: latestPoolFeePct, PreviousPoolFeePct: previousPoolFeePct,
		PoolFeeChanged: poolFeeChanges > 0, PoolFeeChanges: poolFeeChanges, PoolFeeSamples: len(feeSamples), PoolFeeLastChangedAt: poolFeeLastChangedAt,
	}
}

func observationKey(o model.Observation) string {
	return o.Vantage + "\x00" + o.BlockID
}

func blockWindowKey(o model.Observation) string {
	return o.Vantage + "\x00" + o.BlockID
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func round(v float64, places int) float64 { m := math.Pow10(places); return math.Round(v*m) / m }
func ptr(v float64) *float64              { return &v }
