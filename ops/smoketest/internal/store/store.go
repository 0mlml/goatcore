// Package store provides JSONL-backed storage for smoke test results.
package store

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/usareur-af/goatcore/ops/smoketest/internal/probe"
)

// Store reads and writes RunResults to a JSONL file (one JSON object per line).
type Store struct {
	path string
	mu   sync.Mutex
}

// New creates a Store backed by the given file path.
func New(path string) *Store {
	return &Store{path: path}
}

// Append writes a single RunResult as a JSON line to the end of the file.
func (s *Store) Append(rr *probe.RunResult) (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := json.Marshal(rr)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, f.Close())
	}()

	_, err = f.Write(data)
	return err
}

// List returns the last limit results, most recent last.
// If fewer than limit results exist, all are returned.
func (s *Store) List(limit int) ([]probe.RunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return []probe.RunResult{}, nil
	}
	if limit <= 0 || limit > len(all) {
		limit = len(all)
	}
	return all[len(all)-limit:], nil
}

// Latest returns the most recent result, or nil if the store is empty.
func (s *Store) Latest() (*probe.RunResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	all, err := s.readAll()
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, nil
	}
	last := all[len(all)-1]
	return &last, nil
}

// readAll reads every line from the file and decodes each as a RunResult.
// Returns an empty slice if the file does not exist or is empty.
func (s *Store) readAll() ([]probe.RunResult, error) {
	f, err := os.Open(s.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var results []probe.RunResult
	scanner := bufio.NewScanner(f)
	// Allow up to 1MB per line for large result sets.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rr probe.RunResult
		if err := json.Unmarshal(line, &rr); err != nil {
			// Skip malformed lines rather than failing the whole read.
			continue
		}
		results = append(results, rr)
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		return results, err
	}
	return results, nil
}
