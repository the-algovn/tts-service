package backend

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// VieNeu talks to the self-hosted VieNeu-TTS pod. CPU inference is slower than
// a cloud round trip, so the timeout is generous.
type VieNeu struct {
	base string
	hc   *http.Client
}

func NewVieNeu(baseURL string) *VieNeu {
	return &VieNeu{
		base: strings.TrimSuffix(baseURL, "/"),
		hc:   &http.Client{Timeout: 120 * time.Second},
	}
}

func (v *VieNeu) Synthesize(ctx context.Context, text, voiceName string, rate float64) ([]byte, string, error) {
	body, _ := json.Marshal(map[string]any{
		"text": text, "voice": voiceName, "speed": rate,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, v.base+"/synthesize", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := v.hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, "", fmt.Errorf("vieneu %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	data, err := io.ReadAll(resp.Body)
	return data, "wav", err
}
