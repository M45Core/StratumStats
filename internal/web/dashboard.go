package web

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

type dashboardPool struct {
	model.PoolReport
	Website          string
	RowID            string
	SortName         string
	LatencyClass     string
	MiningLossClass  string
	UnsafeReason     string
	WalletEvidence   string
	IsSolo           bool
	TLSConfigured    bool
	FeeSortValue     *float64
	LatencyChart     latencyHistoryChart
	FeeChangeHistory []feeChangePoint
}

type feeChangePoint struct {
	model.MetricHistoryPoint
	Previous float64
}

type latencyChartPoint struct {
	model.MetricHistoryPoint
	X          float64
	Y          float64
	LabelX     float64
	LabelY     float64
	TextAnchor string
}

type latencyHistoryChart struct {
	Points   []latencyChartPoint
	Polyline string
	AreaPath string
	MaxValue float64
	MidValue float64
	Start    model.MetricHistoryPoint
	End      model.MetricHistoryPoint
}

type dashboardPage struct {
	Snapshot                    model.Snapshot
	DataUpdatedAt               *time.Time
	Demo                        bool
	FreePools                   []dashboardPool
	NormalPools                 []dashboardPool
	MissingWalletPools          []dashboardPool
	PendingWalletPools          []dashboardPool
	PPLNSPools                  []dashboardPool
	OtherPools                  []dashboardPool
	NoRecentDataPools           []dashboardPool
	FreePoolsUpdatedAt          *time.Time
	NormalPoolsUpdatedAt        *time.Time
	MissingWalletPoolsUpdatedAt *time.Time
	PendingWalletPoolsUpdatedAt *time.Time
	PPLNSPoolsUpdatedAt         *time.Time
	OtherPoolsUpdatedAt         *time.Time
	NoRecentDataPoolsUpdatedAt  *time.Time
	SelectedVantage             string
	SelectedLabel               string
	SelectedTransport           string
	VantageStatus               *vantageStatus
	AvailableVantages           map[string]bool
	ShowUSCombined              bool
}

