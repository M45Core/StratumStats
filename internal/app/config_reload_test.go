package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/M45Core/StratumStats/internal/ingest"
	"github.com/M45Core/StratumStats/internal/model"
)

func registryTestConfig(id, host string) model.Config {
	return model.Config{Pools: []model.Pool{{
		ID: id, Name: "Test Pool", Category: "shared", Status: "active",
		Endpoints: []model.Endpoint{{Host: host, Port: 3333}},
	}}}
}

func TestDecodeConfigRejectsUnknownAndTrailingFields(t *testing.T) {
	valid := `{"pools":[{"id":"pool","name":"Pool","endpoints":[{"host":"pool.example","port":3333,"tls":false}]}]}`
	if _, err := decodeConfig([]byte(valid)); err != nil {
		t.Fatalf("valid config rejected: %v", err)
	}
	if _, err := decodeConfig([]byte(strings.Replace(valid, `"name":"Pool"`, `"name":"Pool","nmae":"typo"`, 1))); err == nil {
		t.Fatal("unknown field accepted")
	}
	if _, err := decodeConfig([]byte(valid + `{}`)); err == nil {
		t.Fatal("trailing JSON value accepted")
	}
	if _, err := decodeConfig([]byte(`{"pools":[]}`)); err == nil {
		t.Fatal("empty registry accepted")
	}
}

func TestRegistryReloadRejectsInvalidAndAtomicallySwapsValidConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pools.json")
	initial := registryTestConfig("old", "old.example")
	encoded, err := json.Marshal(initial)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o640); err != nil {
		t.Fatal(err)
	}
	builds := 0
	builder := func(current, previous model.Config) (http.Handler, string, error) {
		builds++
		probeConfig, err := ingest.BuildProbeConfig(current.Pools)
		if err != nil {
			return nil, "", err
		}
		id := current.Pools[0].ID
		return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			_, _ = response.Write([]byte(id))
		}), probeConfig.ConfigRevision, nil
	}
	manager, err := newRegistryManager(path, initial, builder)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	response := httptest.NewRecorder()
	manager.public.ServeHTTP(response, request)
	if response.Body.String() != "old" {
		t.Fatalf("initial handler=%q", response.Body.String())
	}

	if err := os.WriteFile(path, []byte(`{"pools":[{"id":"broken"}]}`), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.reload(); err == nil {
		t.Fatal("invalid reload succeeded")
	}
	response = httptest.NewRecorder()
	manager.public.ServeHTTP(response, request)
	if response.Body.String() != "old" {
		t.Fatalf("invalid reload replaced handler with %q", response.Body.String())
	}

	updated := registryTestConfig("new", "new.example")
	encoded, err = json.Marshal(updated)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, encoded, 0o640); err != nil {
		t.Fatal(err)
	}
	revision, err := manager.reload()
	if err != nil {
		t.Fatal(err)
	}
	response = httptest.NewRecorder()
	manager.public.ServeHTTP(response, request)
	if response.Body.String() != "new" || revision == "" || builds != 2 {
		t.Fatalf("handler=%q revision=%q builds=%d", response.Body.String(), revision, builds)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0o640); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.save(encoded); err != nil {
		t.Fatal(err)
	}
	persisted, err := loadConfig(path)
	if err != nil || persisted.Pools[0].ID != "new" {
		t.Fatalf("persisted=%+v err=%v", persisted, err)
	}
}
