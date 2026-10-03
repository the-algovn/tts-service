package ttsserver_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
	"github.com/the-algovn/tts-service/internal/backend"
	"github.com/the-algovn/tts-service/internal/cache"
	"github.com/the-algovn/tts-service/internal/catalog"
	"github.com/the-algovn/tts-service/internal/ttsserver"
	"github.com/the-algovn/tts-service/internal/voices"
)

// counting wraps a backend to prove the cache prevents a second call.
type counting struct {
	inner backend.Backend
	calls int
}

func (c *counting) Synthesize(ctx context.Context, text, voice string, rate float64) ([]byte, string, error) {
	c.calls++
	return c.inner.Synthesize(ctx, text, voice, rate)
}

// failingGetStore always fails reads; writes succeed. It proves a broken
// cache degrades rather than blocks speech.
type failingGetStore struct{}

func (failingGetStore) Get(_ context.Context, _ string) ([]byte, bool, error) {
	return nil, false, errors.New("boom")
}

func (failingGetStore) Put(_ context.Context, _ string, _ []byte) error { return nil }

// failingPutStore reads normally (via an in-memory store) but always fails
// writes. It proves a cache write failure must not corrupt the response.
type failingPutStore struct{ inner cache.Store }

func newFailingPutStore() *failingPutStore { return &failingPutStore{inner: cache.NewMemory()} }

func (f *failingPutStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return f.inner.Get(ctx, key)
}

func (f *failingPutStore) Put(_ context.Context, _ string, _ []byte) error {
	return errors.New("put boom")
}

func newServer(b backend.Backend) *ttsserver.Server {
	return ttsserver.New(ttsserver.Deps{
		Logger:   slog.Default(),
		Backends: map[string]backend.Backend{"google": b, "fake": backend.Fake{}},
		Cache:    cache.NewMemory(),
	})
}

func TestSynthesizeRejectsEmptyText(t *testing.T) {
	_, err := newServer(backend.Fake{}).Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text: "", VoiceId: "google:vi-VN-Wavenet-B",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSynthesizeRejectsUnknownProvider(t *testing.T) {
	_, err := newServer(backend.Fake{}).Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text: "xin chào", VoiceId: "nope:some-voice",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSynthesizePricesAtListRate(t *testing.T) {
	resp, err := newServer(backend.Fake{}).Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		// 10 runes, wavenet tier at $4/1M chars.
		Text: "0123456789", VoiceId: "google:vi-VN-Wavenet-B", Format: ttsv1.AudioFormat_AUDIO_FORMAT_WAV,
	})
	require.NoError(t, err)
	require.InDelta(t, 4.0/1e6*10, resp.GetCostUsd(), 1e-12)
	require.Equal(t, "google", resp.GetProvider())
	require.False(t, resp.GetCacheHit())
}

// A keyless deploy substitutes backend.Fake{} under the "google" key
// (cmd/tts/main.go, GoogleIsFake). The response must report the backend that
// actually served the request -- "fake", at zero cost -- not "google" at the
// Google list price for a second of silence.
func TestSynthesizeReportsFakeProviderWhenGoogleIsSubstituted(t *testing.T) {
	s := ttsserver.New(ttsserver.Deps{
		Logger:       slog.Default(),
		Backends:     map[string]backend.Backend{"google": backend.Fake{}, "fake": backend.Fake{}},
		Cache:        cache.NewMemory(),
		GoogleIsFake: true,
	})

	resp, err := s.Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text: "0123456789", VoiceId: "google:vi-VN-Wavenet-B", Format: ttsv1.AudioFormat_AUDIO_FORMAT_WAV,
	})

	require.NoError(t, err)
	require.Equal(t, "fake", resp.GetProvider())
	require.Zero(t, resp.GetCostUsd())
}

