// Package ttsserver implements algovn.tts.v1.TTSService. It owns request
// validation and assembles catalog, cache, backend and pricing; each of those
// concerns lives in its own package.
package ttsserver

import (
	"context"
	"log/slog"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
	"github.com/the-algovn/tts-service/internal/audio"
	"github.com/the-algovn/tts-service/internal/backend"
	"github.com/the-algovn/tts-service/internal/cache"
	"github.com/the-algovn/tts-service/internal/catalog"
	"github.com/the-algovn/tts-service/internal/pricing"
)

// maxTextChars bounds one utterance. A DJ break is a few hundred characters and
// a race line under a hundred; anything past this is a caller bug, and letting
// it through would bill for it.
const maxTextChars = 5000

type Deps struct {
	Logger       *slog.Logger
	Backends     map[string]backend.Backend
	Cache        cache.Store
	Google       *catalog.GoogleSource
	VieNeuVoices []catalog.Voice
	// GoogleIsFake is true when main.go substituted backend.Fake{} under the
	// "google" key because GOOGLE_TTS_API_KEY is absent (keyless dev). It lets
	// Synthesize report the backend that actually served the request instead
	// of the one the caller asked for.
	GoogleIsFake bool
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

	provider, name := catalog.Resolve(req.GetVoiceId())
	be, ok := s.deps.Backends[provider]
	if !ok {
		return nil, status.Errorf(codes.InvalidArgument, "unknown provider %q", provider)
	}

	// A keyless box substitutes backend.Fake{} under the "google" key (see
	// GoogleIsFake); report the backend that actually served the request, or
	// a keyless deploy would charge the Google list price for silence.
	effectiveProvider := provider
	if provider == "google" && s.deps.GoogleIsFake {
		effectiveProvider = "fake"
	}

	wantExt := extOf(req.GetFormat())
	rate := req.GetSpeakingRate()
	if rate == 0 {
		rate = 1.0
	}
	key := cache.Key(effectiveProvider, name, rate, wantExt, req.GetText())

	if s.deps.Cache != nil {
		if data, hit, err := s.deps.Cache.Get(ctx, key); err != nil {
			// A broken cache must not stop speech; synthesize and move on.
			s.deps.Logger.WarnContext(ctx, "cache read failed", "key", key, "err", err)
		} else if hit {
			return &ttsv1.SynthesizeResponse{
				Audio: data, Format: formatOf(wantExt), VoiceId: req.GetVoiceId(),
				Provider: effectiveProvider, CostUsd: 0, CacheHit: true,
			}, nil
		}
	}

	data, gotExt, err := be.Synthesize(ctx, req.GetText(), name, rate)
	if err != nil {
		s.deps.Logger.ErrorContext(ctx, "synthesis failed",
			"provider", provider, "voice", name, "label", req.GetLabel(), "err", err)
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

	cost := pricing.CostUSD(effectiveProvider, name, chars)
	s.deps.Logger.InfoContext(ctx, "synthesized",
		"provider", effectiveProvider, "voice", name, "chars", chars,
		"cost_usd", cost, "label", req.GetLabel())

	return &ttsv1.SynthesizeResponse{
		Audio: data, Format: formatOf(wantExt), VoiceId: req.GetVoiceId(),
		Provider: effectiveProvider, CostUsd: cost, CacheHit: false,
	}, nil
}

func (s *Server) ListVoices(ctx context.Context, _ *ttsv1.ListVoicesRequest) (*ttsv1.ListVoicesResponse, error) {
	var all []catalog.Voice
	if s.deps.Google != nil {
		gv, err := s.deps.Google.Voices(ctx)
		if err != nil {
			// A voice list is a picker, not a dependency of speech itself.
			s.deps.Logger.WarnContext(ctx, "google catalog unavailable", "err", err)
		} else {
			all = append(all, gv...)
		}
	}
	all = append(all, s.deps.VieNeuVoices...)

	out := make([]*ttsv1.Voice, 0, len(all))
	for _, v := range all {
		out = append(out, &ttsv1.Voice{
			Id: v.ID, Label: v.Label, Provider: v.Provider, Tier: v.Tier,
			Gender: v.Gender, FreeTierCharsPerMonth: v.FreeTierChars,
		})
	}
	return &ttsv1.ListVoicesResponse{Voices: out}, nil
}
