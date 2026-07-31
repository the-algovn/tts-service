package backend_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/backend"
)

func TestGoogleSynthesizeSendsVietnameseAndDecodesAudio(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"audioContent": base64.StdEncoding.EncodeToString([]byte("ID3-audio")),
		})
	}))
	defer srv.Close()

	g := backend.NewGoogle("k")
	g.BaseURL = srv.URL

	data, ext, err := g.Synthesize(context.Background(), "xin chào", "vi-VN-Wavenet-B", 1.1)
	require.NoError(t, err)
	require.Equal(t, []byte("ID3-audio"), data)
	require.Equal(t, "mp3", ext)

	voice := body["voice"].(map[string]any)
	require.Equal(t, "vi-VN", voice["languageCode"])
	require.Equal(t, "vi-VN-Wavenet-B", voice["name"])
	require.InDelta(t, 1.1, body["audioConfig"].(map[string]any)["speakingRate"], 1e-9)
}

// A zero rate is the proto default, not a request for silence-speed audio.
func TestGoogleSynthesizeDefaultsRate(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		_ = json.NewEncoder(w).Encode(map[string]string{"audioContent": ""})
	}))
	defer srv.Close()

	g := backend.NewGoogle("k")
	g.BaseURL = srv.URL

	_, _, err := g.Synthesize(context.Background(), "xin chào", "vi-VN-Wavenet-B", 0)
	require.NoError(t, err)
	require.InDelta(t, 1.0, body["audioConfig"].(map[string]any)["speakingRate"], 1e-9)
}

func TestGoogleSynthesizeSurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"message":"key invalid"}}`))
	}))
	defer srv.Close()

	g := backend.NewGoogle("k")
	g.BaseURL = srv.URL

	_, _, err := g.Synthesize(context.Background(), "xin chào", "vi-VN-Wavenet-B", 1)
	require.ErrorContains(t, err, "key invalid")
}