// A bare id must keep working: ids persisted before this service existed have
// no provider prefix.
func TestSynthesizeAcceptsBareVoiceID(t *testing.T) {
	resp, err := newServer(backend.Fake{}).Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text: "xin chào", VoiceId: "vi-VN-Wavenet-B", Format: ttsv1.AudioFormat_AUDIO_FORMAT_WAV,
	})
	require.NoError(t, err)
	require.Equal(t, "google", resp.GetProvider())
}

// The second identical request must not reach the backend, and must be free.
func TestSynthesizeSecondCallHitsCache(t *testing.T) {
	c := &counting{inner: backend.Fake{}}
	s := newServer(c)
	req := &ttsv1.SynthesizeRequest{
		Text: "xin chào", VoiceId: "google:vi-VN-Wavenet-B", Format: ttsv1.AudioFormat_AUDIO_FORMAT_WAV,
	}

	first, err := s.Synthesize(context.Background(), req)
	require.NoError(t, err)
	second, err := s.Synthesize(context.Background(), req)
	require.NoError(t, err)

	require.Equal(t, 1, c.calls, "cached synthesis must not call the backend again")
	require.True(t, second.GetCacheHit())
	require.Zero(t, second.GetCostUsd(), "a cache hit costs nothing")
	require.Equal(t, first.GetAudio(), second.GetAudio())
}

// A broken cache is a degraded cache, not an outage: a Get error must still
// produce real, correctly priced audio.
func TestSynthesizeSurvivesCacheReadFailure(t *testing.T) {
	s := ttsserver.New(ttsserver.Deps{
		Logger:   slog.Default(),
		Backends: map[string]backend.Backend{"google": backend.Fake{}, "fake": backend.Fake{}},
		Cache:    failingGetStore{},
	})

	resp, err := s.Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		// 8 runes, wavenet tier at $4/1M chars.
		Text: "xin chào", VoiceId: "google:vi-VN-Wavenet-B", Format: ttsv1.AudioFormat_AUDIO_FORMAT_WAV,
	})

	require.NoError(t, err)
	require.False(t, resp.GetCacheHit())
	require.NotEmpty(t, resp.GetAudio())
	require.InDelta(t, 4.0/1e6*8, resp.GetCostUsd(), 1e-12)
}

// A cache write failure must not corrupt the reply that is about to be
// returned to the caller.
func TestSynthesizeSurvivesCacheWriteFailure(t *testing.T) {
	s := ttsserver.New(ttsserver.Deps{
		Logger:   slog.Default(),
		Backends: map[string]backend.Backend{"google": backend.Fake{}, "fake": backend.Fake{}},
		Cache:    newFailingPutStore(),
	})

	resp, err := s.Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text: "xin chào", VoiceId: "google:vi-VN-Wavenet-B", Format: ttsv1.AudioFormat_AUDIO_FORMAT_WAV,
	})

	require.NoError(t, err)
	require.False(t, resp.GetCacheHit())
	require.NotEmpty(t, resp.GetAudio())
	require.InDelta(t, 4.0/1e6*8, resp.GetCostUsd(), 1e-12)
}

// A nil Google source (no API key configured) must not panic, and must
// return whatever VieNeu supplies.
func TestListVoicesNilGoogleReturnsVieNeuOnly(t *testing.T) {
	vieneu := []catalog.Voice{
		{ID: "vieneu:test-voice", Label: "Test Voice", Provider: "vieneu", Tier: "standard", Gender: "FEMALE"},
	}
	s := ttsserver.New(ttsserver.Deps{Logger: slog.Default(), VieNeuVoices: vieneu})

	resp, err := s.ListVoices(context.Background(), &ttsv1.ListVoicesRequest{})

	require.NoError(t, err)
	require.Len(t, resp.GetVoices(), 1)
	require.Equal(t, "vieneu:test-voice", resp.GetVoices()[0].GetId())
}

