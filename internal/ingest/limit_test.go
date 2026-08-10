package ingest

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRateLimitRejectsExcessRequestsAndRefills(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	calls := 0
	handler := newRateLimitedHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusAccepted)
	}), func() time.Time { return now }, 1, 2, 2)

	for index, want := range []int{http.StatusAccepted, http.StatusAccepted, http.StatusTooManyRequests} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/ingest", nil))
		if response.Code != want {
			t.Fatalf("request %d status = %d, want %d", index+1, response.Code, want)
		}
		if want == http.StatusTooManyRequests && (response.Header().Get("Retry-After") != "1" || response.Header().Get("Cache-Control") != "no-store") {
			t.Fatalf("rate-limit headers = %v", response.Header())
		}
	}
	if calls != 2 {
		t.Fatalf("downstream calls = %d, want 2", calls)
	}

	now = now.Add(time.Second)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/ingest", nil))
	if response.Code != http.StatusAccepted || calls != 3 {
		t.Fatalf("refilled request status = %d, downstream calls = %d", response.Code, calls)
	}
}

func TestRateLimitCapsConcurrentRequests(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	entered := make(chan struct{})
	release := make(chan struct{})
	handler := newRateLimitedHandler(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		w.WriteHeader(http.StatusAccepted)
	}), func() time.Time { return now }, 1, 2, 1)

	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/v1/ingest", nil))
	}()
	<-entered

	second := httptest.NewRecorder()
	handler.ServeHTTP(second, httptest.NewRequest(http.MethodPost, "/api/v1/ingest", nil))
	if second.Code != http.StatusTooManyRequests {
		t.Fatalf("concurrent request status = %d, want %d", second.Code, http.StatusTooManyRequests)
	}
	close(release)
	<-firstDone
}