func buildDashboardPage(snapshot model.Snapshot, pools []model.Pool, demo bool, selectedVantage string, status *vantageStatus, selectedTransport string) dashboardPage {
	selectedLabel := "All measurements"
	if label := vantageLabels[selectedVantage]; label != "" {
		selectedLabel = label
	}
	page := dashboardPage{
		Snapshot:          snapshot,
		Demo:              demo,
		SelectedVantage:   selectedVantage,
		SelectedLabel:     selectedLabel,
		SelectedTransport: selectedTransport,
		VantageStatus:     status,
	}
	websiteByPoolID := make(map[string]string, len(pools))
	for _, pool := range pools {
		websiteByPoolID[pool.ID] = pool.Website
	}
	displayedPools := make([]dashboardPool, 0, len(snapshot.Reports))
	for _, report := range snapshot.Reports {
		if report.EndpointTLS != (selectedTransport == "tls") {
			continue
		}
		isSolo := report.Category == "solo"
		var feeSortValue *float64
		if isSolo {
			feeSortValue = report.LatestPoolFeePct
		}
		pool := dashboardPool{
			PoolReport: report, Website: websiteByPoolID[report.PoolID], RowID: endpointRowID(report), SortName: report.PoolName + " " + report.Endpoint,
			LatencyClass: latencyClass(report.MedianMS), MiningLossClass: miningLossClass(report.EstimatedMiningLossPct),
			IsSolo: isSolo, TLSConfigured: report.EndpointTLS, FeeSortValue: feeSortValue,
			LatencyChart: buildLatencyHistoryChart(report.TemplateLatencyHistory), FeeChangeHistory: buildFeeChangeHistory(report.PoolFeeHistory),
		}
		displayedPools = append(displayedPools, pool)
		if report.MedianMS == nil {
			page.NoRecentDataPools = append(page.NoRecentDataPools, pool)
			continue
		}
		if report.Category != "solo" {
			if offersPPLNS(report.Products) {
				page.PPLNSPools = append(page.PPLNSPools, pool)
			} else {
				page.OtherPools = append(page.OtherPools, pool)
			}
			continue
		}
		switch report.WorkerAddressStatus {
		case "always_observed":
			// Positive worker-address evidence is required for the solo lists.
		case "not_observed":
			pool.UnsafeReason = fmt.Sprintf("worker wallet not found in %d decoded coinbase payouts", report.CoinbaseSamples)
			pool.WalletEvidence = "missing"
		case "varied":
			pool.UnsafeReason = fmt.Sprintf("worker wallet not found in some of %d decoded coinbase payouts", report.CoinbaseSamples)
			pool.WalletEvidence = "missing"
		default:
			pool.UnsafeReason = "worker wallet payout not yet verified"
			pool.WalletEvidence = "pending"
		}
		if pool.UnsafeReason != "" {
			if pool.WalletEvidence == "missing" {
				page.MissingWalletPools = append(page.MissingWalletPools, pool)
			} else {
				page.PendingWalletPools = append(page.PendingWalletPools, pool)
			}
		} else if report.LatestPoolFeePct != nil && *report.LatestPoolFeePct == 0 {
			page.FreePools = append(page.FreePools, pool)
		} else {
			page.NormalPools = append(page.NormalPools, pool)
		}
	}
	sortByOverallScore(page.FreePools)
	sortByOverallScore(page.NormalPools)
	sortByOverallScore(page.MissingWalletPools)
	sortByOverallScore(page.PendingWalletPools)
	sortByOverallScore(page.PPLNSPools)
	sortByOverallScore(page.OtherPools)
	sortByTemplateLatency(page.NoRecentDataPools)
	page.FreePoolsUpdatedAt = latestPoolUpdate(page.FreePools)
	page.NormalPoolsUpdatedAt = latestPoolUpdate(page.NormalPools)
	page.MissingWalletPoolsUpdatedAt = latestPoolUpdate(page.MissingWalletPools)
	page.PendingWalletPoolsUpdatedAt = latestPoolUpdate(page.PendingWalletPools)
	page.PPLNSPoolsUpdatedAt = latestPoolUpdate(page.PPLNSPools)
	page.OtherPoolsUpdatedAt = latestPoolUpdate(page.OtherPools)
	page.NoRecentDataPoolsUpdatedAt = latestPoolUpdate(page.NoRecentDataPools)
	page.DataUpdatedAt = latestPoolUpdate(displayedPools)
	return page
}

func endpointRowID(report model.PoolReport) string {
	hash := fnv.New64a()
	_, _ = fmt.Fprintf(hash, "%s\x00%s\x00%t", report.PoolID, report.Endpoint, report.EndpointTLS)
	return fmt.Sprintf("%s-%x", report.PoolID, hash.Sum64())
}

func latestPoolUpdate(pools []dashboardPool) *time.Time {
	var latest time.Time
	for _, pool := range pools {
		if pool.LastObservedAt != nil && pool.LastObservedAt.After(latest) {
			latest = pool.LastObservedAt.UTC()
		}
	}
	if latest.IsZero() {
		return nil
	}
	return &latest
}

func sortByOverallScore(pools []dashboardPool) {
	sort.SliceStable(pools, func(i, j int) bool {
		left, right := pools[i], pools[j]
		if left.OverallScore == nil || right.OverallScore == nil {
			if left.OverallScore == nil && right.OverallScore == nil {
				return left.SortName < right.SortName
			}
			return left.OverallScore != nil
		}
		if *left.OverallScore == *right.OverallScore {
			return left.SortName < right.SortName
		}
		return *left.OverallScore > *right.OverallScore
	})
}

func offersPPLNS(products []string) bool {
	for _, product := range products {
		if product == "PPLNS" || product == "PPLNS sharechain" {
			return true
		}
	}
	return false
}

