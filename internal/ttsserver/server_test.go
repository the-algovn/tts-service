package ttsserver_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
	"github.com/the-algovn/tts-service/internal/backend"
	"github.com/the-algovn/tts-service/internal/cache"
	"github.com/the-algovn/tts-service/internal/ttsserver"
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
