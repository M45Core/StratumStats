package web

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/proofofmike/stratumstats/internal/model"
)

type dashboardPool struct {
	model.PoolReport
	LatencyClass     string
	UnsafeReason     string
	AdvertisedFee    string
	FeeCheckedAt     string
	IsSolo           bool
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
	Snapshot           model.Snapshot
	Demo               bool
	FreePools          []dashboardPool
	NormalPools        []dashboardPool
	UnsafePools        []dashboardPool
	PPLNSPools         []dashboardPool
	OtherPools         []dashboardPool
	PoolCount          int
	BlocksObserved     int
	PoolsWithBlockData int
	UnsafeCount        int
	SelectedVantage    string
	SelectedLabel      string
	VantageStatus      *vantageStatus
}

func buildDashboardPage(snapshot model.Snapshot, pools []model.Pool, demo bool, selectedVantage string, status *vantageStatus) dashboardPage {
	selectedLabel := "All measurements"
	if label := vantageLabels[selectedVantage]; label != "" {
		selectedLabel = label
	}
	page := dashboardPage{
		Snapshot:        snapshot,
		Demo:            demo,
		PoolCount:       len(snapshot.Reports),
		BlocksObserved:  snapshot.BlocksObserved,
		SelectedVantage: selectedVantage,
		SelectedLabel:   selectedLabel,
		VantageStatus:   status,
	}
	poolMetadata := make(map[string]model.Pool, len(pools))
	for _, pool := range pools {
		poolMetadata[pool.ID] = pool
	}
	for _, report := range snapshot.Reports {
		if report.MedianMS == nil {
			continue
		}
		page.PoolsWithBlockData++
		metadata := poolMetadata[report.PoolID]
		isSolo := report.Category == "solo"
		feeSortValue := report.LatestPoolFeePct
		if !isSolo {
			feeSortValue = advertisedFeeSortValue(metadata.AdvertisedFee)
		}
		pool := dashboardPool{
			PoolReport: report, LatencyClass: latencyClass(report.MedianMS),
			AdvertisedFee: metadata.AdvertisedFee, FeeCheckedAt: metadata.FeeCheckedAt,
			IsSolo: isSolo, FeeSortValue: feeSortValue,
			LatencyChart: buildLatencyHistoryChart(report.TemplateLatencyHistory), FeeChangeHistory: buildFeeChangeHistory(report.PoolFeeHistory),
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
		case "varied":
			pool.UnsafeReason = fmt.Sprintf("worker wallet not found in some of %d decoded coinbase payouts", report.CoinbaseSamples)
		default:
			pool.UnsafeReason = "worker wallet payout not yet verified"
		}
		if pool.UnsafeReason != "" {
			page.UnsafeCount++
		}
		if pool.UnsafeReason != "" {
			page.UnsafePools = append(page.UnsafePools, pool)
		} else if report.LatestPoolFeePct != nil && *report.LatestPoolFeePct == 0 {
			page.FreePools = append(page.FreePools, pool)
		} else {
			page.NormalPools = append(page.NormalPools, pool)
		}
	}
	sortByTemplateLatency(page.FreePools)
	sortByTemplateLatency(page.NormalPools)
	sortByTemplateLatency(page.UnsafePools)
	sortByTemplateLatency(page.PPLNSPools)
	sortByTemplateLatency(page.OtherPools)
	return page
}

var advertisedPercentage = regexp.MustCompile("([0-9]+(?:[.][0-9]+)?)[[:space:]]*%")

func advertisedFeeSortValue(fee string) *float64 {
	match := advertisedPercentage.FindStringSubmatch(fee)
	if len(match) != 2 {
		return nil
	}
	value, err := strconv.ParseFloat(match[1], 64)
	if err != nil {
		return nil
	}
	result := new(float64)
	*result = value
	return result
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
				return left.PoolName < right.PoolName
			}
			return left.MedianMS != nil
		}
		if *left.MedianMS == *right.MedianMS {
			return left.PoolName < right.PoolName
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