func sortByTemplateLatency(pools []dashboardPool) {
	sort.SliceStable(pools, func(i, j int) bool {
		left, right := pools[i], pools[j]
		if left.MedianMS == nil || right.MedianMS == nil {
			if left.MedianMS == nil && right.MedianMS == nil {
				return left.SortName < right.SortName
			}
			return left.MedianMS != nil
		}
		if *left.MedianMS == *right.MedianMS {
			return left.SortName < right.SortName
		}
		return *left.MedianMS < *right.MedianMS
	})
}

func latencyClass(value *float64) string {
	if value == nil {
		return "latency-none"
	}
	thresholds := [...]float64{100, 200, 300, 500, 750, 1000, 1500, 2500, 4000}
	for index, threshold := range thresholds {
		if *value <= threshold {
			return fmt.Sprintf("latency-%d", index+1)
		}
	}
	return "latency-10"
}

func miningLossClass(value *float64) string {
	if value == nil {
		return "loss-none"
	}
	thresholds := [...]float64{0.1, 0.25, 0.5, 0.75, 1, 1.5, 2.5, 3.5, 5}
	for index, threshold := range thresholds {
		if *value <= threshold {
			return fmt.Sprintf("loss-%d", index+1)
		}
	}
	return "loss-10"
}

func buildFeeChangeHistory(history []model.MetricHistoryPoint) []feeChangePoint {
	if len(history) < 2 {
		return nil
	}
	changes := make([]feeChangePoint, 0, len(history)-1)
	previous := history[0].Value
	for _, point := range history[1:] {
		if point.Value == previous {
			continue
		}
		changes = append(changes, feeChangePoint{MetricHistoryPoint: point, Previous: previous})
		previous = point.Value
	}
	return changes
}

func buildLatencyHistoryChart(history []model.MetricHistoryPoint) latencyHistoryChart {
	if len(history) == 0 {
		return latencyHistoryChart{}
	}
	const (
		chartLeft   = 56.0
		chartRight  = 624.0
		chartTop    = 18.0
		chartBottom = 178.0
	)
	maximum := latencyChartCeiling(historyMaximum(history))
	chart := latencyHistoryChart{
		Points:   make([]latencyChartPoint, 0, len(history)),
		MaxValue: maximum, MidValue: maximum / 2,
		Start: history[0], End: history[len(history)-1],
	}
	polyline := new(strings.Builder)
	for index, sample := range history {
		x := (chartLeft + chartRight) / 2
		if len(history) > 1 {
			x = chartLeft + float64(index)*(chartRight-chartLeft)/float64(len(history)-1)
		}
		y := chartBottom - sample.Value/maximum*(chartBottom-chartTop)
		labelY := y - 11
		if labelY < chartTop+7 {
			labelY = y + 21
		}
		labelX, textAnchor := x, "middle"
		if index == 0 {
			labelX, textAnchor = x+5, "start"
		} else if index == len(history)-1 {
			labelX, textAnchor = x-5, "end"
		}
		point := latencyChartPoint{MetricHistoryPoint: sample, X: x, Y: y, LabelX: labelX, LabelY: labelY, TextAnchor: textAnchor}
		chart.Points = append(chart.Points, point)
		if index > 0 {
			polyline.WriteByte(32)
		}
		fmt.Fprintf(polyline, "%.1f,%.1f", x, y)
	}
	chart.Polyline = polyline.String()
	last := chart.Points[len(chart.Points)-1]
	chart.AreaPath = fmt.Sprintf("M %.1f %.1f L %s L %.1f %.1f Z", chart.Points[0].X, chartBottom, chart.Polyline, last.X, chartBottom)
	return chart
}

func latencyChartCeiling(maximum float64) float64 {
	step := 10.0
	switch {
	case maximum > 5000:
		step = 1000
	case maximum > 1000:
		step = 500
	case maximum > 500:
		step = 100
	case maximum > 100:
		step = 50
	}
	return math.Max(step, math.Ceil(maximum/step)*step)
}

func historyMaximum(history []model.MetricHistoryPoint) float64 {
	maximum := 1.0
	for _, point := range history {
		if point.Value > maximum {
			maximum = point.Value
		}
	}
	return maximum
}
