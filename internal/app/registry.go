package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"

	"github.com/M45Core/StratumStats/internal/model"
)

type handlerSlot struct {
	handler http.Handler
}

type swapHandler struct {
	current atomic.Pointer[handlerSlot]
}

func newSwapHandler(handler http.Handler) *swapHandler {
	swapped := &swapHandler{}
	swapped.swap(handler)
	return swapped
}

func (handler *swapHandler) swap(next http.Handler) {
	handler.current.Store(&handlerSlot{handler: next})
}

func (handler *swapHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	handler.current.Load().handler.ServeHTTP(response, request)
}

type registryBuilder func(current, previous model.Config) (http.Handler, string, error)

type registryManager struct {
	mu       sync.Mutex
	path     string
	current  model.Config
	revision string
	public   *swapHandler
	build    registryBuilder
}

func newRegistryManager(path string, current model.Config, build registryBuilder) (*registryManager, error) {
	handler, revision, err := build(current, model.Config{})
	if err != nil {
		return nil, err
	}
	return &registryManager{
		path: path, current: current, revision: revision,
		public: newSwapHandler(handler), build: build,
	}, nil
}

func (manager *registryManager) reload() (string, error) {
	cfg, err := loadConfig(manager.path)
	if err != nil {
		return "", err
	}
	return manager.apply(cfg, false)
}

func (manager *registryManager) save(raw []byte) (string, error) {
	cfg, err := decodeConfig(raw)
	if err != nil {
		return "", err
	}
	return manager.apply(cfg, true)
}

func (manager *registryManager) apply(cfg model.Config, persist bool) (string, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()

	if reflect.DeepEqual(cfg, manager.current) {
		if persist {
			if err := writeConfigAtomically(manager.path, cfg); err != nil {
				return "", err
			}
		}
		return manager.revision, nil
	}
	handler, revision, err := manager.build(cfg, manager.current)
	if err != nil {
		return "", err
	}
	if persist {
		if err := writeConfigAtomically(manager.path, cfg); err != nil {
			return "", err
		}
	}
	manager.current = cfg
	manager.revision = revision
	manager.public.swap(handler)
	return revision, nil
}

func (manager *registryManager) json() ([]byte, string, error) {
	manager.mu.Lock()
	defer manager.mu.Unlock()
	encoded, err := json.MarshalIndent(manager.current, "", "  ")
	if err != nil {
		return nil, "", err
	}
	return append(encoded, '\n'), manager.revision, nil
}

func writeConfigAtomically(path string, cfg model.Config) error {
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, ".pools-*.json")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o640); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(encoded); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	directoryHandle, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open config directory: %w", err)
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return fmt.Errorf("sync config directory: %w", err)
	}
	return nil
}
