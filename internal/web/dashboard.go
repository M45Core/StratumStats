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
	Website          string              `json:"website,omitempty"`
	RowID            string              `json:"row_id"`
	SortName         string              `json:"sort_name"`
	LatencyClass     string              `json:"latency_class"`
	MiningLossClass  string              `json:"mining_loss_class"`
	UnsafeReason     string              `json:"unsafe_reason,omitempty"`
	WalletEvidence   string              `json:"wallet_evidence,omitempty"`
	IsSolo           bool                `json:"is_solo"`
	FeeSortValue     *float64            `json:"fee_sort_value"`
	CombinedVantage  bool                `json:"combined_vantage"`
	LatencyChart     latencyHistoryChart `json:"latency_chart"`
	FeeChangeHistory []feeChangePoint    `json:"fee_change_history,omitempty"`
}

type feeChangePoint struct {
	model.MetricHistoryPoint
	Previous float64 `json:"previous"`
}

type latencyChartPoint struct {
	model.MetricHistoryPoint
	X          float64 `json:"x"`
	Y          float64 `json:"y"`
	LabelX     float64 `json:"label_x"`
	LabelY     float64 `json:"label_y"`
	TextAnchor string  `json:"text_anchor"`
}

type latencyHistoryChart struct {
	Points   []latencyChartPoint      `json:"points,omitempty"`
	Polyline string                   `json:"polyline,omitempty"`
	AreaPath string                   `json:"area_path,omitempty"`
	MaxValue float64                  `json:"max_value"`
	MidValue float64                  `json:"mid_value"`
	Start    model.MetricHistoryPoint `json:"start"`
	End      model.MetricHistoryPoint `json:"end"`
}

type dashboardPage struct {
	Snapshot           model.Snapshot  `json:"snapshot"`
	DataUpdatedAt      *time.Time      `json:"data_updated_at,omitempty"`
	Demo               bool            `json:"demo"`
	FreePools          []dashboardPool `json:"free_pools"`
	NormalPools        []dashboardPool `json:"normal_pools"`
	MissingWalletPools []dashboardPool `json:"missing_wallet_pools"`
	PendingWalletPools []dashboardPool `json:"pending_wallet_pools"`
	PPLNSPools         []dashboardPool `json:"pplns_pools"`
	OtherPools         []dashboardPool `json:"other_pools"`
	NoRecentDataPools  []dashboardPool `json:"no_recent_data_pools"`
	SelectedVantage    string          `json:"selected_vantage"`
	SelectedLabel      string          `json:"selected_label"`
	SelectedTransport  string          `json:"selected_transport"`
	VantageStatus      *vantageStatus  `json:"vantage_status,omitempty"`
	ConfigRevision     string          `json:"config_revision,omitempty"`
	AvailableVantages  map[string]bool `json:"available_vantages"`
	VantageOptions     []vantageOption `json:"vantage_options"`
}

type vantageOption struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	City    string `json:"city"`
	Country string `json:"country"`
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
	for _, region := range model.ProductionRegions() {
		page.VantageOptions = append(page.VantageOptions, vantageOption{ID: region.Vantage, Label: region.Label, City: region.City, Country: region.Country})
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
			IsSolo: isSolo, FeeSortValue: feeSortValue, CombinedVantage: selectedVantage == "us-all",
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
	if status != nil && status.LastSuccessfulRunAt != nil {
		page.DataUpdatedAt = status.LastSuccessfulRunAt
	} else {
		page.DataUpdatedAt = latestPoolUpdate(displayedPools)
	}
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
