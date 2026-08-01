// Package report computes deterministic pool telemetry reports without a composite score.
package report

import (
	"math"
	"sort"
	"time"

	"github.com/proofofmike/stratumstats/internal/model"
)

const MethodologyVersion = "2026-08-01.7"

type accumulator struct {
	pool                 model.Pool
	blocks               map[string]bool
	offsets              []float64
	tls                  bool
	coinbaseSamples      int
	workerAddressMatches int
	feeSamples           []float64
	timings              map[string]*timingAccumulator
}

// Compute applies only objective probe measurements. Operator size, fees,
// sponsorships, and subjective reputation are deliberately excluded.
func Compute(pools []model.Pool, observations []model.Observation, now time.Time) model.Snapshot {
	acc := make(map[string]*accumulator, len(pools))
	for _, p := range pools {
		acc[p.ID] = &accumulator{pool: p, blocks: map[string]bool{}}
	}
	for _, o := range observations {
		a := acc[o.PoolID]
		if a == nil {
			continue
		}
		if o.RecordType == model.RecordTypeProtocol || o.ProtocolMethod != "" {
			addProtocolObservation(a, o)
			continue
		}
		if !o.Eligible || o.BlockID == "" {
			continue
		}
		a.blocks[o.BlockID] = true
		if o.Arrived && o.OffsetMS >= 0 {
			a.offsets = append(a.offsets, o.OffsetMS)
		}
		if o.Arrived && o.CoinbaseAnalyzed {
			a.coinbaseSamples++
			if o.WorkerWalletInCoinbase {
				a.workerAddressMatches++
			}
			if o.EstimatedPoolFeePct != nil && *o.EstimatedPoolFeePct >= 0 && *o.EstimatedPoolFeePct <= 100 {
				a.feeSamples = append(a.feeSamples, *o.EstimatedPoolFeePct)
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
		GeneratedAt: now.UTC(), Methodology: MethodologyVersion, Reports: reports,
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
		availability = 100 * wilsonLower(arrivals, blocks)
	}
	var median, p95 *float64
	medValue, p95Value := 0.0, 0.0
	if arrivals > 0 {
		sort.Float64s(a.offsets)
		medValue = percentile(a.offsets, .5)
		p95Value = percentile(a.offsets, .95)
		median, p95 = ptr(round(medValue, 1)), ptr(round(p95Value, 1))
	}
	workerAddressObservedPct := (*float64)(nil)
	workerAddressStatus := "unknown"
	if a.coinbaseSamples > 0 {
		value := round(100*float64(a.workerAddressMatches)/float64(a.coinbaseSamples), 1)
		workerAddressObservedPct = &value
		switch {
		case a.workerAddressMatches == a.coinbaseSamples:
			workerAddressStatus = "always_observed"
		case a.workerAddressMatches == 0:
			workerAddressStatus = "not_observed"
		default:
			workerAddressStatus = "varied"
		}
	}
	poolFeePct := (*float64)(nil)
	poolFeeMinPct := (*float64)(nil)
	poolFeeMaxPct := (*float64)(nil)
	feeClass := "unknown"
	if len(a.feeSamples) > 0 {
		sort.Float64s(a.feeSamples)
		value := round(percentile(a.feeSamples, .5), 3)
		poolFeePct = &value
		minValue, maxValue := round(a.feeSamples[0], 3), round(a.feeSamples[len(a.feeSamples)-1], 3)
		poolFeeMinPct, poolFeeMaxPct = &minValue, &maxValue
		switch {
		case maxValue <= 0.0005:
			feeClass = "zero"
		case minValue <= 0.0005:
			feeClass = "variable"
		default:
			feeClass = "positive"
		}
	}
	return model.PoolReport{
		PoolID: a.pool.ID, PoolName: a.pool.Name, Category: a.pool.Category, Sources: a.pool.Sources,
		Blocks: blocks, Arrivals: arrivals, MedianMS: median, P95MS: p95,
		Availability: round(availability, 1), TLSObserved: a.tls,
		ConnectTiming: timingStats(a, model.ProtocolConnect), TLSTiming: timingStats(a, model.ProtocolTLSHandshake),
		SubscribeTiming: timingStats(a, model.ProtocolSubscribe), AuthorizeTiming: timingStats(a, model.ProtocolAuthorize), PingTiming: timingStats(a, model.ProtocolPing),
		CoinbaseSamples: a.coinbaseSamples, WorkerAddressObservedPct: workerAddressObservedPct, WorkerAddressStatus: workerAddressStatus,
		PoolFeePct: poolFeePct, PoolFeeMinPct: poolFeeMinPct, PoolFeeMaxPct: poolFeeMaxPct, FeeClass: feeClass,
	}
}

func wilsonLower(successes, trials int) float64 {
	if trials == 0 {
		return 0
	}
	z := 1.96
	n, p := float64(trials), float64(successes)/float64(trials)
	denom := 1 + z*z/n
	return clamp((p+z*z/(2*n)-z*math.Sqrt((p*(1-p)+z*z/(4*n))/n))/denom, 0, 1)
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

func clamp(v, lo, hi float64) float64     { return math.Max(lo, math.Min(hi, v)) }
func round(v float64, places int) float64 { m := math.Pow10(places); return math.Round(v*m) / m }
func ptr(v float64) *float64              { return &v }
