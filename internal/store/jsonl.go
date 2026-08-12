package store

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/M45Core/StratumStats/internal/model"
)

func Load(path string) ([]model.Observation, error) {
	return LoadSince(path, time.Time{})
}

// LoadSince retains only current-schema observations at or after cutoff. JSONL
// is sequential, so discarded lines are scanned and decoded but not kept.
func LoadSince(path string, cutoff time.Time) ([]model.Observation, error) {
	f, err := os.Open(path) // #nosec G304 -- the local operator selects the telemetry path.
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []model.Observation
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		var o model.Observation
		if err := json.Unmarshal(s.Bytes(), &o); err != nil {
			return nil, err
		}
		if o.Version != model.ObservationVersion {
			continue
		}
		if !cutoff.IsZero() && o.ObservedAt.Before(cutoff) {
			continue
		}
		out = append(out, o)
	}
	return out, s.Err()
}

func Append(path string, observations []model.Observation) error {
	if len(observations) == 0 {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	var encoded bytes.Buffer
	e := json.NewEncoder(&encoded)
	for _, o := range observations {
		if err := e.Encode(o); err != nil {
			return err
		}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600) // #nosec G304 -- the local operator selects the telemetry path.
	if err != nil {
		return err
	}
	defer f.Close()
	written, err := f.Write(encoded.Bytes())
	if err != nil {
		return err
	}
	if written != encoded.Len() {
		return io.ErrShortWrite
	}
	return f.Sync()
}

// Appender serializes batches bound for one JSONL file. Encoding completes
// before the append starts, so validation/encoding failures cannot leave a
// partial batch in the file.
type Appender struct {
	Path string
	mu   sync.Mutex
}

func (a *Appender) LoadSince(cutoff time.Time) ([]model.Observation, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return LoadSince(a.Path, cutoff)
}

func (a *Appender) Append(observations []model.Observation) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Append(a.Path, observations)
}

type CompactionResult struct {
	Compacted bool
	Removed   int
	Retained  int
}

// CompactBefore rewrites the JSONL atomically when its oldest observation is
// before triggerCutoff. The replacement keeps only current-schema records at
// or after retainCutoff, matching LoadSince's report-visible data set.
func (a *Appender) CompactBefore(retainCutoff, triggerCutoff time.Time) (CompactionResult, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	oldest, exists, err := oldestObservation(a.Path)
	if err != nil || !exists || !oldest.Before(triggerCutoff) {
		return CompactionResult{}, err
	}
	return compact(a.Path, retainCutoff)
}

func oldestObservation(path string) (time.Time, bool, error) {
	f, err := os.Open(path) // #nosec G304 -- the local operator selects the telemetry path.
	if errors.Is(err, os.ErrNotExist) {
		return time.Time{}, false, nil
	}
	if err != nil {
		return time.Time{}, false, err
	}
	defer f.Close()

	var oldest time.Time
	exists := false
	s := bufio.NewScanner(f)
	s.Buffer(make([]byte, 64*1024), 1024*1024)
	for s.Scan() {
		var observation model.Observation
		if err := json.Unmarshal(s.Bytes(), &observation); err != nil {
			return time.Time{}, false, err
		}
		if !exists || observation.ObservedAt.Before(oldest) {
			oldest = observation.ObservedAt
			exists = true
		}
	}
	if err := s.Err(); err != nil {
		return time.Time{}, false, err
	}
	return oldest, exists, nil
}

func compact(path string, cutoff time.Time) (result CompactionResult, err error) {
	source, err := os.Open(path) // #nosec G304 -- the local operator selects the telemetry path.
	if err != nil {
		return result, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return result, err
	}
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".compact-*") // #nosec G304 -- same operator-selected data directory.
	if err != nil {
		return result, err
	}
	temporaryPath := temporary.Name()
	replaced := false
	defer func() {
		if !replaced {
			_ = os.Remove(temporaryPath)
		}
	}()
	if err := temporary.Chmod(info.Mode().Perm()); err != nil {
		_ = temporary.Close()
		return result, err
	}

	writer := bufio.NewWriter(temporary)
	encoder := json.NewEncoder(writer)
	scanner := bufio.NewScanner(source)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		var observation model.Observation
		if err := json.Unmarshal(scanner.Bytes(), &observation); err != nil {
			_ = temporary.Close()
			return result, err
		}
		if observation.Version != model.ObservationVersion || observation.ObservedAt.Before(cutoff) {
			result.Removed++
			continue
		}
		if err := encoder.Encode(observation); err != nil {
			_ = temporary.Close()
			return result, err
		}
		result.Retained++
	}
	if err := scanner.Err(); err != nil {
		_ = temporary.Close()
		return result, err
	}
	if err := writer.Flush(); err != nil {
		_ = temporary.Close()
		return result, err
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return result, err
	}
	if err := temporary.Close(); err != nil {
		return result, err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return result, err
	}
	replaced = true
	result.Compacted = true
	directoryHandle, err := os.Open(directory) // #nosec G304 -- same operator-selected data directory.
	if err != nil {
		return result, err
	}
	defer directoryHandle.Close()
	if err := directoryHandle.Sync(); err != nil {
		return result, err
	}
	return result, nil
}
