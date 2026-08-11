package report

import (
	"testing"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func TestOverallScoreRequiresCoreMeasurements(t *testing.T) {
	report := model.PoolReport{Blocks: 1, Availability: 100, MedianMS: ptr(50)}
	if score := overallScore(report, time.Time{}).Score; score != nil {
		t.Fatalf("score=%v, want nil without P95", score)
	}
}

func TestOverallScoreRewardsExcellentCompleteMeasurements(t *testing.T) {
	report := model.PoolReport{
		Category: "solo", Blocks: 20, Availability: 100,
		MedianMS: ptr(80), P95MS: ptr(200), PoolFeeSamples: 4,
		ConnectTiming:   model.TimingStats{MedianMS: ptr(50)},
		SubscribeTiming: model.TimingStats{MedianMS: ptr(60)},
		AuthorizeTiming: model.TimingStats{MedianMS: ptr(70)},
	}
	if score := overallScore(report, time.Time{}).Score; score == nil || *score != 99.7 {
		t.Fatalf("score=%v, want fractional score 99.7", score)
	}
}

func TestOverallScorePenalizesTailAvailabilityAndFeeChanges(t *testing.T) {
	stable := model.PoolReport{
		Category: "solo", Blocks: 20, Availability: 98,
		MedianMS: ptr(250), P95MS: ptr(1000), PoolFeeSamples: 5,
	}
	changed := stable
	changed.PoolFeeChanges = 4
	stableScore := overallScore(stable, time.Time{}).Score
	changedScore := overallScore(changed, time.Time{}).Score
	if stableScore == nil || changedScore == nil || *stableScore <= *changedScore {
		t.Fatalf("stable score=%v changed score=%v, want stable higher", stableScore, changedScore)
	}
	if *changedScore < 0 || *stableScore > 100 {
		t.Fatalf("scores out of range: stable=%v changed=%v", stableScore, changedScore)
	}
}

func TestOverallScoreReweightsUnavailableOptionalMetrics(t *testing.T) {
	report := model.PoolReport{Blocks: 1, Availability: 100, MedianMS: ptr(50), P95MS: ptr(100)}
	if score := overallScore(report, time.Time{}).Score; score == nil || *score != 100 {
		t.Fatalf("score=%v, want 100 without optional evidence", score)
	}
}

func TestOverallScoreMakesAvailabilityDominant(t *testing.T) {
	fastButUnavailable := model.PoolReport{Blocks: 20, Availability: 95, MedianMS: ptr(50), P95MS: ptr(100)}
	slowerButAvailable := model.PoolReport{Blocks: 20, Availability: 100, MedianMS: ptr(500), P95MS: ptr(1000)}
	fastScore := overallScore(fastButUnavailable, time.Time{}).Score
	availableScore := overallScore(slowerButAvailable, time.Time{}).Score
	if fastScore == nil || availableScore == nil || *fastScore >= *availableScore {
		t.Fatalf("fast/unavailable score=%v available score=%v, want availability to dominate", fastScore, availableScore)
	}
}

func TestOverallScoreRetainsFractionalPrecision(t *testing.T) {
	report := model.PoolReport{Blocks: 20, Availability: 100, MedianMS: ptr(500), P95MS: ptr(500)}
	score := overallScore(report, time.Time{}).Score
	if score == nil || *score != 97.6471 {
		t.Fatalf("score=%v, want fractional score 97.6471", score)
	}
}

func TestMiningLossUnderPointOneGetsPerfectComponentScore(t *testing.T) {
	if score := scoreFromAnchors(0.0999, miningLossScoreAnchors); score != 100 {
		t.Fatalf("sub-0.1%% mining-loss score=%.1f, want 100", score)
	}
	if score := scoreFromAnchors(0.25, miningLossScoreAnchors); score >= 100 {
		t.Fatalf("0.25%% mining-loss score=%.1f, want below 100", score)
	}
}

func TestOverallScoreStronglyPenalizesRecentFeeIncrease(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 0, 0, 0, time.UTC)
	previous, latest := 0.5, 1.0
	changedAt := now.Add(-24 * time.Hour)
	report := model.PoolReport{
		Category: "solo", Blocks: 20, Availability: 100,
		MedianMS: ptr(50), P95MS: ptr(100), PoolFeeSamples: 2, PoolFeeChanges: 1,
		PreviousPoolFeePct: &previous, LatestPoolFeePct: &latest, PoolFeeLastChangedAt: &changedAt,
	}
	result := overallScore(report, now)
	score, penalty := result.Score, result.RecentFeeIncreasePenalty
	if score == nil || penalty < 14 || *score > 82 {
		t.Fatalf("score=%v penalty=%.1f, want a roughly 15-point recent-increase penalty", score, penalty)
	}

	changedAt = now.Add(-ScoreFeeIncreaseWindow)
	report.PoolFeeLastChangedAt = &changedAt
	penalty = overallScore(report, now).RecentFeeIncreasePenalty
	if penalty != 0 {
		t.Fatalf("expired fee-increase penalty=%.1f, want 0", penalty)
	}

	report.PreviousPoolFeePct, report.LatestPoolFeePct = &latest, &previous
	changedAt = now.Add(-time.Hour)
	report.PoolFeeLastChangedAt = &changedAt
	penalty = overallScore(report, now).RecentFeeIncreasePenalty
	if penalty != 0 {
		t.Fatalf("fee-decrease penalty=%.1f, want 0", penalty)
	}
}

