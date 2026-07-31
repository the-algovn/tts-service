package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/the-algovn/tts-service/internal/pricing"
)

// GoogleSource reads the live vi-VN voice list from Google. It is fetched
// rather than hardcoded on purpose: the hand-written list this service
// replaces had gone stale at 6 voices while Google served 40.
type GoogleSource struct {
	APIKey  string
	BaseURL string // defaults to Google when empty; tests point it at httptest
	TTL     time.Duration

	mu      sync.Mutex
	cached  []Voice
	fetched time.Time
}

func (g *GoogleSource) base() string {
	if g.BaseURL != "" {
		return g.BaseURL
	}
	return "https://texttospeech.googleapis.com"
}

func (g *GoogleSource) Voices(ctx context.Context) ([]Voice, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.cached != nil && time.Since(g.fetched) < g.TTL {
		return g.cached, nil
	}

	url := g.base() + "/v1/voices?languageCode=vi-VN&key=" + g.APIKey
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google voices %d", resp.StatusCode)
	}

	var out struct {
		Voices []struct {
			LanguageCodes []string `json:"languageCodes"`
			Name          string   `json:"name"`
			SSMLGender    string   `json:"ssmlGender"`
		} `json:"voices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}

	voices := make([]Voice, 0, len(out.Voices))
	for _, v := range out.Voices {
		// The endpoint is filtered by languageCode, but multilingual voices come
		// back carrying several codes; keep only genuinely Vietnamese ones.
		var vi bool
		for _, c := range v.LanguageCodes {
			if c == "vi-VN" {
				vi = true
			}
		}
		if !vi {
			continue
		}
		tier := pricing.TierOf(v.Name)
		voices = append(voices, Voice{
			ID:            ProviderGoogle + ":" + v.Name,
			Label:         v.Name,
			Provider:      ProviderGoogle,
			Tier:          tier,
			Gender:        v.SSMLGender,
			FreeTierChars: pricing.FreeTierChars(ProviderGoogle, v.Name),
		})
	}

	g.cached, g.fetched = voices, time.Now()
	return voices, nil
}
