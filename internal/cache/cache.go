// Package cache stores synthesized audio by content. The key covers every input
// that changes the bytes, so a hit is always safe to return; there is no index
// and no database, only a deterministic object name.
package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"

	"golang.org/x/text/unicode/norm"
)

type Store interface {
	Get(ctx context.Context, key string) ([]byte, bool, error)
	Put(ctx context.Context, key string, data []byte) error
}

// Key is the object name for one synthesis. Text is NFC-normalized so that the
// same Vietnamese words written with precomposed vs combining diacritics share
// one entry -- a pure Unicode concern, unrelated to reading numbers aloud.
func Key(provider, voiceName string, rate float64, ext, text string) string {
	canonical := fmt.Sprintf("%s\x00%s\x00%.3f\x00%s\x00%s",
		provider, voiceName, rate, ext, norm.NFC.String(text))
	sum := sha256.Sum256([]byte(canonical))
	return fmt.Sprintf("tts/%s/%s.%s", provider, hex.EncodeToString(sum[:]), ext)
}

// Memory is a test store.
type Memory struct {
	mu sync.Mutex
	m  map[string][]byte
}

func NewMemory() *Memory { return &Memory{m: map[string][]byte{}} }

func (c *Memory) Get(_ context.Context, key string) ([]byte, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	return v, ok, nil
}

func (c *Memory) Put(_ context.Context, key string, data []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = data
	return nil
}
