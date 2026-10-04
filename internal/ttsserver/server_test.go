package ttsserver_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
	"github.com/the-algovn/tts-service/internal/backend"
	"github.com/the-algovn/tts-service/internal/cache"
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
		Backends: map[string]backend.Backend{"voxcpm": b, "fake": backend.Fake{}},
		Cache:    cache.NewMemory(),
	})
}

func TestSynthesizeRejectsEmptyText(t *testing.T) {
	_, err := newServer(backend.Fake{}).Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text: "", VoiceId: "voxcpm:v_000000000000",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSynthesizeRejectsUnknownProvider(t *testing.T) {
	_, err := newServer(backend.Fake{}).Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text: "xin chào", VoiceId: "nope:some-voice",
	})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}

func TestSynthesizeReportsProviderAtZeroCost(t *testing.T) {
	resp, err := newServer(backend.Fake{}).Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text: "0123456789", VoiceId: "voxcpm:v_000000000000", Format: ttsv1.AudioFormat_AUDIO_FORMAT_WAV,
	})
	require.NoError(t, err)
	require.Equal(t, "voxcpm", resp.GetProvider())
	require.Zero(t, resp.GetCostUsd())
	require.False(t, resp.GetCacheHit())
}

func TestSynthesizeRejectsBareVoiceID(t *testing.T) {
	for _, id := range []string{"vi-VN-Wavenet-B", "", "voxcpm:", ":v_000000000000"} {
		_, err := newServer(backend.Fake{}).Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
			Text: "xin chào", VoiceId: id,
		})
		require.Equal(t, codes.InvalidArgument, status.Code(err), id)
	}
}

// The second identical request must not reach the backend, and must be free.
func TestSynthesizeSecondCallHitsCache(t *testing.T) {
	c := &counting{inner: backend.Fake{}}
	s := newServer(c)
	req := &ttsv1.SynthesizeRequest{
		Text: "xin chào", VoiceId: "voxcpm:v_000000000000", Format: ttsv1.AudioFormat_AUDIO_FORMAT_WAV,
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
// produce real audio.
func TestSynthesizeSurvivesCacheReadFailure(t *testing.T) {
	s := ttsserver.New(ttsserver.Deps{
		Logger:   slog.Default(),
		Backends: map[string]backend.Backend{"voxcpm": backend.Fake{}, "fake": backend.Fake{}},
		Cache:    failingGetStore{},
	})

	resp, err := s.Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text: "xin chào", VoiceId: "voxcpm:v_000000000000", Format: ttsv1.AudioFormat_AUDIO_FORMAT_WAV,
	})

	require.NoError(t, err)
	require.False(t, resp.GetCacheHit())
	require.NotEmpty(t, resp.GetAudio())
}

// A cache write failure must not corrupt the reply that is about to be
// returned to the caller.
func TestSynthesizeSurvivesCacheWriteFailure(t *testing.T) {
	s := ttsserver.New(ttsserver.Deps{
		Logger:   slog.Default(),
		Backends: map[string]backend.Backend{"voxcpm": backend.Fake{}, "fake": backend.Fake{}},
		Cache:    newFailingPutStore(),
	})

	resp, err := s.Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text: "xin chào", VoiceId: "voxcpm:v_000000000000", Format: ttsv1.AudioFormat_AUDIO_FORMAT_WAV,
	})

	require.NoError(t, err)
	require.False(t, resp.GetCacheHit())
	require.NotEmpty(t, resp.GetAudio())
}

func TestListVoicesWithoutRegistryIsEmpty(t *testing.T) {
	s := ttsserver.New(ttsserver.Deps{Logger: slog.Default()})
	resp, err := s.ListVoices(context.Background(), &ttsv1.ListVoicesRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.GetVoices())
}

func TestListVoicesListsOnlyRegistryVoices(t *testing.T) {
	reg := voices.NewRegistry(voices.NewMemory(), time.Now)
	v, err := reg.Create(context.Background(), voices.NewVoice{Label: "Lan", Gender: "FEMALE", RefText: "xin chao", Source: "clone"}, []byte("wav"))
	require.NoError(t, err)
	s := ttsserver.New(ttsserver.Deps{
		Logger:   slog.Default(),
		Backends: map[string]backend.Backend{"fake": backend.Fake{}},
		Voices:   reg,
	})

	resp, err := s.ListVoices(context.Background(), &ttsv1.ListVoicesRequest{})

	require.NoError(t, err)
	require.Len(t, resp.GetVoices(), 1)
	got := resp.GetVoices()[0]
	require.Equal(t, "voxcpm:"+v.ID, got.GetId())
	require.Equal(t, "voxcpm", got.GetProvider())
	require.Equal(t, "self-hosted", got.GetTier())
	require.Equal(t, "FEMALE", got.GetGender())
	require.Zero(t, got.GetFreeTierCharsPerMonth())
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
		Logger: slog.Default(),
		Voices: voices.NewRegistry(failingListStore{voices.NewMemory()}, time.Now),
	})
	resp, err := s.ListVoices(context.Background(), &ttsv1.ListVoicesRequest{})
	require.NoError(t, err)
	require.Empty(t, resp.GetVoices())
}

type invalidInputBackend struct{}

func (invalidInputBackend) Synthesize(context.Context, string, string, float64) ([]byte, string, error) {
	return nil, "", fmt.Errorf("%w: blank", backend.ErrInvalidInput)
}

func TestSynthesizeMapsInvalidInputToInvalidArgument(t *testing.T) {
	_, err := newServer(invalidInputBackend{}).Synthesize(context.Background(), &ttsv1.SynthesizeRequest{
		Text: "x", VoiceId: "voxcpm:v_000000000000"})
	require.Equal(t, codes.InvalidArgument, status.Code(err))
}
