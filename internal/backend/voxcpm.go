package backend

import (
	"bytes"
	"container/list"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/sync/errgroup"

	"github.com/the-algovn/tts-service/internal/audio"
	"github.com/the-algovn/tts-service/internal/chunk"
	"github.com/the-algovn/tts-service/internal/voices"
)

const (
	chunkTargetMin = 80
	chunkTargetMax = 250
	chunkCap       = 400
	chunkGap       = 150 * time.Millisecond
	voiceCacheSize = 16
	retryMin       = 250 * time.Millisecond
	retryMax       = time.Second
)

// ErrInvalidInput marks a request the voxcpm backend rejects because of its
// text or speaking rate, as opposed to a server-side failure.
var ErrInvalidInput = errors.New("invalid input")

// VoiceSource resolves a registry voice id to its record and reference clip.
type VoiceSource interface {
	Get(ctx context.Context, id string) (voices.Voice, []byte, error)
}

// VoxCPMConfig configures the VoxCPM2 model-server client. One render
// attempt may take ChunkTimeout plus CharTimeout per character of its text,
// because a CPU render grows with the length of what it says.
type VoxCPMConfig struct {
	BaseURL      string
	Parallel     int
	ChunkTimeout time.Duration
	CharTimeout  time.Duration
}

// VoxCPM renders registry voices on the self-hosted VoxCPM2 pods. A script
// is split into sentence chunks rendered concurrently, because one CPU
// render of a whole break is slower than the director can wait.
type VoxCPM struct {
	cfg    VoxCPMConfig
	src    VoiceSource
	hc     *http.Client
	voices *voiceCache
}

type cachedVoice struct {
	id    string
	voice voices.Voice
	ref   []byte
}

// voiceCache is a small LRU of registry voices. Ids are immutable and never
// reused, so an entry can never go stale.
type voiceCache struct {
	mu    sync.Mutex
	order *list.List
	items map[string]*list.Element
}

func newVoiceCache() *voiceCache {
	return &voiceCache{order: list.New(), items: map[string]*list.Element{}}
}

func (c *voiceCache) get(id string) (cachedVoice, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[id]
	if !ok {
		return cachedVoice{}, false
	}
	c.order.MoveToFront(el)
	return el.Value.(cachedVoice), true
}

func (c *voiceCache) put(cv cachedVoice) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.items[cv.id]; ok {
		el.Value = cv
		c.order.MoveToFront(el)
		return
	}
	c.items[cv.id] = c.order.PushFront(cv)
	if c.order.Len() > voiceCacheSize {
		last := c.order.Back()
		c.order.Remove(last)
		delete(c.items, last.Value.(cachedVoice).id)
	}
}

func (v *VoxCPM) voice(ctx context.Context, id string) (voices.Voice, []byte, error) {
	if cv, ok := v.voices.get(id); ok {
		return cv.voice, cv.ref, nil
	}
	voice, ref, err := v.src.Get(ctx, id)
	if err != nil {
		return voices.Voice{}, nil, err
	}
	v.voices.put(cachedVoice{id: id, voice: voice, ref: ref})
	return voice, ref, nil
}

func chunkTarget(text string, parallel int) int {
	n := utf8.RuneCountInString(text)
	t := (n+parallel-1)/parallel + 20
	return min(max(t, chunkTargetMin), chunkTargetMax)
}

// NewVoxCPM returns a client for the model server at cfg.BaseURL that looks
// voices up in src.
func NewVoxCPM(cfg VoxCPMConfig, src VoiceSource) *VoxCPM {
	if cfg.Parallel < 1 {
		cfg.Parallel = 1
	}
	// Keep-alive off: each chunk opens a fresh connection so kube-proxy
	// spreads concurrent chunks over the replicas instead of pinning them
	// to whichever pod a pooled connection landed on.
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DisableKeepAlives = true
	return &VoxCPM{cfg: cfg, src: src, hc: &http.Client{Transport: tr}, voices: newVoiceCache()}
}

