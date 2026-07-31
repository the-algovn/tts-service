package backend_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/backend"
)

func TestVieNeuPostsTextAndReturnsWAV(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/synthesize", r.URL.Path)
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write([]byte("RIFFfake"))
	}))
	defer srv.Close()

	v := backend.NewVieNeu(srv.URL)
	data, ext, err := v.Synthesize(context.Background(), "xin chào", "v3-turbo-female", 1.0)

	require.NoError(t, err)
	require.Equal(t, []byte("RIFFfake"), data)
	require.Equal(t, "wav", ext)
	require.Equal(t, "xin chào", got["text"])
	require.Equal(t, "v3-turbo-female", got["voice"])
}

func TestVieNeuSurfacesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("model not loaded"))
	}))
	defer srv.Close()

	_, _, err := backend.NewVieNeu(srv.URL).Synthesize(context.Background(), "xin chào", "v3-turbo-female", 1.0)
	require.ErrorContains(t, err, "model not loaded")
}
