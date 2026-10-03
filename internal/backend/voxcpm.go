package backend

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/the-algovn/tts-service/internal/audio"
	"github.com/the-algovn/tts-service/internal/chunk"
	"github.com/the-algovn/tts-service/internal/voices"
)

const (
	chunkTarget = 250
	chunkCap    = 400
	chunkGap    = 150 * time.Millisecond
)

// VoiceSource resolves a registry voice id to its record and reference clip.
type VoiceSource interface {
	Get(ctx context.Context, id string) (voices.Voice, []byte, error)
}

// VoxCPMConfig configures the VoxCPM2 model-server client.
type VoxCPMConfig struct {
	BaseURL      string
	Parallel     int
	ChunkTimeout time.Duration
}

// VoxCPM renders registry voices on the self-hosted VoxCPM2 pods. A script
// is split into sentence chunks rendered concurrently, because one CPU
// render of a whole break is slower than the director can wait.
type VoxCPM struct {
	cfg VoxCPMConfig
	src VoiceSource
	hc  *http.Client
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
	return &VoxCPM{cfg: cfg, src: src, hc: &http.Client{Transport: tr}}
}

type voxReq struct {
	Text        string  `json:"text"`
	RefWAVB64   *string `json:"ref_wav_b64"`
	RefText     *string `json:"ref_text"`
	Description *string `json:"description"`
}

// Synthesize renders text in the registry voice voiceName and returns WAV.
// Errors wrap voices.ErrNotFound for an unknown voice; any failed chunk fails
// the whole call.
func (v *VoxCPM) Synthesize(ctx context.Context, text, voiceName string, rate float64) ([]byte, string, error) {
	voice, ref, err := v.src.Get(ctx, voiceName)
	if err != nil {
		return nil, "", fmt.Errorf("voice %s: %w", voiceName, err)
	}
	b64 := base64.StdEncoding.EncodeToString(ref)
	refText := voice.RefText
	chunks := chunk.Split(text, chunkTarget, chunkCap)
	if len(chunks) == 0 {
		return nil, "", fmt.Errorf("nothing to say")
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

func (v *VoxCPM) render(ctx context.Context, r voxReq) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, v.cfg.ChunkTimeout)
	defer cancel()
	body, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	hr, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(v.cfg.BaseURL, "/")+"/synthesize", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	hr.Header.Set("Content-Type", "application/json")
	resp, err := v.hc.Do(hr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("voxcpm %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return io.ReadAll(resp.Body)
}
