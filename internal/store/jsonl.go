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

	"github.com/M45Core/StratumStats/internal/model"
)

func Load(path string) ([]model.Observation, error) {
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

func (a *Appender) Append(observations []model.Observation) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return Append(a.Path, observations)
}
