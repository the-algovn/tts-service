package backend_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/the-algovn/tts-service/internal/audio"
	"github.com/the-algovn/tts-service/internal/backend"
	"github.com/the-algovn/tts-service/internal/voices"
)

type fakeVoices map[string]voices.Voice

func (f fakeVoices) Get(_ context.Context, id string) (voices.Voice, []byte, error) {
	v, ok := f[id]
	if !ok {
		return voices.Voice{}, nil, voices.ErrNotFound
	}
	return v, []byte("REFWAV"), nil
}

func toneWAV(t *testing.T) []byte {
	t.Helper()
	out, err := exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "sine=frequency=300:duration=1:sample_rate=48000", "-f", "wav", "-").Output()
	require.NoError(t, err)
	return out
}

type modelReq struct {
	Text        string  `json:"text"`
	RefWAVB64   *string `json:"ref_wav_b64"`
	RefText     *string `json:"ref_text"`
	Description *string `json:"description"`
}

func longScript() string {
	return strings.Repeat("Day la mot cau noi kha dai cua nguoi dan chuong trinh radio. ", 12)
}

func TestVoxCPMChunksInParallelAndConcatsInOrder(t *testing.T) {
	wav := toneWAV(t)
	var inFlight, peak int32
	var mu sync.Mutex
	var texts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&inFlight, 1)
		for {
			p := atomic.LoadInt32(&peak)
			if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
				break
			}
		}
		defer atomic.AddInt32(&inFlight, -1)
		var req modelReq
		raw, _ := io.ReadAll(r.Body)
		require.NoError(t, json.Unmarshal(raw, &req))
		require.Equal(t, base64.StdEncoding.EncodeToString([]byte("REFWAV")), *req.RefWAVB64)
		require.Equal(t, "xin chao", *req.RefText)
		require.Nil(t, req.Description)
		mu.Lock()
		texts = append(texts, req.Text)
		mu.Unlock()
		time.Sleep(50 * time.Millisecond)
		_, _ = w.Write(wav)
	}))
	defer srv.Close()

	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 2, ChunkTimeout: 5 * time.Second},
		fakeVoices{"v_aaaaaaaaaaaa": {ID: "v_aaaaaaaaaaaa", RefText: "xin chao"}})
	out, ext, err := v.Synthesize(context.Background(), longScript(), "v_aaaaaaaaaaaa", 1.0)
	require.NoError(t, err)
	require.Equal(t, "wav", ext)
	require.LessOrEqual(t, peak, int32(2))
	require.Greater(t, len(texts), 2)
	d, err := audio.Probe(context.Background(), out)
	require.NoError(t, err)
	require.InDelta(t, float64(len(texts))+0.15*float64(len(texts)-1), d, 0.1)
}

func TestVoxCPMOneChunkFailureFailsWhole(t *testing.T) {
	wav := toneWAV(t)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 2 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("oom"))
			return
		}
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 1, ChunkTimeout: 5 * time.Second},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	out, _, err := v.Synthesize(context.Background(), longScript(), "v_aaaaaaaaaaaa", 1.0)
	require.ErrorContains(t, err, "oom")
	require.Nil(t, out)
}

func TestVoxCPMUnknownVoiceIsNotFound(t *testing.T) {
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: "http://unused", Parallel: 1, ChunkTimeout: time.Second}, fakeVoices{})
	_, _, err := v.Synthesize(context.Background(), "xin chao.", "v_missing00000", 1.0)
	require.True(t, errors.Is(err, voices.ErrNotFound))
}

func TestVoxCPMDesignSendsDescriptionWithoutRef(t *testing.T) {
	wav := toneWAV(t)
	var got []modelReq
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req modelReq
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &req)
		mu.Lock()
		got = append(got, req)
		mu.Unlock()
		_, _ = w.Write(wav)
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 3, ChunkTimeout: time.Second}, fakeVoices{})
	takes, err := v.Design(context.Background(), "giong nu tre", "Chao ban.", 3)
	require.NoError(t, err)
	require.Len(t, takes, 3)
	require.Len(t, got, 3)
	for _, r := range got {
		require.Equal(t, "giong nu tre", *r.Description)
		require.Equal(t, "Chao ban.", r.Text)
		require.Nil(t, r.RefWAVB64)
	}
}

func TestVoxCPMChunkTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
	}))
	defer srv.Close()
	v := backend.NewVoxCPM(backend.VoxCPMConfig{BaseURL: srv.URL, Parallel: 1, ChunkTimeout: 50 * time.Millisecond},
		fakeVoices{"v_aaaaaaaaaaaa": {RefText: "x"}})
	_, _, err := v.Synthesize(context.Background(), "xin chao.", "v_aaaaaaaaaaaa", 1.0)
	require.Error(t, err)
}
