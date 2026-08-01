package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"

	"github.com/proofofmike/stratumstats/internal/model"
)

func Load(path string) ([]model.Observation, error) {
	f, err := os.Open(path)
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
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	e := json.NewEncoder(f)
	for _, o := range observations {
		if err := e.Encode(o); err != nil {
			return err
		}
	}
	return f.Sync()
}
