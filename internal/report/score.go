package report

import (
	"math"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

const (
	ScoreMiningLossWeight      = 25.0
	ScoreP95Weight             = 20.0
	ScoreAvailabilityWeight    = 40.0
	ScoreResponsivenessWeight  = 10.0
	ScoreFeeStabilityWeight    = 5.0
	ScoreFeeIncreaseMaxPenalty = 15.0
	ScoreFeeIncreaseWindow     = 30 * 24 * time.Hour
	ScoreHighFeeThreshold      = 2.5
	ScoreHighFeePenaltyPerPct  = 2.5
	ScoreHighFeeMaxPenalty     = 10.0
	ScoreTLSCertificatePenalty = 10.0
)

type overallScoreResult struct {
	Score                    *float64
	RecentFeeIncreasePenalty float64
	HighFeePenalty           float64
	TLSCertificatePenalty    float64
	OverrideReason           string
}

type scoreAnchor struct {
	value float64
	score float64
}

var (
	miningLossScoreAnchors = []scoreAnchor{
		{0, 100}, {0.1, 100}, {0.25, 90}, {0.5, 75}, {1, 50}, {2.5, 10}, {5, 0},
	}
	p95ScoreAnchors = []scoreAnchor{
		{0, 100}, {250, 100}, {500, 90}, {1000, 75}, {2000, 50}, {5000, 10}, {10000, 0},
	}
	availabilityScoreAnchors = []scoreAnchor{
		{0, 0}, {90, 0}, {95, 20}, {98, 60}, {99, 80}, {99.5, 90}, {100, 100},
	}
	responsivenessScoreAnchors = []scoreAnchor{
		{0, 100}, {100, 95}, {250, 80}, {500, 60}, {1000, 30}, {2500, 0},
	}
)

// overallScore combines only observed metrics. The three core components are
// required. Optional responsiveness and fee-stability weights are normalized
// away when there is not enough evidence, so missing data is not scored as a
// failure. Estimated mining loss combines missed deliveries and median delay,
// while availability remains separately dominant. Fee increases, high fees,
// and invalid TLS certificates apply explicit penalties. Missing worker-wallet
// evidence overrides a solo pool's score because that configuration would not
// pay the tested miner.
func overallScore(report model.PoolReport, now time.Time) overallScoreResult {
	if report.Category == "solo" && (report.WorkerAddressStatus == "not_observed" || report.WorkerAddressStatus == "varied") {
		score := 0.0
		return overallScoreResult{Score: &score, OverrideReason: "worker_wallet_not_found"}
	}
	if report.MedianMS == nil || report.P95MS == nil || report.Blocks == 0 {
		return overallScoreResult{}
	}

	miningLoss := report.EstimatedMiningLossPct
	if miningLoss == nil {
		miningLoss = estimatedMiningLoss(report.MedianMS, report.Availability, report.Blocks)
	}
	weightedScore := ScoreMiningLossWeight*scoreFromAnchors(*miningLoss, miningLossScoreAnchors) +
		ScoreP95Weight*scoreFromAnchors(*report.P95MS, p95ScoreAnchors) +
		ScoreAvailabilityWeight*scoreFromAnchors(report.Availability, availabilityScoreAnchors)
	weight := ScoreMiningLossWeight + ScoreP95Weight + ScoreAvailabilityWeight

	if responsiveness, ok := responsivenessScore(report); ok {
		weightedScore += ScoreResponsivenessWeight * responsiveness
		weight += ScoreResponsivenessWeight
	}
	if report.Category == "solo" && report.PoolFeeSamples >= 2 {
		transitions := report.PoolFeeSamples - 1
		stability := 100 * (1 - float64(report.PoolFeeChanges)/float64(transitions))
		weightedScore += ScoreFeeStabilityWeight * clamp(stability, 0, 100)
		weight += ScoreFeeStabilityWeight
	}

	feeIncreasePenalty := recentFeeIncreasePenalty(report, now)
	highFeePenalty := highPoolFeePenalty(report)
	tlsCertificatePenalty := 0.0
	if report.TLSTiming.CertificateErrors > 0 {
		tlsCertificatePenalty = ScoreTLSCertificatePenalty
	}
	// Preserve fractional score differences for ranking and API consumers. The
	// dashboard deliberately rounds only when rendering the visible badge.
	score := round(clamp(weightedScore/weight-feeIncreasePenalty-highFeePenalty-tlsCertificatePenalty, 0, 100), 4)
	return overallScoreResult{
		Score: &score, RecentFeeIncreasePenalty: feeIncreasePenalty,
		HighFeePenalty: highFeePenalty, TLSCertificatePenalty: tlsCertificatePenalty,
	}
}

func applyOverallScore(report *model.PoolReport, now time.Time) {
	result := overallScore(*report, now)
	report.OverallScore = result.Score
	report.RecentFeeIncreasePenalty = result.RecentFeeIncreasePenalty
	report.HighFeePenalty = result.HighFeePenalty
	report.TLSCertificatePenalty = result.TLSCertificatePenalty
	report.ScoreOverrideReason = result.OverrideReason
}

func highPoolFeePenalty(report model.PoolReport) float64 {
	if report.Category != "solo" || report.LatestPoolFeePct == nil || *report.LatestPoolFeePct <= ScoreHighFeeThreshold {
		return 0
	}
	penalty := (*report.LatestPoolFeePct - ScoreHighFeeThreshold) * ScoreHighFeePenaltyPerPct
	return round(clamp(penalty, 0, ScoreHighFeeMaxPenalty), 1)
}

func recentFeeIncreasePenalty(report model.PoolReport, now time.Time) float64 {
	if report.Category != "solo" || report.LatestPoolFeePct == nil || report.PreviousPoolFeePct == nil ||
		report.PoolFeeLastChangedAt == nil || *report.LatestPoolFeePct <= *report.PreviousPoolFeePct {
		return 0
	}
	age := now.UTC().Sub(report.PoolFeeLastChangedAt.UTC())
	if age < 0 {
		age = 0
	}
	if age >= ScoreFeeIncreaseWindow {
		return 0
	}
	remaining := 1 - float64(age)/float64(ScoreFeeIncreaseWindow)
	return round(ScoreFeeIncreaseMaxPenalty*remaining, 1)
}

func responsivenessScore(report model.PoolReport) (float64, bool) {
	timings := [...]model.TimingStats{
		report.ConnectTiming,
		report.TLSTiming,
		report.SubscribeTiming,
		report.AuthorizeTiming,
	}
	total := 0.0
	count := 0
	for _, timing := range timings {
		if timing.MedianMS == nil {
			continue
		}
		total += scoreFromAnchors(*timing.MedianMS, responsivenessScoreAnchors)
		count++
	}
	if count == 0 {
		return 0, false
	}
	return total / float64(count), true
}

func scoreFromAnchors(value float64, anchors []scoreAnchor) float64 {
	if value <= anchors[0].value {
		return anchors[0].score
	}
	for index := 1; index < len(anchors); index++ {
		upper := anchors[index]
		if value > upper.value {
			continue
		}
		lower := anchors[index-1]
		position := (value - lower.value) / (upper.value - lower.value)
		return lower.score + position*(upper.score-lower.score)
	}
	return anchors[len(anchors)-1].score
}

func clamp(value, minimum, maximum float64) float64 {
	return math.Max(minimum, math.Min(maximum, value))
}
