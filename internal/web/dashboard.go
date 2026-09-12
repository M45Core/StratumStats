package web

import (
	"fmt"
	"hash/fnv"
	"sort"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

type dashboardPool struct {
	model.PoolReport
	Website             string           `json:"website,omitempty"`
	RowID               string           `json:"row_id"`
	SortName            string           `json:"sort_name"`
	LatencyClass        string           `json:"latency_class"`
	MiningLossClass     string           `json:"mining_loss_class"`
	UnsafeReason        string           `json:"unsafe_reason,omitempty"`
	WalletEvidence      string           `json:"wallet_evidence,omitempty"`
	IsSolo              bool             `json:"is_solo"`
	FeeSortValue        *float64         `json:"fee_sort_value"`
	CombinedVantage     bool             `json:"combined_vantage"`
	LatencyHistoryCount int              `json:"latency_history_count"`
	FeeChangeHistory    []feeChangePoint `json:"fee_change_history,omitempty"`
}

type feeChangePoint struct {
	model.MetricHistoryPoint
	Previous float64 `json:"previous"`
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
		latencyHistoryCount := min(len(report.TemplateLatencyHistory), poolHistoryLimit)
		// The chart is fetched separately when this endpoint's details open.
		report.TemplateLatencyHistory = nil
		isSolo := report.Category == "solo"
		var feeSortValue *float64
		if isSolo {
			feeSortValue = report.LatestPoolFeePct
		}
		pool := dashboardPool{
			PoolReport: report, Website: websiteByPoolID[report.PoolID], RowID: endpointRowID(report), SortName: report.PoolName + " " + report.Endpoint,
			LatencyClass: latencyClass(report.MedianMS), MiningLossClass: miningLossClass(report.EstimatedMiningLossPct),
			IsSolo: isSolo, FeeSortValue: feeSortValue, CombinedVantage: selectedVantage == "us-all",
			LatencyHistoryCount: latencyHistoryCount, FeeChangeHistory: buildFeeChangeHistory(report.PoolFeeHistory),
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
			// Positive worker-address evidence is required for the measured solo lists.
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
			for _, pair := range [][2]*float64{{left.MedianMS, right.MedianMS}, {left.P95MS, right.P95MS}} {
				if pair[0] == nil || pair[1] == nil {
					if pair[0] != pair[1] {
						return pair[0] != nil
					}
				} else if *pair[0] != *pair[1] {
					return *pair[0] < *pair[1]
				}
			}
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
