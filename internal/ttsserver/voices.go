package ttsserver

import (
	"context"
	"errors"
	"strings"
	"unicode/utf8"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	ttsv1 "github.com/the-algovn/protos/gen/go/algovn/tts/v1"
	"github.com/the-algovn/tts-service/internal/audio"
	"github.com/the-algovn/tts-service/internal/catalog"
	"github.com/the-algovn/tts-service/internal/voices"
)

const (
	providerVoxCPM = "voxcpm"
	minRefSeconds  = 3.0
	maxRefSeconds  = 30.0
)

// Designer renders candidate takes of a text-described voice.
type Designer interface {
	Design(ctx context.Context, description, sampleText string, takes int) ([][]byte, error)
}

func (s *Server) registryReady() error {
	if s.deps.Voices == nil || s.deps.Designer == nil {
		return status.Error(codes.FailedPrecondition, "voice registry not configured")
	}
	return nil
}

func toProtoVoice(v voices.Voice) *ttsv1.Voice {
	return &ttsv1.Voice{Id: providerVoxCPM + ":" + v.ID, Label: v.Label,
		Provider: providerVoxCPM, Tier: "self-hosted", Gender: v.Gender}
}

// CreateVoice registers a cloned voice from a 3-30s reference clip. It returns
// InvalidArgument for bad fields or audio, FailedPrecondition when the registry
// is not configured, and Internal when persisting fails.
func (s *Server) CreateVoice(ctx context.Context, req *ttsv1.CreateVoiceRequest) (*ttsv1.CreateVoiceResponse, error) {
	if err := s.registryReady(); err != nil {
		return nil, err
	}
	in := voices.NewVoice{Label: req.GetLabel(), Gender: req.GetGender(), RefText: req.GetRefText(), Source: "clone"}
	if err := voices.ValidateNew(in); err != nil {
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}
	secs, err := audio.Probe(ctx, req.GetRefAudio())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "ref_audio: %v", err)
	}
	if secs < minRefSeconds || secs > maxRefSeconds {
		return nil, status.Errorf(codes.InvalidArgument, "ref_audio is %.1fs; must be %.0f-%.0fs", secs, minRefSeconds, maxRefSeconds)
	}
	wav, err := audio.NormalizeWAV(ctx, req.GetRefAudio())
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "ref_audio: %v", err)
	}
	v, err := s.deps.Voices.Create(ctx, in, wav)
	if err != nil {
		s.deps.Logger.ErrorContext(ctx, "create voice failed", "err", err)
		return nil, status.Errorf(codes.Internal, "create voice: %v", err)
	}
	s.deps.Logger.InfoContext(ctx, "voice created", "id", v.ID, "label", v.Label, "ref_seconds", secs)
	return &ttsv1.CreateVoiceResponse{Voice: toProtoVoice(v)}, nil
}

// DesignVoice renders 1-3 candidate takes (MP3) of a voice described in text. It
// returns InvalidArgument for out-of-range inputs, FailedPrecondition when the
// registry is not configured, and Unavailable when the model server fails.
func (s *Server) DesignVoice(ctx context.Context, req *ttsv1.DesignVoiceRequest) (*ttsv1.DesignVoiceResponse, error) {
	if err := s.registryReady(); err != nil {
		return nil, err
	}
	desc, sample := strings.TrimSpace(req.GetDescription()), strings.TrimSpace(req.GetSampleText())
	switch {
	case desc == "" || utf8.RuneCountInString(desc) > 300:
		return nil, status.Error(codes.InvalidArgument, "description must be 1-300 characters")
	case sample == "" || utf8.RuneCountInString(sample) > 250:
		return nil, status.Error(codes.InvalidArgument, "sample_text must be 1-250 characters")
	case req.GetTakes() < 1 || req.GetTakes() > 3:
		return nil, status.Error(codes.InvalidArgument, "takes must be 1-3")
	}
	takes, err := s.deps.Designer.Design(ctx, desc, sample, int(req.GetTakes()))
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "design failed: %v", err)
	}
	for i, wav := range takes {
		mp3, err := audio.ToMP3(ctx, wav)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "encode take: %v", err)
		}
		takes[i] = mp3
	}
	return &ttsv1.DesignVoiceResponse{Takes: takes}, nil
}

// DeleteVoice removes a registry voice by its "voxcpm:<id>" id. It returns
// InvalidArgument for non-voxcpm ids, NotFound for unknown voices, and
// FailedPrecondition when the registry is not configured.
func (s *Server) DeleteVoice(ctx context.Context, req *ttsv1.DeleteVoiceRequest) (*ttsv1.DeleteVoiceResponse, error) {
	if err := s.registryReady(); err != nil {
		return nil, err
	}
	provider, name := catalog.Resolve(req.GetId())
	if provider != providerVoxCPM {
		return nil, status.Error(codes.InvalidArgument, "only voxcpm voices can be deleted")
	}
	if err := s.deps.Voices.Delete(ctx, name); err != nil {
		if errors.Is(err, voices.ErrNotFound) {
			return nil, status.Error(codes.NotFound, "voice not found")
		}
		return nil, status.Errorf(codes.Internal, "delete voice: %v", err)
	}
	s.deps.Logger.InfoContext(ctx, "voice deleted", "id", name)
	return &ttsv1.DeleteVoiceResponse{}, nil
}
