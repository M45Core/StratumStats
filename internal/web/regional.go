package web

import (
	"net/http"
	"sync"
	"time"

	"github.com/proofofmike/stratumstats/internal/ingest"
	"github.com/proofofmike/stratumstats/internal/model"
	"github.com/proofofmike/stratumstats/internal/report"
)

const reportCacheTTL = 5 * time.Second

var vantageLabels = map[string]string{
	"unknown":    "Local",
	"us-all":     "US combined",
	"us-west":    "US West",
	"us-central": "US Central",
	"us-east":    "US East",
	"europe":     "Europe",
}

var vantageOrder = []string{"us-west", "us-central", "us-east", "europe"}

type snapshotCache struct {
	mu           sync.Mutex
	pools        []model.Pool
	load         func() ([]model.Observation, error)
	loadedAt     time.Time
	observations []model.Observation
	snapshots    map[string]model.Snapshot
}

func (cache *snapshotCache) snapshot(vantage string, now time.Time) (model.Snapshot, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err := cache.refresh(now); err != nil {
		return model.Snapshot{}, err
	}
	if snapshot, ok := cache.snapshots[vantage]; ok {
		return snapshot, nil
	}
	var snapshot model.Snapshot
	if vantage == "" {
		snapshot = report.Compute(cache.pools, cache.observations, now)
	} else if vantage == "us-all" {
		snapshot = report.ComputeVantages(cache.pools, cache.observations, map[string]bool{"us-west": true, "us-central": true, "us-east": true}, now)
	} else {
		snapshot = report.ComputeVantage(cache.pools, cache.observations, vantage, now)
	}
	cache.snapshots[vantage] = snapshot
	return snapshot, nil
}

func (cache *snapshotCache) records(now time.Time) ([]model.Observation, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if err := cache.refresh(now); err != nil {
		return nil, err
	}
	return append([]model.Observation(nil), cache.observations...), nil
}

func (cache *snapshotCache) refresh(now time.Time) error {
	if cache.snapshots != nil && now.Sub(cache.loadedAt) < reportCacheTTL {
		return nil
	}
	observations, err := cache.load()
	if err != nil {
		return err
	}
	cache.loadedAt = now
	cache.observations = observations
	cache.snapshots = make(map[string]model.Snapshot)
	return nil
}

func (cache *snapshotCache) invalidate() {
	cache.mu.Lock()
	cache.loadedAt = time.Time{}
	cache.snapshots = nil
	cache.mu.Unlock()
}

func validVantage(vantage string) bool {
	if vantage == "" {
		return true
	}
	_, ok := vantageLabels[vantage]
	return ok
}

type vantageStatus struct {
	ID                  string     `json:"id"`
	Label               string     `json:"label"`
	MeasurementMode     string     `json:"measurement_mode"`
	LastSuccessfulRunAt *time.Time `json:"last_successful_run_at,omitempty"`
	LastObservationAt   *time.Time `json:"last_observation_at,omitempty"`
	ConfigRevision      string     `json:"config_revision,omitempty"`
	Blocks              int        `json:"blocks"`
	ProtocolAttempts    int        `json:"protocol_attempts"`
	DroppedObservations int        `json:"dropped_observations"`
	Incomplete          bool       `json:"incomplete"`
	Stale               bool       `json:"stale"`
}

type vantageStatusResponse struct {
	GeneratedAt time.Time       `json:"generated_at"`
	Vantages    []vantageStatus `json:"vantages"`
}

func buildVantageStatuses(observations []model.Observation, now time.Time) vantageStatusResponse {
	statuses := make(map[string]*vantageStatus, len(vantageOrder))
	blocks := make(map[string]map[string]bool, len(vantageOrder))
	latestRuns := make(map[string]time.Time, len(vantageOrder))
	seen := make(map[string]bool)
	for _, vantage := range vantageOrder {
		statuses[vantage] = &vantageStatus{ID: vantage, Label: vantageLabels[vantage], MeasurementMode: "scheduled", Stale: true}
		blocks[vantage] = make(map[string]bool)
	}
	for _, observation := range observations {
		if observation.ObservationID != "" {
			if seen[observation.ObservationID] {
				continue
			}
			seen[observation.ObservationID] = true
		}
		status := statuses[observation.Vantage]
		if status == nil || observation.Source != ingest.RemoteSource {
			continue
		}
		if status.LastObservationAt == nil || observation.ObservedAt.After(*status.LastObservationAt) {
			observedAt := observation.ObservedAt.UTC()
			status.LastObservationAt = &observedAt
		}
		if observation.RecordType == model.RecordTypeProtocol {
			status.ProtocolAttempts++
		}
		if observation.RecordType == "" && observation.BlockID != "" {
			blocks[observation.Vantage][observation.BlockID] = true
		}
		if observation.RecordType == model.RecordTypeProbeRun {
			if observation.RunStatus == "ok" {
				if status.LastSuccessfulRunAt == nil || observation.ObservedAt.After(*status.LastSuccessfulRunAt) {
					runAt := observation.ObservedAt.UTC()
					status.LastSuccessfulRunAt = &runAt
				}
			}
			if latestRuns[observation.Vantage].IsZero() || observation.ObservedAt.After(latestRuns[observation.Vantage]) {
				latestRuns[observation.Vantage] = observation.ObservedAt
				status.ConfigRevision = observation.ConfigRevision
				status.DroppedObservations = observation.DroppedObservations
				status.Incomplete = observation.RunStatus != "ok" || observation.DroppedObservations > 0
			}
		}
	}
	response := vantageStatusResponse{GeneratedAt: now.UTC()}
	for _, vantage := range vantageOrder {
		status := statuses[vantage]
		status.Blocks = len(blocks[vantage])
		status.Stale = status.LastSuccessfulRunAt == nil || now.Sub(*status.LastSuccessfulRunAt) > 2*time.Hour
		response.Vantages = append(response.Vantages, *status)
	}
	return response
}

func selectedVantageStatus(statuses vantageStatusResponse, vantage string) *vantageStatus {
	if vantage == "" {
		return nil
	}
	for index := range statuses.Vantages {
		if statuses.Vantages[index].ID == vantage {
			return &statuses.Vantages[index]
		}
	}
	return nil
}

func availableVantages(observations []model.Observation) map[string]bool {
	available := make(map[string]bool, len(vantageOrder))
	for _, observation := range observations {
		if _, known := vantageLabels[observation.Vantage]; known {
			available[observation.Vantage] = true
		}
	}
	return available
}

func hideStaleRegionalVantages(available map[string]bool, statuses vantageStatusResponse) {
	for _, status := range statuses.Vantages {
		if status.Stale {
			delete(available, status.ID)
		}
	}
}

type statusResponseWriter struct {
	http.ResponseWriter
	status int
}

func (writer *statusResponseWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *statusResponseWriter) Write(body []byte) (int, error) {
	if writer.status == 0 {
		writer.status = 200
	}
	return writer.ResponseWriter.Write(body)
}
