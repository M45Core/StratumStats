package model

import "time"

// Pool describes one operator and the endpoints tested on equal terms.
type Pool struct {
	ID              string      `json:"id"`
	Name            string      `json:"name"`
	Operator        string      `json:"operator,omitempty"`
	Website         string      `json:"website,omitempty"`
	Category        string      `json:"category,omitempty"`
	Status          string      `json:"status,omitempty"`
	AuthModel       string      `json:"auth_model,omitempty"`
	ProbeStatus     string      `json:"probe_status,omitempty"`
	Description     string      `json:"description,omitempty"`
	Products        []string    `json:"products,omitempty"`
	AdvertisedFee   string      `json:"advertised_fee,omitempty"`
	FeeCheckedAt    string      `json:"fee_checked_at,omitempty"`
	LastVerified    string      `json:"last_verified,omitempty"`
	ResearchSources []Reference `json:"research_sources,omitempty"`
	Sources         []string    `json:"sources,omitempty"`
	Endpoints       []Endpoint  `json:"endpoints"`
}

type Endpoint struct {
	Host    string   `json:"host"`
	Port    int      `json:"port"`
	TLS     bool     `json:"tls"`
	Region  string   `json:"region,omitempty"`
	Label   string   `json:"label,omitempty"`
	Sources []string `json:"sources,omitempty"`
}

type Reference struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type RegistrySource struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type Config struct {
	SchemaVersion int              `json:"schema_version,omitempty"`
	GeneratedFrom []RegistrySource `json:"generated_from,omitempty"`
	ResearchAsOf  string           `json:"research_as_of,omitempty"`
	Pools         []Pool           `json:"pools"`
}

// Observation is the immutable evidence used to produce reports. OffsetMS is
// relative to the first full template seen by the same vantage for a block.
type Observation struct {
	Version                int       `json:"version"`
	ObservedAt             time.Time `json:"observed_at"`
	Vantage                string    `json:"vantage"`
	BlockID                string    `json:"block_id"`
	PoolID                 string    `json:"pool_id"`
	Eligible               bool      `json:"eligible"`
	Arrived                bool      `json:"arrived"`
	OffsetMS               float64   `json:"offset_ms,omitempty"`
	EmptyFirst             bool      `json:"empty_first,omitempty"`
	ConnectMS              float64   `json:"connect_ms,omitempty"`
	TLS                    bool      `json:"tls"`
	ErrorCategory          string    `json:"error_category,omitempty"`
	CoinbaseAnalyzed       bool      `json:"coinbase_analyzed,omitempty"`
	WorkerWalletInCoinbase bool      `json:"worker_wallet_in_coinbase,omitempty"`
	CoinbaseTotalSats      uint64    `json:"coinbase_total_sats,omitempty"`
	WorkerPayoutSats       uint64    `json:"worker_payout_sats,omitempty"`
	EstimatedPoolFeePct    *float64  `json:"estimated_pool_fee_pct,omitempty"`
}

type PoolReport struct {
	PoolID          string   `json:"pool_id"`
	PoolName        string   `json:"pool_name"`
	Category        string   `json:"category,omitempty"`
	Sources         []string `json:"sources,omitempty"`
	Confidence      string   `json:"confidence"`
	Blocks          int      `json:"blocks"`
	Arrivals        int      `json:"arrivals"`
	MedianMS        *float64 `json:"median_ms"`
	P95MS           *float64 `json:"p95_ms"`
	Availability    float64  `json:"availability_pct"`
	TLSObserved     bool     `json:"tls_observed"`
	PayoutSamples   int      `json:"payout_samples"`
	DirectPayoutPct *float64 `json:"direct_coinbase_pct"`
	PoolFeePct      *float64 `json:"median_pool_fee_pct"`
	PoolFeeMinPct   *float64 `json:"min_pool_fee_pct"`
	PoolFeeMaxPct   *float64 `json:"max_pool_fee_pct"`
	FeeClass        string   `json:"observed_fee_class"`
	PayoutMode      string   `json:"payout_mode"`
}

type Snapshot struct {
	GeneratedAt time.Time    `json:"generated_at"`
	Methodology string       `json:"methodology_version"`
	Reports     []PoolReport `json:"reports"`
	Disclosure  []string     `json:"disclosure"`
}
