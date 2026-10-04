// Package ttsserver implements algovn.tts.v1.TTSService. It owns request
// validation and assembles cache, backend and the voice registry; each of those
// concerns lives in its own package.
package ttsserver

import (
	"context"
	"errors"
	"log/slog"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
	"github.com/the-algovn/tts-service/internal/audio"
	"github.com/the-algovn/tts-service/internal/backend"
	"github.com/the-algovn/tts-service/internal/cache"
	"github.com/the-algovn/tts-service/internal/voices"
)

// maxTextChars bounds one utterance. The bound is derived from the default
// 4MB gRPC message limit, not an arbitrary sizing choice: neither this
// server nor its callers raise MaxCallRecvMsgSize/MaxCallSendMsgSize, and
// text transcodes to roughly 1MB of MP3 per minute of speech. 5000 Vietnamese characters (~5 minutes) produced ~4.8MB -- a
// request this service would have declared legal but could not deliver,
// after doing all the synthesis work. 2000 characters (~2 minutes, ~1.9MB)
// stays safely under the limit.
const maxTextChars = 2000

type Deps struct {
	Logger   *slog.Logger
	Backends map[string]backend.Backend
	Cache    cache.Store
	// Voices and Designer back the registry RPCs and ListVoices; both nil
	// when VOXCPM_URL is unset.
	Voices   *voices.Registry
	Designer Designer
}

type Server struct {
	ttsv1.UnimplementedTTSServiceServer
	deps Deps
}

func New(deps Deps) *Server { return &Server{deps: deps} }

// extOf maps the requested container onto a file extension. Unspecified means
// mp3: it is what both consumers stored before this service existed.
func extOf(f ttsv1.AudioFormat) string {
	if f == ttsv1.AudioFormat_AUDIO_FORMAT_WAV {
		return "wav"
	}
	return "mp3"
}

func formatOf(ext string) ttsv1.AudioFormat {
	if ext == "wav" {
		return ttsv1.AudioFormat_AUDIO_FORMAT_WAV
	}
	return ttsv1.AudioFormat_AUDIO_FORMAT_MP3
}

func (s *Server) Synthesize(ctx context.Context, req *ttsv1.SynthesizeRequest) (*ttsv1.SynthesizeResponse, error) {
	if req.GetText() == "" {
		return nil, status.Error(codes.InvalidArgument, "text is required")
	}
	chars := utf8.RuneCountInString(req.GetText())
	if chars > maxTextChars {
		return nil, status.Errorf(codes.InvalidArgument, "text exceeds %d characters", maxTextChars)
	}

	provider, name, ok := splitVoiceID(req.GetVoiceId())
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "voice_id %q must be \"provider:name\"", req.GetVoiceId())
	}
	be, ok := s.deps.Backends[provider]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unknown provider %q", provider)
	}

	wantExt := extOf(req.GetFormat())
	rate := req.GetSpeakingRate()
	if rate == 0 {
		rate = 1.0
	}
	key := cache.Key(provider, name, rate, wantExt, req.GetText())

	if s.deps.Cache != nil {
		if data, hit, err := s.deps.Cache.Get(ctx, key); err != nil {
			// A broken cache must not stop speech; synthesize and move on.
			s.deps.Logger.WarnContext(ctx, "cache read failed", "key", key, "err", err)
		} else if hit {
			return &ttsv1.SynthesizeResponse{
				Audio: data, Format: formatOf(wantExt), VoiceId: req.GetVoiceId(),
				Provider: provider, CostUsd: 0, CacheHit: true,
			}, nil
		}
	}

	data, gotExt, err := be.Synthesize(ctx, req.GetText(), name, rate)
	if err != nil {
		s.deps.Logger.ErrorContext(ctx, "synthesis failed",
			"provider", provider, "voice", name, "label", req.GetLabel(), "err", err)
		if errors.Is(err, voices.ErrNotFound) {
			return nil, status.Errorf(codes.InvalidArgument, "unknown voice %q", req.GetVoiceId())
		}
		if errors.Is(err, backend.ErrInvalidInput) {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		return nil, status.Errorf(codes.Unavailable, "synthesis failed: %v", err)
	}

	data, err = audio.Convert(ctx, data, gotExt, wantExt)
	if err != nil {
		s.deps.Logger.ErrorContext(ctx, "transcode failed", "from", gotExt, "to", wantExt, "err", err)
		return nil, status.Errorf(codes.Internal, "transcode failed: %v", err)
	}

	if s.deps.Cache != nil {
		if err := s.deps.Cache.Put(ctx, key, data); err != nil {
			s.deps.Logger.WarnContext(ctx, "cache write failed", "key", key, "err", err)
		}
	}

	s.deps.Logger.InfoContext(ctx, "synthesized",
		"provider", provider, "voice", name, "chars", chars, "label", req.GetLabel())

	return &ttsv1.SynthesizeResponse{
		Audio: data, Format: formatOf(wantExt), VoiceId: req.GetVoiceId(),
		Provider: provider, CostUsd: 0, CacheHit: false,
	}, nil
}

// ListVoices returns every voice in the self-hosted registry. A registry
// failure is logged and yields an empty list rather than an error: a voice
// list is a picker, not a dependency of speech itself.
func (s *Server) ListVoices(ctx context.Context, _ *ttsv1.ListVoicesRequest) (*ttsv1.ListVoicesResponse, error) {
	out := []*ttsv1.Voice{}
	if s.deps.Voices != nil {
		reg, err := s.deps.Voices.List(ctx)
		if err != nil {
			s.deps.Logger.WarnContext(ctx, "voice registry unavailable", "err", err)
		}
		for _, v := range reg {
			out = append(out, toProtoVoice(v))
		}
	}
	return &ttsv1.ListVoicesResponse{Voices: out}, nil
}
