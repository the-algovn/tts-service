package backend

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Google struct {
	key     string
	BaseURL string
	hc      *http.Client
}

func NewGoogle(apiKey string) *Google {
	return &Google{
		key:     apiKey,
		BaseURL: "https://texttospeech.googleapis.com",
		hc:      &http.Client{Timeout: 25 * time.Second},
	}
}

func (g *Google) Synthesize(ctx context.Context, text, voiceName string, rate float64) ([]byte, string, error) {
	if rate == 0 {
		rate = 1.0
	}
	body, _ := json.Marshal(map[string]any{
		"input":       map[string]string{"text": text},
		"voice":       map[string]string{"languageCode": "vi-VN", "name": voiceName},
		"audioConfig": map[string]any{"audioEncoding": "MP3", "speakingRate": rate},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.BaseURL+"/v1/text:synthesize", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	// Key in a header, not the query string: transport errors are *url.Error,
	// which prints the URL, and ttsserver logs that error on failure.
	req.Header.Set("x-goog-api-key", g.key)
	resp, err := g.hc.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error struct{ Message string } }
		_ = json.NewDecoder(resp.Body).Decode(&e)
		return nil, "", fmt.Errorf("google tts %d: %s", resp.StatusCode, e.Error.Message)
	}
	var out struct {
		AudioContent string `json:"audioContent"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, "", err
	}
	data, err := base64.StdEncoding.DecodeString(out.AudioContent)
	return data, "mp3", err
}