func TestOverallScorePenalizesFeesAboveTwoAndAHalfPercent(t *testing.T) {
	standardFee, thresholdFee, highFee, veryHighFee := 2.0, 2.5, 5.0, 8.0
	base := model.PoolReport{Category: "solo", Blocks: 20, Availability: 100, MedianMS: ptr(50), P95MS: ptr(100), PoolFeeSamples: 1}

	base.LatestPoolFeePct = &standardFee
	result := overallScore(base, time.Time{})
	standardScore, standardPenalty := result.Score, result.HighFeePenalty
	base.LatestPoolFeePct = &thresholdFee
	thresholdPenalty := overallScore(base, time.Time{}).HighFeePenalty
	base.LatestPoolFeePct = &highFee
	result = overallScore(base, time.Time{})
	highScore, highPenalty := result.Score, result.HighFeePenalty
	base.LatestPoolFeePct = &veryHighFee
	cappedPenalty := overallScore(base, time.Time{}).HighFeePenalty

	if standardPenalty != 0 || thresholdPenalty != 0 || standardScore == nil || *standardScore != 100 {
		t.Fatalf("standard fee score=%v penalties=%.1f/%.1f, want 100/0/0", standardScore, standardPenalty, thresholdPenalty)
	}
	if highPenalty != 6.3 || highScore == nil || *highScore != 93.7 {
		t.Fatalf("5%% fee score=%v penalty=%.1f, want 93.7/6.3", highScore, highPenalty)
	}
	if cappedPenalty != ScoreHighFeeMaxPenalty {
		t.Fatalf("very high fee penalty=%.1f, want capped %.1f", cappedPenalty, ScoreHighFeeMaxPenalty)
	}
}

func TestOverallScorePenalizesInvalidTLSCertificate(t *testing.T) {
	report := model.PoolReport{
		Blocks: 20, Availability: 100, MedianMS: ptr(50), P95MS: ptr(100),
		TLSTiming: model.TimingStats{CertificateErrors: 1},
	}
	result := overallScore(report, time.Time{})
	if result.Score == nil || *result.Score != 90 || result.TLSCertificatePenalty != 10 {
		t.Fatalf("score=%v TLS penalty=%.1f, want 90/10", result.Score, result.TLSCertificatePenalty)
	}
}

func TestOverallScoreIsZeroWhenSoloWorkerWalletNotFound(t *testing.T) {
	for _, status := range []string{"not_observed", "varied"} {
		report := model.PoolReport{Category: "solo", WorkerAddressStatus: status}
		result := overallScore(report, time.Time{})
		if result.Score == nil || *result.Score != 0 || result.OverrideReason != "worker_wallet_not_found" {
			t.Errorf("status=%s score=%v override=%q, want 0/worker_wallet_not_found", status, result.Score, result.OverrideReason)
		}
	}
}

func TestOverallScoreDoesNotApplyWalletOverrideToSharedPool(t *testing.T) {
	report := model.PoolReport{Category: "shared", WorkerAddressStatus: "not_observed"}
	result := overallScore(report, time.Time{})
	if result.Score != nil || result.OverrideReason != "" {
		t.Fatalf("score=%v override=%q, want no score or override", result.Score, result.OverrideReason)
	}
}
