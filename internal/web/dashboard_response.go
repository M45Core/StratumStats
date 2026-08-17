package web

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

type cachedDashboardResponse struct {
	body     []byte
	gzipBody []byte
	etag     string
}

type dashboardResponseCache struct {
	mu      sync.RWMutex
	rebuild sync.Mutex
	entries map[string]cachedDashboardResponse
}

var dashboardGzipWriters = sync.Pool{
	New: func() any { return gzip.NewWriter(io.Discard) },
}

// Regional Scouts finalize the same Bitcoin block about 30 seconds after first
// observing it, then upload their observation batch and terminal run record.
// Ten seconds groups that multi-region burst, matches the dashboard client's
// polling interval, and remains short relative to Bitcoin's 10-minute mean
// block interval (about 1.65% of intervals are shorter).
const dashboardRefreshCoalesceWindow = 10 * time.Second

// dashboardRefreshScheduler coalesces bursts of accepted ingest requests. At
// most one rebuild runs at a time, and arrivals during that rebuild request a
// single follow-up pass instead of queuing one full rebuild per batch.
type dashboardRefreshScheduler struct {
	mu         sync.Mutex
	running    bool
	generation uint64
	delay      time.Duration
	refresh    func() error
	onError    func(error)
}

func (scheduler *dashboardRefreshScheduler) schedule() {
	scheduler.mu.Lock()
	scheduler.generation++
	if scheduler.running {
		scheduler.mu.Unlock()
		return
	}
	scheduler.running = true
	scheduler.mu.Unlock()
	go scheduler.run()
}

func (scheduler *dashboardRefreshScheduler) run() {
	delay := scheduler.delay
	if delay <= 0 {
		delay = dashboardRefreshCoalesceWindow
	}
	for {
		time.Sleep(delay)
		scheduler.mu.Lock()
		generation := scheduler.generation
		scheduler.mu.Unlock()

		if err := scheduler.refresh(); err != nil && scheduler.onError != nil {
			scheduler.onError(err)
		}

		scheduler.mu.Lock()
		if scheduler.generation == generation {
			scheduler.running = false
			scheduler.mu.Unlock()
			return
		}
		scheduler.mu.Unlock()
	}
}

func encodeDashboard(page dashboardPage) (cachedDashboardResponse, error) {
	body, err := json.Marshal(page)
	if err != nil {
		return cachedDashboardResponse{}, err
	}
	body = append(body, '\n')
	var compressed bytes.Buffer
	compressor := dashboardGzipWriters.Get().(*gzip.Writer)
	compressor.Reset(&compressed)
	defer func() {
		compressor.Reset(io.Discard)
		dashboardGzipWriters.Put(compressor)
	}()
	if _, err := compressor.Write(body); err != nil {
		return cachedDashboardResponse{}, err
	}
	if err := compressor.Close(); err != nil {
		return cachedDashboardResponse{}, err
	}
	sum := sha256.Sum256(body)
	return cachedDashboardResponse{body: body, gzipBody: compressed.Bytes(), etag: `W/"` + hex.EncodeToString(sum[:]) + `"`}, nil
}

func (cache *dashboardResponseCache) response(key string) (cachedDashboardResponse, bool) {
	cache.mu.RLock()
	defer cache.mu.RUnlock()
	response, ok := cache.entries[key]
	return response, ok
}

func (cache *dashboardResponseCache) rebuildFrom(snapshots *snapshotCache, pools []model.Pool, demo bool, configRevision string) error {
	cache.rebuild.Lock()
	defer cache.rebuild.Unlock()
	now := time.Now().UTC()
	observations, err := snapshots.records(now)
	if err != nil {
		return err
	}
	available := availableVantages(observations)
	statuses := buildVantageStatuses(observations, now, configRevision)
	if !demo {
		hideStaleRegionalVantages(available, statuses)
	}
	entries := make(map[string]cachedDashboardResponse, (len(vantageLabels)+1)*2)
	vantages := make([]string, 0, len(vantageLabels)+1)
	vantages = append(vantages, "")
	for vantage := range vantageLabels {
		vantages = append(vantages, vantage)
	}
	for _, vantage := range vantages {
		for _, transport := range []string{"plain", "tls"} {
			page, err := buildDashboard(snapshots, pools, demo, vantage, transport, now, available, statuses, configRevision)
			if err != nil {
				return err
			}
			response, err := encodeDashboard(page)
			if err != nil {
				return err
			}
			entries[vantage+"\x00"+transport] = response
		}
	}
	cache.mu.Lock()
	cache.entries = entries
	cache.mu.Unlock()
	return nil
}

func writeCachedDashboard(w http.ResponseWriter, r *http.Request, response cachedDashboardResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("ETag", response.etag)
	w.Header().Set("Vary", "Accept-Encoding")
	if ifNoneMatch(r.Header.Get("If-None-Match"), response.etag) || weakETagEqual(r.URL.Query().Get("generation"), response.etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	body := response.body
	if acceptsGzip(r.Header.Get("Accept-Encoding")) {
		w.Header().Set("Content-Encoding", "gzip")
		body = response.gzipBody
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	_, _ = w.Write(body)
}

func ifNoneMatch(header, current string) bool {
	for _, candidate := range strings.Split(header, ",") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "*" || weakETagEqual(candidate, current) {
			return true
		}
	}
	return false
}

func weakETagEqual(left, right string) bool {
	left = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(left), "W/"))
	right = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(right), "W/"))
	return left != "" && left == right
}

func acceptsGzip(header string) bool {
	for _, part := range strings.Split(header, ",") {
		fields := strings.Split(strings.TrimSpace(part), ";")
		if strings.EqualFold(strings.TrimSpace(fields[0]), "gzip") {
			for _, parameter := range fields[1:] {
				name, value, found := strings.Cut(strings.TrimSpace(parameter), "=")
				quality, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
				if found && strings.EqualFold(name, "q") && err == nil && quality == 0 {
					return false
				}
			}
			return true
		}
	}
	return false
}

func buildDashboard(cache *snapshotCache, pools []model.Pool, demo bool, requestedVantage, transport string, now time.Time, available map[string]bool, statuses vantageStatusResponse, configRevision string) (dashboardPage, error) {
	vantage := requestedVantage
	if vantage == "" {
		if !demo && available["unknown"] {
			vantage = "unknown"
		} else {
			for _, candidate := range vantageOrder {
				if available[candidate] {
					vantage = candidate
					break
				}
			}
			if vantage == "" {
				vantage = vantageOrder[0]
			}
		}
	}
	snapshot, err := cache.snapshot(vantage, now)
	if err != nil {
		return dashboardPage{}, err
	}
	page := buildDashboardPage(snapshot, pools, demo, vantage, selectedVantageStatus(statuses, vantage), transport)
	page.ConfigRevision = configRevision
	// Reports are represented once in the transport-filtered dashboard groups.
	// Keeping the unfiltered copy in Snapshot nearly doubles the refresh payload.
	page.Snapshot.Reports = nil
	page.Snapshot.Disclosure = nil
	page.AvailableVantages = available
	return page, nil
}
