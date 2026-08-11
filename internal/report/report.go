// Package report computes deterministic pool telemetry reports and their
// measurement-based performance score.
package report

import (
	"math"
	"sort"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

const (
	MethodologyVersion = "2026-08-10.26"
	reportHistoryLimit = 12
	// LatencyWindow is the rolling period used for block-template and protocol timing statistics.
	LatencyWindow = 24 * time.Hour
	// RetentionWindow is the maximum age of any observation used in a report.
	RetentionWindow = 30 * 24 * time.Hour
)

type metricSample struct {
	at    time.Time
	order int
	value float64
}

type coinbaseSample struct {
	observation model.Observation
	order       int
}

type accumulator struct {
	pool           model.Pool
	lastObservedAt time.Time
	blocks         map[string]bool
	offsets        map[string]metricSample
	tls            bool
	coinbase       map[string]coinbaseSample
	timings        map[string]*timingAccumulator
}

// Compute applies only objective probe measurements. Operator size, fees,
// sponsorships, and subjective reputation are deliberately excluded.
func Compute(pools []model.Pool, observations []model.Observation, now time.Time) model.Snapshot {
	now = now.UTC()
	observations = RetainObservations(observations, now)
	observations = uniqueObservations(observations)
	completedRemoteRuns := completedScheduledRuns(observations)
	acc := make(map[string]*accumulator, len(pools))
	observedBlocks := make(map[string]bool)
	eligiblePoolSamples := make(map[string]bool)
	templateDeliveries := make(map[string]bool)
	canonicalWindows := make(map[string]time.Time)
	for _, p := range pools {
		acc[p.ID] = &accumulator{pool: p, blocks: map[string]bool{}, offsets: map[string]metricSample{}, coinbase: map[string]coinbaseSample{}}
	}
	for _, o := range observations {
		if acc[o.PoolID] == nil || o.RecordType == model.RecordTypeProtocol || o.ProtocolMethod != "" || !o.Eligible || o.BlockID == "" || !scoreableBlockObservation(o, completedRemoteRuns) {
			continue
		}
		key := blockWindowKey(o)
		if old, exists := canonicalWindows[key]; !exists || o.ObservedAt.Before(old) {
			canonicalWindows[key] = o.ObservedAt
		}
	}
	for order, o := range observations {
		protocolRecord := o.RecordType == model.RecordTypeProtocol || o.ProtocolMethod != ""
		if !protocolRecord && o.BlockID != "" && scoreableBlockObservation(o, completedRemoteRuns) {
			observedBlocks[o.BlockID] = true
		}
		a := acc[o.PoolID]
		if a == nil {
			continue
		}
		if protocolRecord {
			recordLatestObservation(a, o.ObservedAt)
			if withinLatencyWindow(o.ObservedAt, now) {
				addProtocolObservation(a, o)
			}
			continue
		}
		if !o.Eligible || o.BlockID == "" || !scoreableBlockObservation(o, completedRemoteRuns) {
			continue
		}
		if start := canonicalWindows[blockWindowKey(o)]; !o.ObservedAt.Equal(start) {
			continue
		}
		recordLatestObservation(a, o.ObservedAt)
		key := observationKey(o)
		globalKey := o.PoolID + "\x00" + key
		eligiblePoolSamples[globalKey] = true
		a.blocks[key] = true
		if o.Arrived && o.OffsetMS >= 0 {
			templateDeliveries[globalKey] = true
			if old, exists := a.offsets[key]; !exists || o.OffsetMS < old.value {
				a.offsets[key] = metricSample{at: o.ObservedAt, order: order, value: o.OffsetMS}
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
		reports = append(reports, build(a, now))
	}
	sort.SliceStable(reports, func(i, j int) bool {
		return reports[i].PoolName < reports[j].PoolName
	})
	return model.Snapshot{
		GeneratedAt:         now.UTC(),
		Methodology:         MethodologyVersion,
		LatencyWindowHours:  int(LatencyWindow / time.Hour),
		RetentionWindowDays: int(RetentionWindow / (24 * time.Hour)),
		BlocksObserved:      len(observedBlocks),
		EligiblePoolSamples: len(eligiblePoolSamples),
		TemplateDeliveries:  len(templateDeliveries),
		Reports:             reports,
		Disclosure: []string{
			"Reports use automated observations only; no pool pays or applies for placement.",
			"Latency is relative within the same block and vantage, reducing geographic bias.",
			"No observation older than 30 days is used; block-template latency, latency history, and protocol timing use a rolling 24-hour window.",
			"Eligible block and protocol-attempt counts are published directly with their measurements.",
			"Scheduled block observations affect scores only after their probe run completes successfully without dropped observations.",
			"The probe uses pseudonymous miner credentials, but a pool can still observe its source IP.",
			"Observed solo-pool fee is inferred only when a decoded coinbase output matches the generated worker script; optional donations or splits may be included.",
			"Matched worker payout destinations are reduced to aggregate verification and fee evidence; their address and script are never published.",
			"Coinbase destinations identify decoded output scripts, not who controls a non-worker address.",
		},
	}
}

// completedScheduledRuns returns the remote run IDs whose terminal record
// proves that the whole scheduled collection was uploaded without loss. Block
// records can arrive in earlier batches, so an interrupted server upload may
// leave useful diagnostics in JSONL without leaving a complete scoring cohort.
func completedScheduledRuns(observations []model.Observation) map[string]bool {
	completed := make(map[string]bool)
	for _, observation := range observations {
		if observation.Source == model.SourceRemoteScheduled &&
			observation.RecordType == model.RecordTypeProbeRun &&
			observation.RunID != "" && observation.RunStatus == "ok" &&
			observation.DroppedObservations == 0 {
			completed[observation.RunID] = true
		}
	}
	return completed
}

func scoreableBlockObservation(observation model.Observation, completedRemoteRuns map[string]bool) bool {
	return observation.Source != model.SourceRemoteScheduled || completedRemoteRuns[observation.RunID]
}

// ComputeVantage filters telemetry to one coarse vantage while retaining
// global coinbase evidence for pool safety classification and fee history.
func ComputeVantage(pools []model.Pool, observations []model.Observation, vantage string, now time.Time) model.Snapshot {
	return ComputeVantages(pools, observations, map[string]bool{vantage: true}, now)
}

// ComputeVantages filters telemetry to a set of coarse vantages while keeping
// the same global evidence behavior as a single-vantage report.
func ComputeVantages(pools []model.Pool, observations []model.Observation, vantages map[string]bool, now time.Time) model.Snapshot {
	filtered := make([]model.Observation, 0, len(observations))
	for _, observation := range observations {
		if vantages[observation.Vantage] {
			filtered = append(filtered, observation)
		}
	}
	regional := Compute(pools, filtered, now)
	global := Compute(pools, observations, now)
	globalReports := make(map[string]model.PoolReport, len(global.Reports))
	for _, poolReport := range global.Reports {
		globalReports[poolReport.PoolID] = poolReport
	}
	for index := range regional.Reports {
		evidence := globalReports[regional.Reports[index].PoolID]
		regional.Reports[index].CoinbaseSamples = evidence.CoinbaseSamples
		regional.Reports[index].WorkerAddressObservedPct = evidence.WorkerAddressObservedPct
		regional.Reports[index].WorkerAddressStatus = evidence.WorkerAddressStatus
		regional.Reports[index].LatestPoolFeePct = evidence.LatestPoolFeePct
		regional.Reports[index].PreviousPoolFeePct = evidence.PreviousPoolFeePct
		regional.Reports[index].PoolFeeChanged = evidence.PoolFeeChanged
		regional.Reports[index].PoolFeeChanges = evidence.PoolFeeChanges
		regional.Reports[index].PoolFeeSamples = evidence.PoolFeeSamples
		regional.Reports[index].PoolFeeLastChangedAt = evidence.PoolFeeLastChangedAt
		regional.Reports[index].LatestCoinbaseObservedAt = evidence.LatestCoinbaseObservedAt
		regional.Reports[index].LatestCoinbaseTotalSats = evidence.LatestCoinbaseTotalSats
		regional.Reports[index].LatestCoinbaseOutputCount = evidence.LatestCoinbaseOutputCount
		regional.Reports[index].LatestPayoutDestinations = evidence.LatestPayoutDestinations
		regional.Reports[index].LatestPayoutDestinationsTruncated = evidence.LatestPayoutDestinationsTruncated
		regional.Reports[index].LatestPayoutOmittedSats = evidence.LatestPayoutOmittedSats
		regional.Reports[index].PoolFeeHistory = evidence.PoolFeeHistory
		applyOverallScore(&regional.Reports[index], now)
	}
	regional.Disclosure = append(regional.Disclosure,
		"Regional views contain scheduled samples only; availability is not continuous uptime.",
		"Solo-pool safety and fee evidence remain global when latency and protocol metrics are filtered by vantage.",
	)
	return regional
}

func uniqueObservations(observations []model.Observation) []model.Observation {
	seen := make(map[string]bool)
	unique := make([]model.Observation, 0, len(observations))
	for _, observation := range observations {
		if observation.ObservationID != "" {
			if seen[observation.ObservationID] {
				continue
			}
			seen[observation.ObservationID] = true
		}
		unique = append(unique, observation)
	}
	return unique
}

// RetainObservations applies the hard report horizon before observations enter
// counts, regional health, aggregation, or scoring.
func RetainObservations(observations []model.Observation, now time.Time) []model.Observation {
	cutoff := now.Add(-RetentionWindow)
	retained := make([]model.Observation, 0, len(observations))
	for _, observation := range observations {
		if observation.ObservedAt.Before(cutoff) || observation.ObservedAt.After(now) {
			continue
		}
		retained = append(retained, observation)
	}
	return retained
}

func build(a *accumulator, now time.Time) model.PoolReport {
	blocks, arrivals := len(a.blocks), len(a.offsets)
	availability := 0.0
	if blocks > 0 {
		if arrivals > blocks {
			arrivals = blocks
		}
		availability = 100 * float64(arrivals) / float64(blocks)
	}

	latencySamples := make([]metricSample, 0, arrivals)
	offsetValues := make([]float64, 0, arrivals)
	for _, sample := range a.offsets {
		if !withinLatencyWindow(sample.at, now) {
			continue
		}
		latencySamples = append(latencySamples, sample)
		offsetValues = append(offsetValues, sample.value)
	}
	var median, p95 *float64
	if len(offsetValues) > 0 {
		sort.Float64s(offsetValues)
		medianValue := percentile(offsetValues, .5)
		p95Value := percentile(offsetValues, .95)
		median, p95 = ptr(round(medianValue, 1)), ptr(round(p95Value, 1))
	}

	coinbaseSamples, workerAddressMatches := len(a.coinbase), 0
	feeSamples := make([]metricSample, 0, coinbaseSamples)
	var latestCoinbase coinbaseSample
	hasLatestCoinbase := false
	for _, sample := range a.coinbase {
		o := sample.observation
		if o.WorkerWalletInCoinbase {
			workerAddressMatches++
		}
		if a.pool.Category == "solo" && o.EstimatedPoolFeePct != nil && *o.EstimatedPoolFeePct >= 0 && *o.EstimatedPoolFeePct <= 100 {
			feeSamples = append(feeSamples, metricSample{at: o.ObservedAt, order: sample.order, value: *o.EstimatedPoolFeePct})
		}
		if !hasLatestCoinbase || o.ObservedAt.After(latestCoinbase.observation.ObservedAt) || (o.ObservedAt.Equal(latestCoinbase.observation.ObservedAt) && sample.order > latestCoinbase.order) {
			latestCoinbase = sample
			hasLatestCoinbase = true
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

	var latestCoinbaseObservedAt *time.Time
	var latestCoinbaseTotalSats, latestPayoutOmittedSats uint64
	var latestCoinbaseOutputCount int
	var latestPayoutDestinations []model.PayoutDestination
	latestPayoutDestinationsTruncated := false
	if hasLatestCoinbase {
		observation := latestCoinbase.observation
		if !observation.ObservedAt.IsZero() {
			observedAt := observation.ObservedAt.UTC()
			latestCoinbaseObservedAt = &observedAt
		}
		latestCoinbaseTotalSats = observation.CoinbaseTotalSats
		latestCoinbaseOutputCount = observation.CoinbaseOutputCount
		if latestCoinbaseOutputCount == 0 && len(observation.CoinbaseOutputs) > 0 {
			latestCoinbaseOutputCount = len(observation.CoinbaseOutputs)
		}
		latestPayoutDestinationsTruncated = observation.CoinbaseOutputsTruncated
		latestPayoutOmittedSats = observation.CoinbaseOmittedSats
		latestPayoutDestinations = make([]model.PayoutDestination, 0, len(observation.CoinbaseOutputs))
		for _, output := range observation.CoinbaseOutputs {
			// Version 8 observations already omit this private destination. Keep
			// the filter here so historical version 7 records cannot expose it.
			if output.Worker {
				continue
			}
			percentage := 0.0
			if observation.CoinbaseTotalSats > 0 {
				percentage = round(100*float64(output.ValueSats)/float64(observation.CoinbaseTotalSats), 4)
			}
			latestPayoutDestinations = append(latestPayoutDestinations, model.PayoutDestination{
				ValueSats: output.ValueSats, Percentage: percentage,
				ScriptPubKey: output.ScriptPubKey, ScriptPubKeyTruncated: output.ScriptPubKeyTruncated,
				Address: output.Address, ScriptType: output.ScriptType,
			})
		}
	}

	var lastObservedAt *time.Time
	if !a.lastObservedAt.IsZero() {
		value := a.lastObservedAt.UTC()
		lastObservedAt = &value
	}
	report := model.PoolReport{
		PoolID: a.pool.ID, PoolName: a.pool.Name, Category: a.pool.Category, Products: a.pool.Products,
		LastObservedAt: lastObservedAt,
		Blocks:         blocks, Arrivals: arrivals, MedianMS: median, P95MS: p95, EstimatedMiningLossPct: estimatedMiningLoss(median, availability, blocks),
		Availability: round(availability, 1), TLSObserved: a.tls,
		ConnectTiming: timingStats(a, model.ProtocolConnect), TLSTiming: timingStats(a, model.ProtocolTLSHandshake),
		SubscribeTiming: timingStats(a, model.ProtocolSubscribe), AuthorizeTiming: timingStats(a, model.ProtocolAuthorize), PingTiming: timingStats(a, model.ProtocolPing),
		CoinbaseSamples: coinbaseSamples, WorkerAddressObservedPct: workerAddressObservedPct, WorkerAddressStatus: workerAddressStatus,
		LatestPoolFeePct: latestPoolFeePct, PreviousPoolFeePct: previousPoolFeePct,
		PoolFeeChanged: poolFeeChanges > 0, PoolFeeChanges: poolFeeChanges, PoolFeeSamples: len(feeSamples), PoolFeeLastChangedAt: poolFeeLastChangedAt,
		LatestCoinbaseObservedAt: latestCoinbaseObservedAt, LatestCoinbaseTotalSats: latestCoinbaseTotalSats,
		LatestCoinbaseOutputCount: latestCoinbaseOutputCount, LatestPayoutDestinations: latestPayoutDestinations,
		LatestPayoutDestinationsTruncated: latestPayoutDestinationsTruncated, LatestPayoutOmittedSats: latestPayoutOmittedSats,
		TemplateLatencyHistory: recentMetricHistory(latencySamples, 1), PoolFeeHistory: recentMetricHistory(feeSamples, 2),
	}
	applyOverallScore(&report, now)
	return report
}

func recordLatestObservation(a *accumulator, observedAt time.Time) {
	if !observedAt.IsZero() && observedAt.After(a.lastObservedAt) {
		a.lastObservedAt = observedAt
	}
}

func recentMetricHistory(samples []metricSample, places int) []model.MetricHistoryPoint {
	if len(samples) == 0 {
		return nil
	}
	sort.SliceStable(samples, func(i, j int) bool {
		if samples[i].at.Equal(samples[j].at) {
			return samples[i].order < samples[j].order
		}
		return samples[i].at.Before(samples[j].at)
	})
	if len(samples) > reportHistoryLimit {
		samples = samples[len(samples)-reportHistoryLimit:]
	}
	history := make([]model.MetricHistoryPoint, 0, len(samples))
	for _, sample := range samples {
		history = append(history, model.MetricHistoryPoint{ObservedAt: sample.at.UTC(), Value: round(sample.value, places)})
	}
	return history
}

func withinLatencyWindow(observedAt, now time.Time) bool {
	cutoff := now.Add(-LatencyWindow)
	return !observedAt.Before(cutoff) && !observedAt.After(now)
}

func estimatedMiningLoss(latencyMS *float64, availability float64, blocks int) *float64 {
	if blocks == 0 || latencyMS == nil {
		return nil
	}
	latencyLossPct := *latencyMS / 600_000 * 100
	availableFraction := clamp(availability/100, 0, 1)
	value := round((1-availableFraction)*100+availableFraction*latencyLossPct, 4)
	return &value
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