type voxReq struct {
	Text        string  `json:"text"`
	RefWAVB64   *string `json:"ref_wav_b64"`
	RefText     *string `json:"ref_text"`
	Description *string `json:"description"`
}

// Synthesize renders text in the registry voice voiceName and returns WAV.
// Errors wrap voices.ErrNotFound for an unknown voice and ErrInvalidInput for
// blank text or a rate outside 0.5-2.0; any failed chunk fails the whole call.
func (v *VoxCPM) Synthesize(ctx context.Context, text, voiceName string, rate float64) ([]byte, string, error) {
	if rate < 0.5 || rate > 2.0 {
		return nil, "", fmt.Errorf("%w: rate %.2f outside 0.5-2.0", ErrInvalidInput, rate)
	}
	if strings.TrimSpace(text) == "" {
		return nil, "", fmt.Errorf("%w: text is blank", ErrInvalidInput)
	}
	voice, ref, err := v.voice(ctx, voiceName)
	if err != nil {
		return nil, "", fmt.Errorf("voice %s: %w", voiceName, err)
	}
	b64 := base64.StdEncoding.EncodeToString(ref)
	refText := voice.RefText
	chunks := chunk.Split(text, chunkTarget(text, v.cfg.Parallel), chunkCap)
	if len(chunks) == 0 {
		return nil, "", fmt.Errorf("%w: nothing to say", ErrInvalidInput)
	}
	parts, err := v.renderAll(ctx, len(chunks), func(i int) voxReq {
		return voxReq{Text: chunks[i], RefWAVB64: &b64, RefText: &refText}
	})
	if err != nil {
		return nil, "", err
	}
	wav, err := audio.ConcatWAV(ctx, parts, chunkGap)
	if err != nil {
		return nil, "", err
	}
	wav, err = audio.Tempo(ctx, wav, rate)
	if err != nil {
		return nil, "", err
	}
	return wav, "wav", nil
}

// Design renders takes independent candidate WAVs of sampleText in a voice
// described by description. Nothing is stored.
func (v *VoxCPM) Design(ctx context.Context, description, sampleText string, takes int) ([][]byte, error) {
	return v.renderAll(ctx, takes, func(int) voxReq {
		return voxReq{Text: sampleText, Description: &description}
	})
}

func (v *VoxCPM) renderAll(ctx context.Context, n int, req func(i int) voxReq) ([][]byte, error) {
	parts := make([][]byte, n)
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(v.cfg.Parallel)
	for i := 0; i < n; i++ {
		g.Go(func() error {
			wav, err := v.render(gctx, req(i))
			if err != nil {
				return fmt.Errorf("chunk %d/%d: %w", i+1, n, err)
			}
			parts[i] = wav
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return parts, nil
}

// render posts one request, retrying while a replica is busy (HTTP 503) or
// unreachable, until ctx is done. Each attempt gets its own timeout.
func (v *VoxCPM) render(ctx context.Context, r voxReq) ([]byte, error) {
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	timeout := v.cfg.ChunkTimeout + time.Duration(utf8.RuneCountInString(r.Text))*v.cfg.CharTimeout
	for {
		wav, retry, err := v.attempt(ctx, body, timeout)
		if err == nil || !retry || ctx.Err() != nil {
			return wav, err
		}
		wait := retryMin + rand.N(retryMax-retryMin)
		select {
		case <-ctx.Done():
			return nil, err
		case <-time.After(wait):
		}
	}
}

func (v *VoxCPM) attempt(parent context.Context, body []byte, timeout time.Duration) (wav []byte, retry bool, err error) {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(v.cfg.BaseURL, "/")+"/synthesize", bytes.NewReader(body))
	if err != nil {
		return nil, false, err
	}
	hr.Header.Set("Content-Type", "application/json")
	resp, err := v.hc.Do(hr)
	if err != nil {
		return nil, ctx.Err() == nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, resp.StatusCode == http.StatusServiceUnavailable,
			fmt.Errorf("voxcpm %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	wav, err = io.ReadAll(resp.Body)
	return wav, false, err
}
