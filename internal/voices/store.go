// Package voices is the registry of runtime-created self-hosted voices: one
// reference clip and one metadata record per voice, in an object store.
package voices

import (
	"context"
	"sort"
	"strings"
	"sync"
)

// Store is the blob store the registry persists to. Get reports a missing key
// as ok=false, not an error; Delete of a missing key is not an error.
type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Put(ctx context.Context, key string, data []byte) error
	Delete(ctx context.Context, key string) error
	List(ctx context.Context, prefix string) ([]string, error)
}

// Memory is an in-process Store for tests and keyless dev.
type Memory struct {
	mu sync.Mutex
	m  map[string][]byte
}

// NewMemory returns an empty in-process Store.
func NewMemory() *Memory { return &Memory{m: map[string][]byte{}} }

// Get returns a copy of the value at key; ok is false when absent.
func (s *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[key]
	return append([]byte(nil), v...), ok, nil
}

// Put stores a copy of data at key.
func (s *Memory) Put(_ context.Context, key string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = append([]byte(nil), data...)
	return nil
}

// Delete removes key; a missing key is not an error.
func (s *Memory) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
	return nil
}

// List returns the sorted keys that start with prefix.
func (s *Memory) List(_ context.Context, prefix string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for k := range s.m {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out, nil
}