// A voice list is a picker, not a dependency of speech: a Google catalog
// fetch failure must not fail the RPC, only omit Google's voices.
func TestListVoicesToleratesGoogleFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := ttsserver.New(ttsserver.Deps{
		Logger: slog.Default(),
		Google: &catalog.GoogleSource{APIKey: "k", BaseURL: srv.URL, TTL: time.Minute},
	})

	resp, err := s.ListVoices(context.Background(), &ttsv1.ListVoicesRequest{})

	require.NoError(t, err)
	require.Empty(t, resp.GetVoices())
}

const listVoicesFixtureJSON = `{"voices":[
  {"languageCodes":["vi-VN"],"name":"vi-VN-Wavenet-B","ssmlGender":"MALE"}
]}`

// The merged list must carry every field through for both a fetched Google
// voice and a directly injected VieNeu voice.
func TestListVoicesMergesGoogleAndVieNeu(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(listVoicesFixtureJSON))
	}))
	defer srv.Close()

	vieneu := []catalog.Voice{
		{ID: "vieneu:custom-1", Label: "Custom One", Provider: "vieneu", Tier: "standard", Gender: "FEMALE", FreeTierChars: 0},
	}
	s := ttsserver.New(ttsserver.Deps{
		Logger:       slog.Default(),
		Google:       &catalog.GoogleSource{APIKey: "k", BaseURL: srv.URL, TTL: time.Minute},
		VieNeuVoices: vieneu,
	})

	resp, err := s.ListVoices(context.Background(), &ttsv1.ListVoicesRequest{})
	require.NoError(t, err)
	require.Len(t, resp.GetVoices(), 2)

	g := resp.GetVoices()[0]
	require.Equal(t, "google:vi-VN-Wavenet-B", g.GetId())
	require.Equal(t, "vi-VN-Wavenet-B", g.GetLabel())
	require.Equal(t, "google", g.GetProvider())
	require.Equal(t, "wavenet", g.GetTier())
	require.Equal(t, "MALE", g.GetGender())
	require.EqualValues(t, 4_000_000, g.GetFreeTierCharsPerMonth())

	v := resp.GetVoices()[1]
	require.Equal(t, "vieneu:custom-1", v.GetId())
	require.Equal(t, "Custom One", v.GetLabel())
	require.Equal(t, "vieneu", v.GetProvider())
	require.Equal(t, "standard", v.GetTier())
	require.Equal(t, "FEMALE", v.GetGender())
	require.EqualValues(t, 0, v.GetFreeTierCharsPerMonth())
}

type notFoundBackend struct{}

func (notFoundBackend) Synthesize(_ context.Context, _, voice string, _ float64) ([]byte, string, error) {
	return nil, "", fmt.Errorf("voice %s: %w", voice, voices.ErrNotFound)
}

func TestSynthesizeUnknownVoiceIsInvalidArgument(t *testing.T) {
	s := ttsserver.New(ttsserver.Deps{
		Logger:   slog.Default(),
		Backends: map[string]backend.Backend{"voxcpm": notFoundBackend{}},
	})
	_, err := s.Synthesize(context.Background(), &ttsv1.SynthesizeRequest{Text: "xin chao", VoiceId: "voxcpm:v_000000000000"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

type failingListStore struct{ *voices.Memory }

func (failingListStore) List(_ context.Context, _ string) ([]string, error) {
	return nil, errors.New("list boom")
}

func TestListVoicesSurvivesRegistryFailure(t *testing.T) {
	s := ttsserver.New(ttsserver.Deps{
		Logger:       slog.Default(),
		VieNeuVoices: []catalog.Voice{{ID: "vieneu:custom-1", Label: "Custom One", Provider: "vieneu"}},
		Voices:       voices.NewRegistry(failingListStore{voices.NewMemory()}, time.Now),
	})
	resp, err := s.ListVoices(context.Background(), &ttsv1.ListVoicesRequest{})
	require.NoError(t, err)
	require.Len(t, resp.GetVoices(), 1)
	require.Equal(t, "vieneu:custom-1", resp.GetVoices()[0].GetId())
}
